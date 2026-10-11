//go:build godbus_no_tcp

package dbus

import "testing"

func TestNoTcpTransport(t *testing.T) {
	for _, addr := range []string{"tcp:host=127.0.0.1,port=1", "nonce-tcp:host=127.0.0.1,port=1"} {
		if _, err := Dial(addr); err == nil {
			t.Errorf("%s: expected error, got nil", addr)
		}
	}
}
