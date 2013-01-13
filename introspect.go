package dbus

import (
	"encoding/xml"
	"strings"
)

// Node is the root element of an introspection.
type Node struct {
	XMLName    xml.Name    `xml:"node"`
	Name       string      `xml:"name,attr"`
	Interfaces []Interface `xml:"interface"`
	Children   []Node      `xml:"node"`
}

// Interface describes a DBus interface as returned by an introspection.
type Interface struct {
	Name        string       `xml:"name,attr"`
	Methods     []Method     `xml:"method"`
	Signals     []Signal     `xml:"signal"`
	Properties  []Property   `xml:"property"`
	Annotations []Annotation `xml:"annotation"`
}

// Method describes a Method on an Interface as retured by an introspection.
type Method struct {
	Name        string       `xml:"name,attr"`
	Args        []Arg        `xml:"arg"`
	Annotations []Annotation `xml:"annotation"`
}

// Signal describes a Signal emitted on an Interface as returned by an
// introspection.
type Signal struct {
	Name        string       `xml:"name,attr"`
	Args        []Arg        `xml:"arg"`
	Annotations []Annotation `xml:"annotation"`
}

// Property describes a property of an Interface as returned by an
// introspection.
type Property struct {
	Name        string       `xml:"name,attr"`
	Type        string       `xml:"type,attr"`
	Access      string       `xml:"access,attr"`
	Annotations []Annotation `xml:"annotation"`
}

// Arg represents an argument of a method or a signal as returned by an
// introspection.
type Arg struct {
	Name      string `xml:"name,attr"` // can be empty
	Type      string `xml:"type,attr"`
	Direction string `xml:"direction,attr"` // "in"/"out", can be empty for signals,
}

// Annotation is a annotation in the introspection format.
type Annotation struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Introspect calls Introspect on the object identified by path and dest and
// returns the resulting data or any error.
func (conn *Connection) Introspect(path, dest string) (*Node, error) {
	var xmldata string
	var node Node

	err := conn.Call(dest, path, "org.freedesktop.DBus.Introspectable",
		"Introspect", 0).StoreReply(&xmldata)
	if err != nil {
		return nil, err
	}
	err = xml.NewDecoder(strings.NewReader(xmldata)).Decode(&node)
	if err != nil {
		return nil, err
	}
	if node.Name == "" {
		node.Name = path
	}
	return &node, nil
}
