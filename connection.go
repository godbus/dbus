package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
)

const defaultSystemBusAddress = "unix:path=/var/run/dbus/system_bus_socket"

// Connection represents a connection to a message bus (usually, the system or
// session bus).
type Connection struct {
	transport     net.Conn
	uuid          string
	names         []string
	lastSerial    uint32
	lastSerialLck sync.Mutex
	replies       map[uint32]chan interface{}
	repliesLck    sync.RWMutex
	handlers      map[ObjectPath]*expObject
	handlersLck   sync.RWMutex
	out           chan *Message
	signals       chan Signal
	eavesdropped  chan Message
	busObj        *Object
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
	conn.signals = make(chan Signal, 10)
	conn.handlers = make(map[ObjectPath]*expObject)
	conn.eavesdropped = make(chan Message, 10)
	conn.busObj = conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	go conn.inWorker()
	go conn.outWorker()
	if err = conn.hello(); err != nil {
		conn.transport.Close()
		return nil, err
	}
	return conn, nil
}

// BusObject returns the message bus object (i.e.
// conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")).
func (conn *Connection) BusObject() *Object {
	return conn.busObj
}

// Close closes the underlying transport of the connection and stops all
// related goroutines.
func (conn *Connection) Close() error {
	close(conn.out)
	close(conn.signals)
	return conn.transport.Close()
}

// Eavesdropped returns a channel to which all messages are sent whose
// destination field is not one of the know names of this connection and which
// are not signals. The channel is buffered; messages received when the channel
// is full are discarded.
func (conn *Connection) Eavesdropped() <-chan Message {
	return conn.eavesdropped
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

func (conn *Connection) hello() error {
	var s string
	err := conn.busObj.Call("org.freedesktop.DBus.Hello", 0).StoreReply(&s)
	if err != nil {
		return err
	}
	conn.names = make([]string, 1)
	conn.names[0] = s
	return nil
}

func (conn *Connection) inWorker() {
	for {
		msg, err := conn.readMessage()
		if err == nil {
			dest, _ := msg.Headers[FieldDestination].value.(string)
			found := false
			if len(conn.names) == 0 {
				found = true
			}
			for _, v := range conn.names {
				if dest == v {
					found = true
					break
				}
			}
			log.Println(msg)
			if !found && msg.Type != TypeSignal {
				select {
				case conn.eavesdropped<-*msg:
					log.Println("sent")
				default:
				}
				continue
			}
			switch msg.Type {
			case TypeMethodReply, TypeError:
				serial := msg.Headers[FieldReplySerial].value.(uint32)
				conn.repliesLck.RLock()
				if c, ok := conn.replies[serial]; ok {
					c <- msg
				}
				conn.repliesLck.RUnlock()
			case TypeSignal:
				var signal Signal
				signal.Name = msg.Headers[FieldMember].value.(string)
				// if the signature is present, it is guaranteed to be valid
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
				go conn.handleCall(msg)
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

func (conn *Connection) sendError(e Error, dest string, serial uint32) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeError
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldErrorName] = MakeVariant(e.Name)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	if len(e.Values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(e.Values...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(e.Values...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	conn.out <- msg
}

func (conn *Connection) sendReply(dest string, serial uint32, values ...interface{}) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodReply
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldDestination] = MakeVariant(dest)
	msg.Headers[FieldReplySerial] = MakeVariant(serial)
	if len(values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(values...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(values...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	conn.out <- msg
}

// Object returns the object identified by the given destination name and path.
func (conn *Connection) Object(dest string, path ObjectPath) *Object {
	if !path.IsValid() {
		panic("invalid DBus path")
	}
	return &Object{conn, dest, path}
}

// Signals returns a channel to which all received signal messages are forwarded.
// The channel is buffered, but package dbus will not block when it is full, but
// discard the signal.
//
// If the connection is closed by the server or a call to Close, the channel is
// also closed.
func (conn *Connection) Signals() <-chan Signal {
	return conn.signals
}

// Error represents a DBus message of type Error.
type Error struct {
	Name   string
	Values []interface{}
}

func (e Error) Error() string {
	if len(e.Values) > 1 {
		s, ok := e.Values[0].(string)
		if ok {
			return s
		}
	}
	return e.Name
}

// Signal represents a DBus message of type Signal.
type Signal struct {
	Name      string
	Values    []interface{}
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
