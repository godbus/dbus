//go:build !windows && !solaris && !js

package dbus

import (
	"net"
	"path/filepath"
	"testing"
)

func TestInvalidFramingClosesUnixConnection(t *testing.T) {
	testInvalidFramingClosesConnection(t, func(t *testing.T, wire []byte) transport {
		listener, err := net.ListenUnix("unix", &net.UnixAddr{
			Name: filepath.Join(t.TempDir(), "bus"),
			Net:  "unix",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.Close() })
		peer, err := listener.AcceptUnix()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { peer.Close() })
		go func() {
			defer peer.Close()
			_, _ = peer.Write(wire)
		}()
		return newUnixTransportFromConn(client)
	})
}
