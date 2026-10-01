//go:build !windows && !solaris

package dbus

import "syscall"

// closeFDs releases descriptors from a received message that was not delivered.
// Use the received list, not Body: multiple body values can reference the same
// descriptor, and some received descriptors might not be referenced at all.
func (msg *Message) closeFDs() {
	for _, fd := range msg.fds {
		_ = syscall.Close(fd)
	}
	msg.fds = nil
}
