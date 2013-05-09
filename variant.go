package dbus

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
)

// Variant represents the DBus variant type.
type Variant struct {
	sig   Signature
	value interface{}
}

// MakeVariant converts the given value to a Variant. It panics if v cannot be
// represented as a DBus type.
func MakeVariant(v interface{}) Variant {
	return Variant{SignatureOf(v), v}
}

// format returns a formatted version of v and whether this string can be parsed
// unambigously.
func (v Variant) format() (string, bool) {
	switch v.sig.str[0] {
	case 'b', 'i':
		return fmt.Sprint(v.value), true
	case 'n', 'q', 'u', 'x', 't', 'd', 'h':
		return fmt.Sprint(v.value), false
	case 's':
		return strconv.Quote(v.value.(string)), true
	case 'o':
		return strconv.Quote(string(v.value.(ObjectPath))), false
	case 'g':
		return strconv.Quote(v.value.(Signature).str), false
	case 'v':
		s, unamb := v.value.(Variant).format()
		if !unamb {
			return "<@" + v.value.(Variant).sig.str + " " + s + ">", true
		}
		return "<" + s + ">", true
	case 'y':
		return fmt.Sprintf("%#x", v.value.(byte)), false
	}
	rv := reflect.ValueOf(v.value)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.Len() == 0 {
			return "[]", false
		}
		unamb := true
		buf := bytes.NewBuffer([]byte("["))
		for i := 0; i < rv.Len(); i++ {
			// TODO: slooow
			s, b := MakeVariant(rv.Index(i).Interface()).format()
			unamb = unamb && b
			buf.WriteString(s)
			if i != rv.Len()-1 {
				buf.WriteString(", ")
			}
		}
		buf.WriteByte(']')
		return buf.String(), unamb
	case reflect.Map:
		if rv.Len() == 0 {
			return "{}", false
		}
		unamb := true
		buf := bytes.NewBuffer([]byte("{"))
		for i, k := range rv.MapKeys() {
			s, b := MakeVariant(k.Interface()).format()
			unamb = unamb && b
			buf.WriteString(s)
			buf.WriteString(": ")
			s, b = MakeVariant(rv.MapIndex(k).Interface()).format()
			unamb = unamb && b
			buf.WriteString(s)
			if i != rv.Len()-1 {
				buf.WriteString(", ")
			}
		}
		buf.WriteByte('}')
		return buf.String(), unamb
	}
	return `"INVALID"`, true
}

// Signature returns the DBus signature of the underlying value of v.
func (v Variant) Signature() Signature {
	return v.sig
}

// String returns the string representation of the underlying value of v as
// described at https://developer.gnome.org/glib/unstable/gvariant-text.html.
func (v Variant) String() string {
	s, unamb := v.format()
	if !unamb {
		return "@" + v.sig.str + " " + s
	}
	return s
}

// Value returns the underlying value of v.
func (v Variant) Value() interface{} {
	return v.value
}
