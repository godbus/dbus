package dbus

import "testing"

func TestSessionBus(t *testing.T) {
	_, err := ConnectSessionBus()
	if err != nil {
		t.Error(err)
	}
}

func TestSystemBus(t *testing.T) {
	_, err := ConnectSystemBus()
	if err != nil {
		t.Error(err)
	}
}
