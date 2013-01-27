package dbus

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"reflect"
)

var (
	errmsgInvalidArg = Error{
		"org.freedesktop.DBus.Error.InvalidArgs",
		[]interface{}{"Invalid type / number of args"},
	}
	errmsgUnknownMethod = Error{
		"org.freedesktop.DBus.Error.UnknownMethod",
		[]interface{}{"Unkown / invalid method"},
	}
)

func (conn *Connection) handleCall(msg *Message) {
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
	ifacename := msg.Headers[FieldInterface].value.(string)
	sender := msg.Headers[FieldSender].value.(string)
	serial := msg.Serial
	conn.handlersLck.RLock()
	obj := conn.handlers[path]
	if obj == nil {
		conn.sendError(errmsgUnknownMethod, sender, serial)
		conn.handlersLck.RUnlock()
		return
	}
	iface := obj[ifacename]
	conn.handlersLck.RUnlock()
	if ifacename == "org.freedesktop.DBus.Peer" {
		switch name {
		case "Ping":
			conn.sendReply(sender, serial)
		case "GetMachineId":
			conn.sendReply(sender, serial, conn.uuid)
		}
		return
	} else if ifacename == "org.freedesktop.DBus.Introspectable" && name == "Introspect" {
		var n Node
		n.Interfaces = make([]Interface, 0)
		conn.handlersLck.RLock()
		for _, v := range obj {
			n.Interfaces = append(n.Interfaces, *v)
		}
		conn.handlersLck.RUnlock()
		b, _ := xml.Marshal(n)
		conn.sendReply(sender, serial, string(b))
		return
	}
	if iface == nil {
		conn.sendError(errmsgUnknownMethod, sender, serial)
		return
	}
	m := reflect.ValueOf(iface.v).MethodByName(name)
	if !m.IsValid() {
		conn.sendError(errmsgUnknownMethod, sender, serial)
		return
	}
	t := m.Type()
	if t.NumOut() == 0 ||
		t.Out(t.NumOut()-1) != reflect.TypeOf(&errmsgInvalidArg) {

		conn.sendError(errmsgUnknownMethod, sender, serial)
		return
	}
	if t.NumIn() != len(vs) {
		conn.sendError(errmsgInvalidArg, sender, serial)
		return
	}
	for i := 0; i < t.NumIn(); i++ {
		if t.In(i) != reflect.TypeOf(vs[i]) {
			conn.sendError(errmsgInvalidArg, sender, serial)
			return
		}
	}
	params := make([]reflect.Value, len(vs))
	for i := 0; i < len(vs); i++ {
		params[i] = reflect.ValueOf(vs[i])
	}
	ret := m.Call(params)
	if em := ret[t.NumOut()-1].Interface().(*Error); em != nil {
		conn.sendError(*em, sender, serial)
		return
	}
	if msg.Flags&NoReplyExpected == 0 {
		body := new(bytes.Buffer)
		sig := ""
		enc := NewEncoder(body, binary.LittleEndian)
		for i := 0; i < len(ret)-1; i++ {
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
		if len(ret) != 1 {
			reply.Headers[FieldSignature] = MakeVariant(Signature{sig})
			reply.Body = body.Bytes()
		} else {
			reply.Body = []byte{}
		}
		conn.out <- reply
	}
}

// Emit emits the given signal on the message bus.
func (conn *Connection) Emit(path ObjectPath, iface string, name string, values ...interface{}) {
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeSignal
	msg.Serial = conn.getSerial()
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldInterface] = MakeVariant(iface)
	msg.Headers[FieldMember] = MakeVariant(name)
	msg.Headers[FieldPath] = MakeVariant(path)
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

// Export the given value as an object on the message bus. Package dbus will
// translate method calls on path to actual method calls. The iface parameter
// gives the name of the interface and the other introspection data that is
// passed to any peer calling org.freedesktop.Introspectable.Introspect.
//
// If a method call on the given path and interface is received, an exported
// method with the same name is called if the parameters match and the last
// return value is of type *ErrorMessage. If this value is not nil, it is
// sent back to the caller as an error. Otherwise, a method reply is sent
// with the other parameters as its body.
//
// The method is executed in a new goroutine.
//
// If you need to implement multiple interfaces on one "object", wrap it with
// (Go) interfaces.
//
// If path is not a valid object path, Export panics.
func (conn *Connection) Export(v interface{}, path ObjectPath, iface *Interface) {
	if !path.IsValid() {
		panic("(*dbus.Connection).Export: invalid path name")
	}
	iface.v = v
	iface.Methods = genMethods(v)
	// TODO: check that iface is valid (valid name, valid signatures ...)
	conn.handlersLck.Lock()
	if conn.handlers[path] == nil {
		conn.handlers[path] = make(map[string]*Interface)
	}
	conn.handlers[path][iface.Name] = iface
	conn.handlersLck.Unlock()
}

// Request name calls org.freedesktop.DBus.RequestName.
func (conn *Connection) RequestName(name string, flags RequestNameFlags) (RequestNameReply, error) {
	var r uint32
	err := conn.busObj.Call("org.freedesktop.DBus.RequestName", 0, name, flags).StoreReply(&r)
	if err != nil {
		return 0, err
	}
	return RequestNameReply(r), nil
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

func genMethods(v interface{}) []Method {
	rv := reflect.ValueOf(v)
	ms := make([]Method, 0)
	for i := 0; i < rv.NumMethod(); i++ {
		m := rv.Type().Method(i)
		t := m.Type
		args := make([]Arg, 0)
		if t.NumOut() == 0 ||
			t.Out(t.NumOut()-1) != reflect.TypeOf(&errmsgInvalidArg) {

			continue
		}
		// t.In(0) is receiver, so start at 1
		for j := 1; j < t.NumIn(); j++ {
			// TODO: maybe use the name here
			args = append(args, Arg{Type: getSignature(t.In(j)), Direction: "in"})
		}
		for j := 0; j < t.NumOut()-1; j++ {
			// TODO: dito
			args = append(args, Arg{Type: getSignature(t.Out(j)), Direction: "out"})
		}
		ms = append(ms, Method{Name: m.Name, Args: args})
	}
	return ms
}
