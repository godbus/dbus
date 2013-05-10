package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
)

const defaultSystemBusAddress = "unix:path=/var/run/dbus/system_bus_socket"

var (
	systemBus  *Conn
	sessionBus *Conn
)

// ErrClosed is the error returned by calls on a closed connection.
var ErrClosed = errors.New("closed by user")

// Conn represents a connection to a message bus (usually, the system or
// session bus).
//
// Multiple goroutines may invoke methods on a connection simultaneously.
type Conn struct {
	transport

	busObj *Object
	unixFD bool
	uuid   string

	names    []string
	namesLck sync.RWMutex

	serial     chan uint32
	serialUsed chan uint32

	calls    map[uint32]*Call
	callsLck sync.RWMutex

	handlers    map[ObjectPath]map[string]interface{}
	handlersLck sync.RWMutex

	out    chan *Message
	closed bool
	outLck sync.RWMutex

	signals    chan *Signal
	signalsLck sync.Mutex

	eavesdropped    chan *Message
	eavesdroppedLck sync.Mutex
}

// SessionBus returns the connection to the session bus, connecting to it if not
// already done.
func SessionBus() (conn *Conn, err error) {
	if sessionBus != nil {
		return sessionBus, nil
	}
	defer func() {
		if conn != nil {
			sessionBus = conn
		}
	}()
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address != "" && address != "autolaunch:" {
		return Dial(address)
	}
	cmd := exec.Command("dbus-launch")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}
	i := bytes.IndexByte(b, '=')
	j := bytes.IndexByte(b, '\n')
	if i == -1 || j == -1 {
		return nil, errors.New("couldn't determine address of the session bus")
	}
	return Dial(string(b[i+1 : j]))
}

// SystemBus returns the connection to the sytem bus, connecting to it if not
// already done.
func SystemBus() (conn *Conn, err error) {
	if systemBus != nil {
		return systemBus, nil
	}
	defer func() {
		if conn != nil {
			systemBus = conn
		}
	}()
	address := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
	if address != "" {
		return Dial(address)
	}
	return Dial(defaultSystemBusAddress)
}

// Dial establishes a new connection to the message bus specified by address.
func Dial(address string) (*Conn, error) {
	tr, err := getTransport(address)
	if err != nil {
		return nil, err
	}
	return newConn(tr)
}

// NewConn creates a new *Conn from an already established connection.
func NewConn(conn io.ReadWriteCloser) (*Conn, error) {
	return newConn(genericTransport{conn})
}

