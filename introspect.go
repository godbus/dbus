package dbus

import (
	"encoding/xml"
	"strings"
)

var introspectMsg = &CallMessage{
	Interface: "org.freedesktop.DBus.Introspectable",
	Name:      "Introspect",
}

// Node is the root element of an introspection.
type Node struct {
	XMLName    xml.Name    `xml:"node"`
	Name       string      `xml:"name,attr,omitempty"`
	Interfaces []Interface `xml:"interface"`
	Children   []Node      `xml:"node,omitempty"`
}

// Interface describes a DBus interface that is available on the message bus. It
// is returned by Introspect (as a member of Node) or passed to Export which
// uses it to generate the Introspection data.
type Interface struct {
	Name string `xml:"name,attr"`

	// This field is currently ignored by Export.
	Methods []Method `xml:"method"`

	Signals     []Signal     `xml:"signal"`
	Properties  []Property   `xml:"property"`
	Annotations []Annotation `xml:"annotation"`

	// Value that methods are invoked on (for Export).
	v interface{}
}

// Method describes a Method on an Interface as retured by an introspection.
type Method struct {
	Name        string       `xml:"name,attr"`
	Args        []Arg        `xml:"arg"`
	Annotations []Annotation `xml:"annotation"`
}

// Signal describes a Signal emitted on an Interface.
type Signal struct {
	Name        string       `xml:"name,attr"`
	Args        []Arg        `xml:"arg"`
	Annotations []Annotation `xml:"annotation"`
}

// Property describes a property of an Interface.
type Property struct {
	Name string `xml:"name,attr"`

	// Must be a valid signature.
	Type string `xml:"type,attr"`

	Access      string       `xml:"access,attr"`
	Annotations []Annotation `xml:"annotation"`
}

// Arg represents an argument of a method or a signal.
type Arg struct {
	// May be empty.
	Name string `xml:"name,attr"`

	// Must be a valid signature.
	Type string `xml:"type,attr"`

	// Must be "in" or "out" for methods and "out" or "" for signals.
	Direction string `xml:"direction,attr"`
}

// Annotation is a annotation in the introspection format.
type Annotation struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Introspect calls Introspect on the object identified by path and dest and
// returns the resulting data or any error.
func (conn *Connection) Introspect(path ObjectPath, dest string) (*Node, error) {
	var xmldata string
	var node Node

	msg := introspectMsg
	msg.Path = path
	msg.Destination = dest
	err := conn.Call(msg, 0).StoreReply(&xmldata)
	if err != nil {
		return nil, err
	}
	err = xml.NewDecoder(strings.NewReader(xmldata)).Decode(&node)
	if err != nil {
		return nil, err
	}
	if node.Name == "" {
		node.Name = string(path)
	}
	return &node, nil
}
