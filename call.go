package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
)

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
func (c *Cookie) Reply() ([]interface{}, error) {
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
		return nil, Error{name, rvs}
	}
	return rvs, nil
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
		return Error{msg.Headers[FieldErrorName].value.(string), rvs}
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
		return Error{msg.Headers[FieldErrorName].value.(string), rvs}
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
func (o *Object) Call(method string, flags Flags, args ...interface{}) *Cookie {
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
		o.conn.replies[msg.Serial] = make(chan interface{}, 1)
		o.conn.repliesLck.Unlock()
		o.conn.out <- msg
		return &Cookie{o.conn, msg.Serial}
	}
	o.conn.out <- msg
	return nil
}
