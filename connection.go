package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
)

const defaultSystemBusAddress = "unix:path=/var/run/dbus/system_bus_socket"

// CallMessage represents a DBus message of type MethodCall.
type CallMessage struct {
	Name      string
	Path      string
	Interface string
	Args      []interface{}
}

// BUG(guelfey): Object paths should be verified and passed as ObjectPaths where
// possible.

// Connection represents a connection to a message bus (usually, the system or
// session bus).
type Connection struct {
	transport     net.Conn
	uuid          string
	uaddr         string
	lastSerial    uint32
	lastSerialLck sync.Mutex
	replies       map[uint32]chan interface{}
	repliesLck    sync.RWMutex
	handlers      map[string]Handler
	handlersLck   sync.RWMutex
	out           chan *Message
	signals       chan SignalMessage
}

// ConnectSessionBus connects to the session message bus and returns the
// connection or any error that occured.
func ConnectSessionBus() (*Connection, error) {
	// TODO: autolaunch
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	if address != "" {
		return NewConnection(address)
	}
	return nil, errors.New("couldn't determine address of the session bus")
}

// ConnectSystemBus connects to the system message bus and returns the
// connection or any error that occured.
func ConnectSystemBus() (*Connection, error) {
	address := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
	if address != "" {
		return NewConnection(address)
	}
	return NewConnection(defaultSystemBusAddress)
}

