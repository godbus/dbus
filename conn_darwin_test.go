package dbus

import "testing"

func TestSystemBusAddressFromLaunchdSocket(t *testing.T) {
	t.Setenv("DBUS_LAUNCHD_SESSION_BUS_SOCKET", "/tmp/dbus-test-socket")
	addr, err := getSystemBusPlatformAddress()
	if err != nil {
		t.Fatal(err)
	}
	if addr != "unix:path=/tmp/dbus-test-socket" {
		t.Fatalf("address %q", addr)
	}
}
