// Package prop provides the Properties struct which can be used to implement
// org.freedesktop.DBus.Properties.
package prop

import (
	"github.com/guelfey/go.dbus"
	"sync"
)

// ErrIfaceNotFound is the error returned to peers who try to access properties
// on interfaces that aren't found.
var ErrIfaceNotFound = &dbus.Error{"org.freedesktop.DBus.Properties.Error.InterfaceNotFound", nil}

// ErrPropNotFound is the error returned to peers trying to access properties
// that aren't found.
var ErrPropNotFound = &dbus.Error{"org.freedesktop.DBus.Properties.Error.PropertyNotFound", nil}

// ErrReadOnly is the error returned to peers trying to set a read-only
// property.
var ErrReadOnly = &dbus.Error{"org.freedesktop.DBus.Properties.Error.ReadOnly", nil}

// ErrInvalidType is returned to peers that set a property to a value of invalid
// type.
var ErrInvalidType = &dbus.Error{"org.freedesktop.DBus.Properties.Error.InvalidType", nil}

// Prop represents a single property. It is used for creating a Properties
// value.
type Prop struct {
	// Initial value. Must be a DBus-representable type.
	Value interface{}

	// If true, the value can be modified by calls to Set.
	Writable bool
}

// Properties is a set of values that can be made available to the message bus
// using the org.freedesktop.DBus.Properties interface.
type Properties struct {
	m   map[string]map[string]Prop
	mut sync.RWMutex
}

// New returns a new Properties structure that manages the given properties.
// The key for the first-level map of props is the name of the interface; the
// second-level key is the name of the property.
func New(props map[string]map[string]Prop) *Properties {
	return &Properties{m: props}
}

func (p *Properties) Get(iface, property string) (dbus.Variant, *dbus.Error) {
	p.mut.RLock()
	defer p.mut.RUnlock()
	m, ok := p.m[iface]
	if !ok {
		return dbus.Variant{}, ErrIfaceNotFound
	}
	prop, ok := m[property]
	if !ok {
		return dbus.Variant{}, ErrPropNotFound
	}
	return dbus.MakeVariant(prop.Value), nil
}

func (p *Properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	p.mut.RLock()
	defer p.mut.RUnlock()
	m, ok := p.m[iface]
	if !ok {
		return nil, ErrIfaceNotFound
	}
	rm := make(map[string]dbus.Variant, len(m))
	for k, v := range m {
		rm[k] = dbus.MakeVariant(v.Value)
	}
	return rm, nil
}

func (p *Properties) GetMust(iface, property string) interface{} {
	p.mut.RLock()
	defer p.mut.RUnlock()
	return p.m[iface][property].Value
}

func (p *Properties) Set(iface, property string, newv dbus.Variant) *dbus.Error {
	p.mut.Lock()
	defer p.mut.Unlock()
	m, ok := p.m[iface]
	if !ok {
		return ErrIfaceNotFound
	}
	prop, ok := m[property]
	if !ok {
		return ErrPropNotFound
	}
	if prop.Writable {
		if dbus.GetSignature(prop.Value) == newv.Signature() {
			m[property] = Prop{newv.Value(), prop.Writable}
		} else {
			return ErrInvalidType
		}
	} else {
		return ErrReadOnly
	}
	return nil
}

func (p *Properties) SetMust(iface, property string, v interface{}) {
	p.mut.Lock()
	defer p.mut.Unlock()
	m := p.m[iface]
	prop := m[property]
	if dbus.GetSignature(prop) != dbus.GetSignature(v) {
		panic(ErrInvalidType)
	}
	m[property] = Prop{v, prop.Writable}
}
