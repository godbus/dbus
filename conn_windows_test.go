package dbus

import "testing"

func TestSystemBusAddressFromEnv(t *testing.T) {
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "tcp:host=127.0.0.1,port=1")
	addr, err := getSystemBusPlatformAddress()
	if err != nil {
		t.Fatal(err)
	}
	if addr != "tcp:host=127.0.0.1,port=1" {
		t.Fatalf("address %q", addr)
	}
}
