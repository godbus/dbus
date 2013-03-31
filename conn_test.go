package dbus

import "testing"

func TestSessionBus(t *testing.T) {
	_, err := SessionBus()
	if err != nil {
		t.Error(err)
	}
}

func TestSystemBus(t *testing.T) {
	_, err := SystemBus()
	if err != nil {
		t.Error(err)
	}
}
