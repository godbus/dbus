package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
)

// Reply represents a reply to an error call. If Error is non-nil, it is either
// an error of the underlying transport a error message.
type Reply struct {
	Values []interface{}
	Err error
}

// Cookie represents a pending message reply. To get the reply, simply read from
// the channel.
type Cookie chan *Reply

// Store but decodes the values into provided pointers.
// It panics if one of retvalues is not a pointer to a DBus-representable value
// and returns an error if the signatures of the body and retvalues don't match,
// or if the reply that was returned contained an error.
func (c Cookie) Store(retvalues ...interface{}) error {
	reply := <-c
	if reply.Err != nil {
		return reply.Err
	}

	esig := GetSignature(retvalues...)
	rsig := GetSignature(reply.Values...)
	if esig != rsig {
		return errors.New("mismatched signature")
	}
	for i, v := range reply.Values {
		reflect.ValueOf(retvalues[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

// Object represents a remote object on which methods can be invoked.
type Object struct {
	conn *Connection
	dest string
	path ObjectPath
}

// Call calls a method with the given arguments. The method parameter must be
// formatted as "interface.method" (e.g., "org.freedesktop.DBus.Hello"). The
// returned cookie can be used to get the reply later.
func (o *Object) Call(method string, flags Flags, args ...interface{}) Cookie {
	i := strings.LastIndex(method, ".")
	if i == -1 {
		panic("invalid method parameter")
	}
	iface := method[:i]
	method = method[i+1:]
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeMethodCall
	msg.Serial = o.conn.getSerial()
	msg.Flags = flags & (NoAutoStart | NoReplyExpected)
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldPath] = MakeVariant(o.path)
	msg.Headers[FieldDestination] = MakeVariant(o.dest)
	msg.Headers[FieldMember] = MakeVariant(method)
	if iface != "" {
		msg.Headers[FieldInterface] = MakeVariant(iface)
	}
	if len(args) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(args...))
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.LittleEndian)
		enc.EncodeMulti(args...)
		msg.Body = buf.Bytes()
	} else {
		msg.Body = []byte{}
	}
	if msg.Flags&NoReplyExpected == 0 {
		o.conn.repliesLck.Lock()
		c := make(chan *Reply, 1)
		o.conn.replies[msg.Serial] = Cookie(c)
		o.conn.repliesLck.Unlock()
		o.conn.out <- msg
		return Cookie(c)
	}
	o.conn.out <- msg
	return nil
}
