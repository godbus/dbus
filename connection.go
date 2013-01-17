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

var (
	errmsgInvalidArg = ErrorMessage{
		"org.freedesktop.DBus.Error.InvalidArgs",
		[]interface{}{"Invalid type / number of args"},
	}
	errmsgUnknownMethod = ErrorMessage{
		"org.freedesktop.DBus.Error.UnknownMethod",
		[]interface{}{"Unkown / invalid method"},
	}
)

var (
	helloMsg = &CallMessage{
		Destination: "org.freedesktop.DBus",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Name:        "Hello",
	}
	requestNameMsg = &CallMessage{
		Destination: "org.freedesktop.DBus",
		Path:        "/org/freedesktop/DBus",
		Interface:   "org.freedesktop.DBus",
		Name:        "RequestName",
		Args:        []interface{}{nil, nil},
	}
)

// CallMessage represents a DBus message of type MethodCall.
type CallMessage struct {
	Destination string
	Path        string
	Interface   string
	Name        string
	Args        []interface{}
}

func (cm *CallMessage) toMessage(conn *Connection) *Message {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodCall
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldPath] = MakeVariant(ObjectPath(cm.Path))
	msg.Headers[FieldDestination] = MakeVariant(cm.Destination)
	msg.Headers[FieldMember] = MakeVariant(cm.Name)
	if cm.Interface != "" {
		msg.Headers[FieldInterface] = MakeVariant(cm.Interface)
	}
	if len(cm.Args) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(cm.Args...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(cm.Args...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	return msg
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
	handlers      map[string]map[string]interface{}
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

// NewConnection establishes a new connection to the message bus specified by
// address.
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
	conn.handlers = make(map[string]map[string]interface{})
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
func (conn *Connection) Call(cm *CallMessage, flags Flags) *Cookie {
	msg := cm.toMessage(conn)
	msg.Flags = flags & (NoAutoStart | NoReplyExpected)
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
	name := msg.Headers[FieldMember].value.(string)
	path := msg.Headers[FieldPath].value.(ObjectPath)
	iface := msg.Headers[FieldInterface].value.(string)
	sender := msg.Headers[FieldSender].value.(string)
	serial := msg.Serial
	conn.handlersLck.RLock()
	v := conn.handlers[string(path)][iface]
	conn.handlersLck.RUnlock()
	if v == nil {
		conn.out <- errmsgUnknownMethod.toMessage(conn, sender, serial)
		return
	}
	m := reflect.ValueOf(v).MethodByName(name)
	if !m.IsValid() {
		conn.out <- errmsgUnknownMethod.toMessage(conn, sender, serial)
		return
	}
	t := m.Type()
	if t.NumIn() != len(vs) {
		conn.out <- errmsgInvalidArg.toMessage(conn, sender, serial)
		return
	}
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) != reflect.TypeOf(vs[i]) {
			conn.out <- errmsgInvalidArg.toMessage(conn, sender, serial)
			return
		}
	}
	params := make([]reflect.Value, len(vs))
	for i := 0; i < len(vs); i++ {
		params[i] = reflect.ValueOf(vs[i])
	}
	ret := m.Call(params)
	if msg.Flags&NoReplyExpected == 0 {
		body := new(bytes.Buffer)
		sig := ""
		enc := NewEncoder(body, binary.LittleEndian)
		for i := 0; i < len(ret); i++ {
			enc.encode(ret[i])
			sig += getSignature(ret[i].Type())
		}
		reply := new(Message)
		reply.Order = binary.LittleEndian
		reply.Type = TypeMethodReply
		reply.Serial = conn.getSerial()
		reply.Headers = make(map[HeaderField]Variant)
		reply.Headers[FieldDestination] = msg.Headers[FieldSender]
		reply.Headers[FieldReplySerial] = MakeVariant(msg.Serial)
		if len(ret) != 0 {
			reply.Headers[FieldSignature] = MakeVariant(Signature{sig})
			reply.Body = body.Bytes()
		}
		conn.out <- reply
	}
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

// Translate DBus method calls on the given path and interface to actual method
// calls on the given interface. If a method call on the given path and
// interface is received, a exported method with the same name is searched and
// called if the parameters match. The method is executed in a new goroutine.
//
// If you need to implement multiple interfaces on one object, wrap it with
// (Go) interfaces.
func (conn *Connection) Handle(v interface{}, path, iface string) {
	conn.handlersLck.Lock()
	if conn.handlers[path] == nil {
		conn.handlers[path] = make(map[string]interface{})
	}
	// TODO: maybe we could do basic sanity checks on the methods of v here
	conn.handlers[path][iface] = v
	conn.handlersLck.Unlock()
}

func (conn *Connection) hello() error {
	var s string
	err := conn.Call(helloMsg, 0).StoreReply(&s)
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
						rm := ReplyMessage(nil)
						conn.out <- rm.toMessage(conn, sender, serial)
					case "GetMachineId":
						rm := ReplyMessage([]interface{}{conn.uuid})
						conn.out <- rm.toMessage(conn, sender, serial)
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
	if hlen%8 != 0 {
		hlen += 8 - (hlen % 8)
	}
	rest := make([]byte, int(blen+hlen))
	if _, err := conn.transport.Read(rest); err != nil {
		return nil, err
	}
	all := make([]byte, 16+len(rest))
	copy(all, header[:])
	copy(all[16:], rest)
	return DecodeMessage(bytes.NewBuffer(all))
}

// Request name calls org.freedesktop.DBus.RequestName.
func (conn *Connection) RequestName(name string, flags RequestNameFlags) (RequestNameReply, error) {

	var r uint32
	msg := requestNameMsg
	msg.Args[0] = name
	msg.Args[1] = flags
	err := conn.Call(msg, 0).StoreReply(&r)
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

func (em ErrorMessage) toMessage(conn *Connection, dest string, serial uint32) *Message {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeError
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldErrorName] = MakeVariant(em.Name)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	if len(em.Values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(em.Values...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(em.Values...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	return msg
}

// ReplyMessage represents a DBus message of type MethodReply.
type ReplyMessage []interface{}

func (rm ReplyMessage) toMessage(conn *Connection, dest string, serial uint32) *Message {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodReply
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	if len(rm) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(rm...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(rm...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	return msg
}

// RequestNameFlags represents the possible flags for the RequestName call.
type RequestNameFlags uint32

const (
	FlagAllowReplacement RequestNameFlags = 1 << iota
	FlagReplaceExisting
	FlagDoNotQueue
)

// RequestNameReply is the reply to a RequestName call.
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
