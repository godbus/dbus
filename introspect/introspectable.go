package introspect

import (
	"encoding/xml"
	"github.com/guelfey/go.dbus"
)

// Introspectable implements org.freedesktop.Introspectable.
//
// You can create it by converting the XML-formatted introspection data from a
// string to an Introspectable or call NewIntrospectable with a Node. Then,
// export it as org.freedesktop.Introspectable on you object.
type Introspectable string

// NewIntrospectable returns an Introspectable that returns the introspection
// data that corresponds to the given Node.
func NewIntrospectable(n *Node) Introspectable {
	b, err := xml.Marshal(n)
	if err != nil {
		panic(err)
	}
	return Introspectable(b)
}

// Introspect implements org.freedesktop.Introspectable.Introspect.
func (i Introspectable) Introspect() (string, *dbus.Error) {
	return string(i), nil
}