// newConn creates a new *Conn from a transport.
func newConn(tr transport) (*Conn, error) {
	conn := new(Conn)
	conn.transport = tr
	if err := conn.auth(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	conn.calls = make(map[uint32]*Call)
	conn.out = make(chan *Message, 10)
	conn.handlers = make(map[ObjectPath]map[string]interface{})
	conn.serial = make(chan uint32)
	conn.serialUsed = make(chan uint32)
	conn.busObj = conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	go conn.inWorker()
	go conn.outWorker()
	go conn.serials()
	if err := conn.hello(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	return conn, nil
}

// BusObject returns the object owned by the bus daemon which handles
// administrative requests.
func (conn *Conn) BusObject() *Object {
	return conn.busObj
}

// Close closes the connection. Any blocked operations will return with errors
// and the channels passed to Eavesdrop and Signal are closed.
func (conn *Conn) Close() error {
	conn.outLck.Lock()
	close(conn.out)
	conn.closed = true
	conn.outLck.Unlock()
	conn.signalsLck.Lock()
	if conn.signals != nil {
		close(conn.signals)
	}
	conn.signalsLck.Unlock()
	conn.eavesdroppedLck.Lock()
	if conn.eavesdropped != nil {
		close(conn.eavesdropped)
	}
	conn.eavesdroppedLck.Unlock()
	return conn.transport.Close()
}

// Eavesdrop causes conn to send all incoming messages to the given channel
// without further processing. Method replies, errors and signals will not be
// sent to the appropiate channels and method calls will not be handled. If nil
// is passed, the normal behaviour is restored.
//
// The caller has to make sure that c is sufficiently buffered;
// if a message arrives when a write to c is not possible, the message is
// discarded.
func (conn *Conn) Eavesdrop(c chan *Message) {
	conn.eavesdroppedLck.Lock()
	conn.eavesdropped = c
	conn.eavesdroppedLck.Unlock()
}

// hello sends the initial org.freedesktop.DBus.Hello call.
func (conn *Conn) hello() error {
	var s string
	err := conn.busObj.Call("org.freedesktop.DBus.Hello", 0).Store(&s)
	if err != nil {
		return err
	}
	conn.namesLck.Lock()
	conn.names = make([]string, 1)
	conn.names[0] = s
	conn.namesLck.Unlock()
	return nil
}

// inWorker runs in an own goroutine, reading incoming messages from the
// transport and dispatching them appropiately.
func (conn *Conn) inWorker() {
	for {
		msg, err := conn.ReadMessage()
		if err == nil {
			conn.eavesdroppedLck.Lock()
			if conn.eavesdropped != nil {
				select {
				case conn.eavesdropped <- msg:
				default:
				}
				conn.eavesdroppedLck.Unlock()
				continue
			}
			conn.eavesdroppedLck.Unlock()
			dest, _ := msg.Headers[FieldDestination].value.(string)
			found := false
			if dest == "" {
				found = true
			} else {
				conn.namesLck.RLock()
				if len(conn.names) == 0 {
					found = true
				}
				for _, v := range conn.names {
					if dest == v {
						found = true
						break
					}
				}
				conn.namesLck.RUnlock()
			}
			if !found {
				// Eavesdropped a message, but no channel for it is registered.
				// Ignore it.
				continue
			}
			switch msg.Type {
			case TypeMethodReply, TypeError:
				serial := msg.Headers[FieldReplySerial].value.(uint32)
				conn.callsLck.Lock()
				if c, ok := conn.calls[serial]; ok {
					if msg.Type == TypeError {
						name, _ := msg.Headers[FieldErrorName].value.(string)
						c.Err = Error{name, msg.Body}
					} else {
						c.Body = msg.Body
					}
					c.Done <- c
					conn.serialUsed <- serial
					delete(conn.calls, serial)
				}
				conn.callsLck.Unlock()
			case TypeSignal:
				iface := msg.Headers[FieldInterface].value.(string)
				member := msg.Headers[FieldMember].value.(string)
				if iface == "org.freedesktop.DBus" && member == "NameLost" &&
					msg.Headers[FieldSender].value.(string) == "org.freedesktop.DBus" {

					name, _ := msg.Body[0].(string)
					conn.namesLck.Lock()
					for i, v := range conn.names {
						if v == name {
							copy(conn.names[i:], conn.names[i+1:])
							conn.names = conn.names[:len(conn.names)-1]
						}
					}
					conn.namesLck.Unlock()
				}
				signal := &Signal{
					Sender: msg.Headers[FieldSender].value.(string),
					Path:   msg.Headers[FieldPath].value.(ObjectPath),
					Name:   iface + "." + member,
					Body:   msg.Body,
				}
				// don't block trying to send a signal
				conn.signalsLck.Lock()
				select {
				case conn.signals <- signal:
				default:
				}
				conn.signalsLck.Unlock()
			case TypeMethodCall:
				go conn.handleCall(msg)
			}
		} else if _, ok := err.(InvalidMessageError); !ok {
			// Some read error occured (usually EOF); we can't really do
			// anything but to shut down all stuff and returns errors to all
			// pending replies.
			conn.Close()
			conn.callsLck.RLock()
			for _, v := range conn.calls {
				v.Err = err
				v.Done <- v
			}
			conn.callsLck.RUnlock()
			return
		}
		// invalid messages are ignored
	}
}

// Names returns the list of all names that are currently owned by this
// connection. The slice is always at least one element long, the first element
// being the unique name of the connection.
func (conn *Conn) Names() []string {
	conn.namesLck.RLock()
	// copy the slice so it can't be modified
	s := make([]string, len(conn.names))
	copy(s, conn.names)
	conn.namesLck.RUnlock()
	return s
}

// Object returns the object identified by the given destination name and path.
func (conn *Conn) Object(dest string, path ObjectPath) *Object {
	return &Object{conn, dest, path}
}

// outWorker runs in an own goroutine, encoding and sending messages that are
// sent to conn.out.
func (conn *Conn) outWorker() {
	for msg := range conn.out {
		err := conn.SendMessage(msg)
		conn.callsLck.RLock()
		if err != nil {
			if c := conn.calls[msg.serial]; c != nil {
				c.Err = err
				c.Done <- c
			}
			conn.serialUsed <- msg.serial
		} else if msg.Type != TypeMethodCall {
			conn.serialUsed <- msg.serial
		}
		conn.callsLck.RUnlock()
	}
}

// Send sends the given message to the message bus. You usually don't need to
// use this; use the higher-level equivalents (Call / Go, Emit and Export)
// instead. If msg is a method call and NoReplyExpected is not set, a non-nil
// call is returned and the same value is sent to ch (which must be buffered)
// once the call is complete. Otherwise, ch is ignored and a Call structure is
// returned of which only the Err member is valid.
func (conn *Conn) Send(msg *Message, ch chan *Call) *Call {
	var call *Call

	msg.serial = <-conn.serial
	if msg.Type == TypeMethodCall && msg.Flags&FlagNoReplyExpected == 0 {
		if ch == nil {
			ch = make(chan *Call, 5)
		} else if cap(ch) == 0 {
			panic("(*dbus.Conn).Send: unbuffered channel")
		}
		call = new(Call)
		call.Destination, _ = msg.Headers[FieldDestination].value.(string)
		call.Path, _ = msg.Headers[FieldPath].value.(ObjectPath)
		iface, _ := msg.Headers[FieldInterface].value.(string)
		member, _ := msg.Headers[FieldMember].value.(string)
		call.Method = iface + "." + member
		call.Args = msg.Body
		call.Done = ch
		conn.callsLck.Lock()
		conn.calls[msg.serial] = call
		conn.callsLck.Unlock()
		conn.outLck.RLock()
		if conn.closed {
			call.Err = ErrClosed
			call.Done <- call
		} else {
			conn.out <- msg
		}
		conn.outLck.RUnlock()
	} else {
		conn.outLck.RLock()
		if conn.closed {
			call = &Call{Err: ErrClosed}
		} else {
			conn.out <- msg
			call = &Call{Err: nil}
		}
		conn.outLck.RUnlock()
	}
	return call
}

// sendError creates an error message corresponding to the parameters and sends
// it to conn.out.
func (conn *Conn) sendError(e Error, dest string, serial uint32) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeError
	msg.serial = <-conn.serial
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldErrorName] = MakeVariant(e.Name)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	msg.Body = e.Body
	if len(e.Body) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(SignatureOf(e.Body...))
	}
	conn.outLck.RLock()
	if !conn.closed {
		conn.out <- msg
	}
	conn.outLck.RUnlock()
}

