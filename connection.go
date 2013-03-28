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

// Connection represents a connection to a message bus (usually, the system or
// session bus).
//
// Multiple goroutines may invoke methods on a connection simultaneously.
type Connection struct {
	transport
	uuid            string
	names           []string
	namesLck        sync.RWMutex
	serial          chan uint32
	serialUsed      chan uint32
	replies         map[uint32]chan *Reply
	repliesLck      sync.RWMutex
	handlers        map[ObjectPath]map[string]interface{}
	handlersLck     sync.RWMutex
	out             chan *Message
	signals         chan Signal
	signalsLck      sync.Mutex
	eavesdropped    chan *Message
	eavesdroppedLck sync.Mutex
	busObj          *Object
	unixFD          bool
}

// ConnectSessionBus connects to the session message bus.
func ConnectSessionBus() (*Connection, error) {
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address != "" && address != "autolaunch:" {
		return NewConnection(address)
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
	return NewConnection(string(b[i+1 : j]))
}

// ConnectSystemBus connects to the system message bus.
func ConnectSystemBus() (*Connection, error) {
	address := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
	if address != "" {
		return NewConnection(address)
	}
	return NewConnection(defaultSystemBusAddress)
}

// NewConnection establishes a new connection to the message bus specified by
// address.
func NewConnection(address string) (*Connection, error) {
	var err error
	conn := new(Connection)
	conn.transport, err = getTransport(address)
	if err != nil {
		return nil, err
	}
	if err = conn.auth(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	conn.replies = make(map[uint32]chan *Reply)
	conn.out = make(chan *Message, 10)
	conn.handlers = make(map[ObjectPath]map[string]interface{})
	conn.serial = make(chan uint32)
	conn.serialUsed = make(chan uint32)
	conn.busObj = conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	go conn.inWorker()
	go conn.outWorker()
	go conn.serials()
	if err = conn.hello(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	return conn, nil
}

// BusObject returns the message bus object.
func (conn *Connection) BusObject() *Object {
	return conn.busObj
}

// Close closes the connection. Any blocked operations will return with errors
// and the channels passed to Eavesdrop and Signal are closed.
func (conn *Connection) Close() error {
	close(conn.out)
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

// Eavesdrop changes the channel to which all messages are sent whose
// destination field is not one of the known names of this connection and which
// are not signals. The caller has to make sure that c is sufficiently buffered;
// if a message arrives when a write to c is not possible, the message is
// discarded.
//
// The channel can be reset by passing nil.
func (conn *Connection) Eavesdrop(c chan *Message) {
	conn.eavesdroppedLck.Lock()
	conn.eavesdropped = c
	conn.eavesdroppedLck.Unlock()
}

// hello sends the initial org.freedesktop.DBus.Hello call.
func (conn *Connection) hello() error {
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
func (conn *Connection) inWorker() {
	for {
		msg, err := conn.ReadMessage()
		if err == nil {
			dest, _ := msg.Headers[FieldDestination].value.(string)
			found := false
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
			conn.eavesdroppedLck.Lock()
			if !found && (msg.Type != TypeSignal || conn.eavesdropped != nil) {
				select {
				case conn.eavesdropped <- msg:
				default:
				}
				conn.eavesdroppedLck.Unlock()
				continue
			}
			conn.eavesdroppedLck.Unlock()
			switch msg.Type {
			case TypeMethodReply, TypeError:
				var reply *Reply
				serial := msg.Headers[FieldReplySerial].value.(uint32)
				if reply == nil {
					if msg.Type == TypeError {
						name, _ := msg.Headers[FieldErrorName].value.(string)
						reply = &Reply{nil, Error{name, msg.Body}}
					} else {
						reply = &Reply{msg.Body, nil}
					}
				}
				conn.repliesLck.Lock()
				if c, ok := conn.replies[serial]; ok {
					c <- reply
					conn.serialUsed <- serial
					delete(conn.replies, serial)
				}
				conn.repliesLck.Unlock()
			case TypeSignal:
				var signal Signal
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
				signal.Sender = msg.Headers[FieldSender].value.(string)
				signal.Path = msg.Headers[FieldPath].value.(ObjectPath)
				signal.Name = member + "." + iface
				signal.Body = msg.Body
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
			conn.repliesLck.RLock()
			for _, v := range conn.replies {
				v <- &Reply{nil, err}
			}
			conn.repliesLck.RUnlock()
			return
		}
		// invalid messages are ignored
	}
}

// Names returns the list of all names that are currently owned by this
// connection. The slice is always at least one element long, the first element
// being the unique name of the connection.
func (conn *Connection) Names() []string {
	conn.namesLck.RLock()
	// copy the slice so it can't be modified
	s := make([]string, len(conn.names))
	copy(s, conn.names)
	conn.namesLck.RUnlock()
	return s
}

// Object returns the object identified by the given destination name and path.
// It panics if path is not a valid object path.
func (conn *Connection) Object(dest string, path ObjectPath) *Object {
	if !path.IsValid() {
		panic("(*dbus.Connection).Object: invalid path name")
	}
	return &Object{conn, dest, path}
}

// outWorker runs in an own goroutine, encoding and sending messages that are
// sent to conn.out.
func (conn *Connection) outWorker() {
	for msg := range conn.out {
		err := conn.SendMessage(msg)
		conn.repliesLck.RLock()
		if err != nil {
			if conn.replies[msg.serial] != nil {
				conn.replies[msg.serial] <- &Reply{nil, err}
			}
			conn.serialUsed <- msg.serial
		} else if msg.Type != TypeMethodCall {
			conn.serialUsed <- msg.serial
		}
		conn.repliesLck.RUnlock()
	}
}

// Send the given message to the message bus. You usually don't need to use
// this; use the higher-level equivalents (Call, Emit and Export) instead.
// The returned cookie is nil if msg isn't a message call or if NoReplyExpected
// is set.
func (conn *Connection) Send(msg *Message) Cookie {
	if err := msg.IsValid(); err != nil {
		c := make(chan *Reply, 1)
		c <- &Reply{nil, err}
		return Cookie(c)
	}
	msg.serial = <-conn.serial
	if msg.Type == TypeMethodCall && msg.Flags&FlagNoReplyExpected == 0 {
		conn.repliesLck.Lock()
		c := make(chan *Reply, 1)
		conn.replies[msg.serial] = c
		conn.repliesLck.Unlock()
		conn.out <- msg
		return Cookie(c)
	}
	conn.out <- msg
	return nil
}

// sendError creates an error message corresponding to the parameters and sends
// it to conn.out.
func (conn *Connection) sendError(e Error, dest string, serial uint32) {
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
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(e.Body...))
	}
	conn.out <- msg
}

// sendReply creates a method reply message corresponding to the parameters and
// sends it to conn.out.
func (conn *Connection) sendReply(dest string, serial uint32, values ...interface{}) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodReply
	msg.serial = <-conn.serial
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	msg.Body = values
	if len(values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(values...))
	}
	conn.out <- msg
}

// serials runs in an own goroutine, constantly sending serials on conn.serial
// and reading serials that are ready for "recycling" from conn.serialUsed.
func (conn *Connection) serials() {
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
func (conn *Connection) Signal(c chan Signal) {
	conn.signalsLck.Lock()
	conn.signals = c
	conn.signalsLck.Unlock()
}

// SupportsUnixFDs returns whether the underlying transport supports passing of
// unix file descriptors. If this is false, method calls containing unix file
// descriptors will return an error and emitted signals containing them will
// not be sent.
func (conn *Connection) SupportsUnixFDs() bool {
	return conn.unixFD
}

// Error represents a DBus message of type Error.
type Error struct {
	Name string
	Body []interface{}
}

func (e Error) Error() string {
	if len(e.Body) > 1 {
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
