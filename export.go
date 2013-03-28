package dbus

import (
	"encoding/binary"
	"reflect"
	"strings"
	"unicode"
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

// handleCall handles the given method call (i.e. looks if it's one of the
// pre-implemented ones and searches for a corresponding handler if not).
func (conn *Connection) handleCall(msg *Message) {
	vs := msg.Body
	name := msg.Headers[FieldMember].value.(string)
	path := msg.Headers[FieldPath].value.(ObjectPath)
	ifacename := msg.Headers[FieldInterface].value.(string)
	sender := msg.Headers[FieldSender].value.(string)
	serial := msg.serial
	if len(name) == 0 || unicode.IsLower([]rune(name)[0]) {
		conn.sendError(errmsgUnknownMethod, sender, serial)
	}
	conn.handlersLck.RLock()
	obj, ok := conn.handlers[path]
	if !ok {
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
		default:
			conn.sendError(errmsgUnknownMethod, sender, serial)
		}
		return
	}
	if iface == nil {
		conn.sendError(errmsgUnknownMethod, sender, serial)
		return
	}
	m := reflect.ValueOf(iface).MethodByName(name)
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
	if msg.Flags&FlagNoReplyExpected == 0 {
		reply := new(Message)
		reply.Order = binary.LittleEndian
		reply.Type = TypeMethodReply
		reply.serial = <-conn.serial
		reply.Headers = make(map[HeaderField]Variant)
		reply.Headers[FieldDestination] = msg.Headers[FieldSender]
		reply.Headers[FieldReplySerial] = MakeVariant(msg.serial)
		reply.Body = make([]interface{}, len(ret)-1)
		for i := 0; i < len(ret)-1; i++ {
			reply.Body[i] = ret[i].Interface()
		}
		if len(ret) != 1 {
			reply.Headers[FieldSignature] = MakeVariant(GetSignature(reply.Body...))
		}
		conn.out <- reply
	}
}

// Emit emits the given signal on the message bus. The name parameter must be
// formatted as "interface.member", e.g., "org.freedesktop.DBus.NameLost". It
// panics if the path or the method name are invalid.
func (conn *Connection) Emit(path ObjectPath, name string, values ...interface{}) {
	if !path.IsValid() {
		panic("(*dbus.Connection).Emit: invalid path name")
	}
	i := strings.LastIndex(name, ".")
	if i == -1 {
		panic("(*dbus.Connection).Emit: invalid signal name")
	}
	iface := name[:i]
	member := name[i+1:]
	msg := new(Message)
	msg.Order = binary.LittleEndian
	msg.Type = TypeSignal
	msg.serial = <-conn.serial
	msg.Headers = make(map[HeaderField]Variant)
	msg.Headers[FieldInterface] = MakeVariant(iface)
	msg.Headers[FieldMember] = MakeVariant(member)
	msg.Headers[FieldPath] = MakeVariant(path)
	msg.Body = values
	if len(values) > 0 {
		msg.Headers[FieldSignature] = MakeVariant(GetSignature(values...))
	}
	conn.out <- msg
}

// Export the given value as an object on the message bus.
//
// If a method call on the given path and interface is received, an exported
// method with the same name is called if the parameters match and the last
// return value is of type *ErrorMessage. If this value is not nil, it is
// sent back to the caller as an error. Otherwise, a method reply is sent
// with the other parameters as its body.
//
// Every method call is executed in a new goroutine, so the method may be called
// in multiple goroutines at once.
//
// Multiple DBus interfaces can be implemented on one object by calling Export
// multiple times and converting the value to different (Go) interfaces each
// time.
//
// Export panics if path is not a valid object path.
func (conn *Connection) Export(v interface{}, path ObjectPath, iface string) {
	if !path.IsValid() {
		panic("(*dbus.Connection).Export: invalid path name")
	}
	conn.handlersLck.Lock()
	if _, ok := conn.handlers[path]; !ok {
		conn.handlers[path] = make(map[string]interface{})
	}
	conn.handlers[path][iface] = v
	conn.handlersLck.Unlock()
}

// ReleaseName calls org.freedesktop.DBus.ReleaseName. You should use only this
// method to release a name (see below).
func (conn *Connection) ReleaseName(name string) (ReleaseNameReply, error) {
	var r uint32
	err := conn.busObj.Call("org.freedesktop.DBus.ReleaseName", 0, name).Store(&r)
	if err != nil {
		return 0, err
	}
	if r == uint32(ReleaseNameReplyReleased) {
		conn.namesLck.Lock()
		for i, v := range conn.names {
			if v == name {
				copy(conn.names[i:], conn.names[i+1:])
				conn.names = conn.names[:len(conn.names)-1]
			}
		}
		conn.namesLck.Unlock()
	}
	return ReleaseNameReply(r), nil
}

// RequestName calls org.freedesktop.DBus.RequestName. You should use only this
// method to request a name because package dbus needs to keep track of all
// names that the connection has.
func (conn *Connection) RequestName(name string, flags RequestNameFlags) (RequestNameReply, error) {
	var r uint32
	err := conn.busObj.Call("org.freedesktop.DBus.RequestName", 0, name, flags).Store(&r)
	if err != nil {
		return 0, err
	}
	if r == uint32(RequestNameReplyPrimaryOwner) {
		conn.namesLck.Lock()
		conn.names = append(conn.names, name)
		conn.namesLck.Unlock()
	}
	return RequestNameReply(r), nil
}

// ReleaseNameReply is the reply to a ReleaseName call.
type ReleaseNameReply uint32

const (
	ReleaseNameReplyReleased ReleaseNameReply = 1 + iota
	ReleaseNameReplyNonExistent
	ReleaseNameReplyNotOwner
)

// RequestNameFlags represents the possible flags for the RequestName call.
type RequestNameFlags uint32

const (
	NameFlagAllowReplacement RequestNameFlags = 1 << iota
	NameFlagReplaceExisting
	NameFlagDoNotQueue
)

// RequestNameReply is the reply to a RequestName call.
type RequestNameReply uint32

const (
	RequestNameReplyPrimaryOwner RequestNameReply = 1 + iota
	RequestNameReplyInQueue
	RequestNameReplyExists
	RequestNameReplyAlreadyOwner
)
