package dbus

import (
	"encoding/binary"
	"errors"
	"reflect"
	"strings"
)

// Call represents a pending or completed method call. lIf Erris non-nil, it is
// either an error from the underlying transport or an error message from the peer.
type Call struct {
	Destination string
	Path        ObjectPath
	Method      string        // Formatted as "interface.method".
	Args        []interface{} // Original arguments of the call.
	Body        []interface{} // Holds the response once the call is done.
	Err         error         // After completion, the error status.
	Done        chan *Call    // Strobes when the call is complete.
}

// Store stores the body of the reply into the provided pointers.
// It panics if one of retvalues is not a pointer to a DBus-representable value
// and returns an error if the signatures of the body and retvalues don't match,
// or if the reply that was returned contained an error.
func (c *Call) Store(retvalues ...interface{}) error {
	if c.Err != nil {
		return c.Err
	}

	esig := GetSignature(retvalues...)
	rsig := GetSignature(c.Body...)
	if esig != rsig {
		return errors.New("mismatched signature")
	}
	for i, v := range c.Body {
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

// Call calls a method with (*Object).Go and waits for its reply.
func (o *Object) Call(method string, flags Flags, args ...interface{}) *Call {
	return <-o.Go(method, flags, make(chan *Call, 1), args...).Done
}

// Go calls a method with the given arguments asynchronously. The method
// parameter must be formatted as "interface.method" (e.g.,
// "org.freedesktop.DBus.Hello"). It returns a Call structure representing this
// method call. The passed channel will return the same value once the call is
// done. If ch is nil, a new channel will be allocated. Otherwise, ch has to be
// buffered or Call will panic.
//
// If the flags include FlagNoReplyExpected, nil is returned and ch is ignored.
func (o *Object) Go(method string, flags Flags, ch chan *Call, args ...interface{}) *Call {
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
		if ch == nil {
			ch = make(chan *Call, 10)
		} else if cap(ch) == 0 {
			panic("(*dbus.Object).Call: unbuffered channel")
		}
		call := &Call{
			Destination: o.dest,
			Path:        o.path,
			Method:      method,
			Args:        args,
			Done:        ch,
		}
		o.conn.callsLck.Lock()
		o.conn.calls[msg.serial] = call
		o.conn.callsLck.Unlock()
		o.conn.out <- msg
		return call
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