func NewConnection(address string) (*Connection, error) {
	// BUG(guelfey): Only the "unix" transport is supported right now.
	var err error
	conn := new(Connection)
	if strings.HasPrefix(address, "unix") {
		abstract := getKey(address, "abstract")
		path := getKey(address, "path")
		switch {
		case abstract == "" && path == "":
			return nil, errors.New("neither path nor abstract set")
		case abstract != "" && path == "":
			conn.transport, err = net.Dial("unix", "@"+abstract)
			if err != nil {
				return nil, err
			}
		case abstract == "" && path != "":
			conn.transport, err = net.Dial("unix", path)
			if err != nil {
				return nil, err
			}
		case abstract != "" && path != "":
			return nil, errors.New("both path and abstract set")
		}
	} else {
		return nil, errors.New("invalid or unsupported transport")
	}
	if err = conn.auth(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	conn.replies = make(map[uint32]chan interface{})
	conn.out = make(chan *Message, 10)
	conn.signals = make(chan SignalMessage, 10)
	conn.handlers = make(map[string]Handler)
	go conn.inWorker()
	go conn.outWorker()
	if err = conn.hello(); err != nil {
		return nil, err
	}
	return conn, nil
}

// Call invokes the method named name on the object specified by destination,
// path and iface with the given parameters. If iface is empty, it is not sent
// in the message. If flags does not contain NoReplyExpected, a cookie
// is returned that can be used for querying the reply. Otherwise, nil is returned.
//
// TODO: maybe this should take a CallMessage
func (conn *Connection) Call(destination, path, iface, name string,
	flags Flags, params ...interface{}) *Cookie {

	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodCall
	msg.Flags = flags & (NoAutoStart | NoReplyExpected)
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldPath] = MakeVariant(ObjectPath(path))
	msg.Headers[FieldDestination] = MakeVariant(destination)
	msg.Headers[FieldMember] = MakeVariant(name)
	if iface != "" {
		msg.Headers[FieldInterface] = MakeVariant(iface)
	}
	if params != nil && len(params) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(params...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(params...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	if msg.Flags&NoReplyExpected == 0 {
		conn.repliesLck.Lock()
		conn.replies[msg.Serial] = make(chan interface{}, 1)
		conn.repliesLck.Unlock()
		conn.out <- msg
		return &Cookie{conn, msg.Serial}
	}
	conn.out <- msg
	return nil
}

// Close closes the underlying transport of the connection and stops all
// related goroutines.
func (conn *Connection) Close() error {
	conn.signals <- SignalMessage{Name: ".Closed"}
	close(conn.out)
	close(conn.signals)
	return conn.transport.Close()
}

func (conn *Connection) doHandle(msg *Message) {
	var vs []interface{}
	if len(msg.Body) != 0 {
		vs := msg.Headers[FieldSignature].value.(Signature).Values()
		dec := NewDecoder(bytes.NewBuffer(msg.Body), msg.Order)
		err := dec.DecodeMulti(vs...)
		if err != nil {
			return
		}
		vs = dereferenceAll(vs)
	}
	call := &CallMessage{msg.Headers[FieldMember].value.(string),
		string(msg.Headers[FieldPath].value.(ObjectPath)),
		msg.Headers[FieldInterface].value.(string), vs}
	h := conn.getHandler(call.Path)
	if h != nil {
		reply, errmsg := h(call)
		if (reply == nil && errmsg == nil) || (reply != nil && errmsg != nil) {
			return
		}
		if msg.Flags&NoReplyExpected == 0 {
			if reply != nil {
				conn.sendReply(msg.Serial,
					msg.Headers[FieldSender].value.(string), reply...)
				return
			}
			nmsg := new(Message)
			nmsg.Order = binary.LittleEndian
			nmsg.Type = TypeError
			nmsg.Serial = conn.getSerial()
			nmsg.Headers = make(map[HeaderField]Variant)
			nmsg.Headers[FieldDestination] = msg.Headers[FieldSender]
			nmsg.Headers[FieldReplySerial] = MakeVariant(msg.Serial)
			nmsg.Headers[FieldErrorName] = MakeVariant(errmsg.Name)
			buf := new(bytes.Buffer)
			if len(errmsg.Values) != 0 {
				nmsg.Headers[FieldSignature] = MakeVariant(GetSignature(errmsg.Values...))
				NewEncoder(buf, binary.LittleEndian).EncodeMulti(errmsg.Values...)
			}
			nmsg.Body = buf.Bytes()
			conn.out <- nmsg
		}
	}
}

func (conn *Connection) getHandler(path string) Handler {
	var n int
	var h Handler
	conn.handlersLck.RLock()
	for k, v := range conn.handlers {
		if strings.HasPrefix(path, k) && len(k) > n {
			n = len(k)
			h = v
		}
	}
	conn.handlersLck.RUnlock()
	return h
}

func (conn *Connection) getSerial() uint32 {
	conn.lastSerialLck.Lock()
	conn.repliesLck.RLock()
	defer conn.lastSerialLck.Unlock()
	defer conn.repliesLck.RUnlock()
	if conn.replies[conn.lastSerial+1] == nil {
		conn.lastSerial++
		return conn.lastSerial
	}
	var i uint32
	for i = conn.lastSerial + 2; conn.replies[i] != nil; {
		i++
	}
	conn.lastSerial = i
	return i
}

// Register the given function to be executed for method calls on objects that
// match pattern. The function will run in its own goroutine.
//
// Pattern matching is similar to http.(*ServeMux); i.e., longest pattern wins.
func (conn *Connection) HandleCall(pattern string, handler Handler) {
	conn.handlersLck.Lock()
	conn.handlers[pattern] = handler
	conn.handlersLck.Unlock()
}

func (conn *Connection) hello() error {
	var s string
	err := conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "Hello", 0).StoreReply(&s)
	if err != nil {
		return err
	}
	conn.uaddr = s
	return nil
}

func (conn *Connection) inWorker() {
	for {
		msg, err := conn.readMessage()
		if err == nil {
			switch msg.Type {
			case TypeMethodReply, TypeError:
				serial := msg.Headers[FieldReplySerial].value.(uint32)
				conn.repliesLck.RLock()
				if c, ok := conn.replies[serial]; ok {
					c <- msg
				}
				conn.repliesLck.RUnlock()
			case TypeSignal:
				var signal SignalMessage
				signal.Name = msg.Headers[FieldMember].value.(string)
				sig, _ := msg.Headers[FieldSignature].value.(Signature)
				if sig.str != "" {
					rvs := sig.Values()
					dec := NewDecoder(bytes.NewBuffer(msg.Body), msg.Order)
					err := dec.DecodeMulti(rvs...)
					if err != nil {
						continue
					}
					signal.Values = dereferenceAll(rvs)
				} else {
					signal.Values = make([]interface{}, 0)
				}
				// don't block trying to send a signal
				select {
				case conn.signals <- signal:
				default:
				}
			case TypeMethodCall:
				if msg.Headers[FieldInterface].value.(string) ==
					"org.freedesktop.DBus.Peer" {

					serial := msg.Serial
					sender := msg.Headers[FieldSender].value.(string)
					switch msg.Headers[FieldMember].value.(string) {
					case "Ping":
						conn.sendReply(serial, sender)
					case "GetMachineId":
						conn.sendReply(serial, sender, conn.uuid)
					}
				} else {
					go conn.doHandle(msg)
				}
			}
		} else if _, ok := err.(InvalidMessageError); !ok {
			conn.Close()
			conn.repliesLck.RLock()
			for _, v := range conn.replies {
				v <- err
			}
			conn.repliesLck.RUnlock()
			return
		}
		// invalid messages are ignored
	}
}

func (conn *Connection) outWorker() {
	buf := new(bytes.Buffer)
	for msg := range conn.out {
		msg.EncodeTo(buf)
		_, err := buf.WriteTo(conn.transport)
		conn.repliesLck.RLock()
		if err != nil && conn.replies[msg.Serial] != nil {
			conn.replies[msg.Serial] <- err
		}
		conn.repliesLck.RUnlock()
		buf.Reset()
	}
}

func (conn *Connection) readMessage() (*Message, error) {
	// read the first 16 bytes, from which we can figure out the length of the
	// rest of the message
	var header [16]byte
	if _, err := conn.transport.Read(header[:]); err != nil {
		return nil, err
	}
	var order binary.ByteOrder
	switch header[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, InvalidMessageError("invalid byte order")
	}
	// header[4:8] -> length of message body, header[12:16] -> length of header
	// fields (without alignment)
	var blen, hlen uint32
	binary.Read(bytes.NewBuffer(header[4:8]), order, &blen)
	binary.Read(bytes.NewBuffer(header[12:16]), order, &hlen)
	if hlen % 8 != 0 {
		hlen += 8 - (hlen % 8)
	}
	rest := make([]byte, int(blen + hlen))
	if _, err := conn.transport.Read(rest); err != nil {
		return nil, err
	}
	all := make([]byte, 16 + len(rest))
	copy(all, header[:])
	copy(all[16:], rest)
	return DecodeMessage(bytes.NewBuffer(all))
}

// Request name calls org.freedesktop.DBus.RequestName.
func (conn *Connection) RequestName(name string, flags RequestNameFlags) (RequestNameReply, error) {

	var r uint32
	err := conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "RequestName", 0, name, flags).StoreReply(&r)
	if err != nil {
		return 0, err
	}
	return RequestNameReply(r), nil
}

