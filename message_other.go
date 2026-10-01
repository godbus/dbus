//go:build windows || solaris

package dbus

// These platforms do not have a transport that receives Unix file descriptors.
func (msg *Message) closeFDs() {}
