package dbus

import (
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
)

// Cookie represents a pending message reply. To get the reply, simply receive
// from the channel.
type Cookie <-chan *Reply

// Store waits for the reply of c and stores the values into the provided pointers.
// It panics if one of retvalues is not a pointer to a DBus-representable value
// and returns an error if the signatures of the body and retvalues don't match,
// or if the reply that was returned contained an error.
func (c Cookie) Store(retvalues ...interface{}) error {
	reply := <-c
	if reply.Err != nil {
		return reply.Err
	}

	esig := GetSignature(retvalues...)
	rsig := GetSignature(reply.Body...)
	if esig != rsig {
		return errors.New("mismatched signature")
	}
	for i, v := range reply.Body {
		reflect.ValueOf(retvalues[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// Object represents a remote object on which methods can be invoked.
type Object struct {
	conn *Conn
	dest string
	path ObjectPath
}

// Call calls a method with the given arguments. The method parameter must be
// formatted as "interface.method" (e.g., "org.freedesktop.DBus.Hello"). The
// returned cookie can be used to get the reply later, unless the flags include
// FlagNoReplyExpected, in which case a nil channel is returned.
func (o *Object) Call(method string, flags Flags, args ...interface{}) Cookie {
	i := strings.LastIndex(method, ".")
	iface := method[:i]
	method = method[i+1:]
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodCall
	msg.serial = <-o.conn.serial
	msg.Flags = flags & (FlagNoAutoStart | FlagNoReplyExpected)
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldPath] = MakeVariant(o.path)
	msg.Headers[FieldDestination] = MakeVariant(o.dest)
	msg.Headers[FieldMember] = MakeVariant(method)
	if iface != "" {
		msg.Headers[FieldInterface] = MakeVariant(iface)
	}
	msg.Body = args
	if len(args) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(args...))
	}
	if msg.Flags&FlagNoReplyExpected == 0 {
		o.conn.repliesLck.Lock()
		c := make(chan *Reply, 1)
		o.conn.replies[msg.serial] = c
		o.conn.repliesLck.Unlock()
		o.conn.out <- msg
		return Cookie(c)
	}
	o.conn.out <- msg
	return nil
}

// Destination returns the destination that calls on o are sent to.
func (o *Object) Destination() string {
	return o.dest
}

// Path returns the path that calls on o are sent to.
func (o *Object) Path() ObjectPath {
	return o.path
}

// Reply represents a reply to a method call. If Error is non-nil, it is either
// an error from the underlying transport or an error message from the peer.
type Reply struct {
	Body []interface{}
	Err  error
}