// Signals returns a channel to which all received signal messages are forwarded.
// The channel is buffered, but package dbus will not block when it is full, but
// discard the signal.
//
// If the connection is closed by the server or a call to Close, the special
// signal named ".Closed" is returned and the channel is closed.
func (conn *Connection) Signals() <-chan SignalMessage {
	return conn.signals
}

func (conn *Connection) sendReply(serial uint32, destination string,
	vs ...interface{}) {

	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodReply
	msg.Flags = 0
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(destination)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	if len(vs) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(vs...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(vs...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	conn.out <- msg
}

// Cookie represents a pending message reply. Each reply can only be queried
// once.
type Cookie struct {
	conn   *Connection
	serial uint32
}

// Reply blocks until a reply to this cookie is received and returns the
// unmarshalled representation of the body, treated as if it was wrapped in a
// Variant. If the error is not nil, it is either an error on the underlying
// transport or an ErrorMessage.
//
// If you know the type of the response, use StoreReply().
func (c *Cookie) Reply() (ReplyMessage, error) {
	msg, err := c.reply()
	if err != nil {
		return nil, err
	}
	sig := msg.Headers[FieldSignature].value.(Signature)
	rvs := sig.Values()
	dec := NewDecoder(bytes.NewBuffer(msg.Body), msg.Order)
	err = dec.DecodeMulti(rvs...)
	if err != nil {
		return nil, err
	}
	rvs = dereferenceAll(rvs)
	if msg.Type == TypeError {
		name, _ := msg.Headers[FieldErrorName].value.(string)
		return nil, ErrorMessage{name, rvs}
	}
	return ReplyMessage(rvs), nil
}

// reply blocks waiting for the reply and returns either the reply or a
// transport error. It can only be called once for every cookie.
func (c *Cookie) reply() (*Message, error) {
	if c == nil || c.conn == nil {
		return nil, errors.New("invalid cookie")
	}
	c.conn.repliesLck.RLock()
	if c.conn.replies[c.serial] == nil {
		return nil, errors.New("invalid cookie")
	}
	resp := <-c.conn.replies[c.serial]
	c.conn.repliesLck.RUnlock()
	defer func() {
		c.conn.repliesLck.Lock()
		c.conn.replies[c.serial] = nil
		c.conn.repliesLck.Unlock()
		c.conn = nil
	}()
	if msg, ok := resp.(*Message); ok {
		return msg, nil
	}
	return nil, resp.(error)
}

// StoreReply behaves like Reply, but decodes the values into provided pointers
// It panics if one of retvalues is not a pointer to a DBus-representable value
// and returns an error if the signatures of the body and retvalues don't match.
func (c *Cookie) StoreReply(retvalues ...interface{}) error {
	msg, err := c.reply()
	if err != nil {
		return err
	}
	esig := GetSignature(retvalues...)
	rsig := msg.Headers[FieldSignature].value.(Signature)
	dec := NewDecoder(bytes.NewBuffer(msg.Body), msg.Order)
	if msg.Type == TypeError {
		rvs := rsig.Values()
		err := dec.DecodeMulti(rvs...)
		if err != nil {
			return err
		}
		return ErrorMessage{msg.Headers[FieldErrorName].value.(string), rvs}
	}
	if esig != rsig {
		return errors.New("mismatched signature")
	}
	err = dec.DecodeMulti(retvalues...)
	if err != nil {
		return err
	}
	return nil
}

// WaitReply behaves like Reply, except that it discards the reply after it
// arrived.
func (c *Cookie) WaitReply() error {
	msg, err := c.reply()
	if err != nil {
		return err
	}
	if msg.Type == TypeError {
		rvs := msg.Headers[FieldSignature].value.(Signature).Values()
		dec := NewDecoder(bytes.NewBuffer(msg.Body), msg.Order)
		err := dec.DecodeMulti(rvs...)
		if err != nil {
			return err
		}
		return ErrorMessage{msg.Headers[FieldErrorName].value.(string), rvs}
	}
	return nil
}

// ErrorMessage represents a DBus message of type Error.
type ErrorMessage struct {
	Name   string
	Values []interface{}
}

func (e ErrorMessage) Error() string {
	if len(e.Values) > 1 {
		s, ok := e.Values[0].(string)
		if ok {
			return s
		}
	}
	return e.Name
}

type Handler func(*CallMessage) (ReplyMessage, *ErrorMessage)

// ReplyMessage represents a DBus message of type MethodReply.
type ReplyMessage []interface{}

type RequestNameFlags uint32

const (
	FlagAllowReplacement RequestNameFlags = 1 << iota
	FlagReplaceExisting
	FlagDoNotQueue
)

type RequestNameReply uint32

const (
	NameReplyPrimaryOwner RequestNameReply = 1 + iota
	NameReplyInQueue
	NameReplyExists
	NameReplyAlreadyOwner
)

// Signal represents a DBus message of type Signal.
type SignalMessage struct {
	Name   string
	Values []interface{}
}

func getKey(s, key string) string {
	i := strings.IndexRune(s, ':')
	if i == -1 {
		return ""
	}
	s = s[i+1:]
	i = strings.Index(s, key)
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

func dereferenceAll(vs []interface{}) []interface{} {
	for i := range vs {
		v := reflect.ValueOf(vs[i])
		v = v.Elem()
		vs[i] = v.Interface()
	}
	return vs
}
