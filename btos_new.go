//go:build go1.20
// +build go1.20

package dbus

import "unsafe"

// toString converts a byte slice to a string without allocating.
func toString(b []byte) string {
	return unsafe.String(&b[0], len(b))
}