// sendReply creates a method reply message corresponding to the parameters and
// sends it to conn.out.
func (conn *Conn) sendReply(dest string, serial uint32, values ...interface{}) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodReply
	msg.serial = <-conn.serial
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	msg.Body = values
	if len(values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(SignatureOf(values...))
	}
	conn.outLck.RLock()
	if !conn.closed {
		conn.out <- msg
	}
	conn.outLck.RUnlock()
}

// serials runs in an own goroutine, constantly sending serials on conn.serial
// and reading serials that are ready for "recycling" from conn.serialUsed.
func (conn *Conn) serials() {
	s := uint32(1)
	used := make(map[uint32]bool)
	used[0] = true // ensure that 0 is never used
	for {
		select {
		case conn.serial <- s:
			used[s] = true
			s++
			for used[s] {
				s++
			}
		case n := <-conn.serialUsed:
			delete(used, n)
		}
	}
}

// Signal sets the channel to which all received signal messages are forwarded.
// The caller has to make sure that c is sufficiently buffered; if a message
// arrives when a write to c is not possible, it is discarded.
//
// The channel can be reset by passing nil.
//
// This channel is "overwritten" by Eavesdrop; i.e., if there currently is a
// channel for eavesdropped messages, this channel receives all signals, and the
// channel passed to Signal will not receive any signals.
func (conn *Conn) Signal(c chan *Signal) {
	conn.signalsLck.Lock()
	conn.signals = c
	conn.signalsLck.Unlock()
}

// SupportsUnixFDs returns whether the underlying transport supports passing of
// unix file descriptors. If this is false, method calls containing unix file
// descriptors will return an error and emitted signals containing them will
// not be sent.
func (conn *Conn) SupportsUnixFDs() bool {
	return conn.unixFD
}

// Error represents a DBus message of type Error.
type Error struct {
	Name string
	Body []interface{}
}

func (e Error) Error() string {
	if len(e.Body) >= 1 {
		s, ok := e.Body[0].(string)
		if ok {
			return s
		}
	}
	return e.Name
}

// Signal represents a DBus message of type Signal. The name member is given in
// "interface.member" notation, e.g. org.freedesktop.DBus.NameLost.
type Signal struct {
	Sender string
	Path   ObjectPath
	Name   string
	Body   []interface{}
}

// transport is a DBus transport.
type transport interface {
	// Read and Write raw data (for example, for the authentication protocol).
	io.ReadWriteCloser

	// Send the initial null byte used for the EXTERNAL mechanism.
	SendNullByte() error

	// Returns whether this transport supports passing Unix FDs.
	SupportsUnixFDs() bool

	// Signal the transport that Unix FD passing is enabled for this connection.
	EnableUnixFDs()

	// Read / send a message, handling things like Unix FDs.
	ReadMessage() (*Message, error)
	SendMessage(*Message) error
}

func getTransport(address string) (transport, error) {
	var err error
	var t transport

	m := map[string]func(string) (transport, error){
		"unix": newUnixTransport,
	}
	addresses := strings.Split(address, ";")
	for _, v := range addresses {
		i := strings.IndexRune(v, ':')
		if i == -1 {
			err = errors.New("bad address: no transport")
			continue
		}
		f := m[v[:i]]
		if f == nil {
			err = errors.New("bad address: invalid or unsupported transport")
		}
		t, err = f(v[i+1:])
		if err == nil {
			return t, nil
		}
	}
	return nil, err
}

// dereferenceAll returns a slice that, assuming that vs is a slice of pointers
// of arbitrary types, containes the values that are obtained from dereferencing
// all elements in vs.
func dereferenceAll(vs []interface{}) []interface{} {
	for i := range vs {
		v := reflect.ValueOf(vs[i])
		v = v.Elem()
		vs[i] = v.Interface()
	}
	return vs
}

// getKey gets a key from a the list of keys. Returns "" on error / not found...
func getKey(s, key string) string {
	i := strings.Index(s, key)
	if i == -1 {
		return ""
	}
	if i+len(key)+1 >= len(s) || s[i+len(key)] != '=' {
		return ""
	}
	j := strings.Index(s, ",")
	if j == -1 {
		j = len(s)
	}
	return s[i+len(key)+1 : j]
}
