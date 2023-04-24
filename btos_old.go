//go:build !go1.20
// +build !go1.20

package dbus

import (
	"reflect"
	"unsafe"
)

// toString converts a byte slice to a string without allocating.
func toString(b []byte) string {
	var s string
	h := (*reflect.StringHeader)(unsafe.Pointer(&s))
	h.Data = uintptr(unsafe.Pointer(&b[0]))
	h.Len = len(b)

	return s
}
