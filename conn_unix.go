//go:build !windows && !solaris && !darwin && !js

package dbus

import (
	"fmt"
	"net"
	"os"
)

const defaultSystemBusAddress = "unix:path=/var/run/dbus/system_bus_socket"

// defaultSystemBusPaths lists default system bus socket locations in order of preference.
var defaultSystemBusPaths = []string{
	"/run/dbus/system_bus_socket",
	"/var/run/dbus/system_bus_socket",
}

// tryDiscoverDbusSystemBusAddress checks default paths for an existing system bus socket.
// It returns the bus address if found, or an empty string.
func tryDiscoverDbusSystemBusAddress() string {
	for _, p := range defaultSystemBusPaths {
		if fileExists(p) {
			return fmt.Sprintf("unix:path=%s", EscapeBusAddressValue(p))
		}
	}
	return ""
}

func getSystemBusPlatformAddress() string {
	address := os.Getenv("DBUS_SYSTEM_BUS_ADDRESS")
	if address != "" {
		return address
	}
	if address := tryDiscoverDbusSystemBusAddress(); address != "" {
		return address
	}
	return defaultSystemBusAddress
}

// DialUnix establishes a new private connection to the message bus specified by UnixConn.
func DialUnix(conn *net.UnixConn, opts ...ConnOption) (*Conn, error) {
	tr := newUnixTransportFromConn(conn)
	return newConn(tr, opts...)
}

func ConnectUnix(uconn *net.UnixConn, opts ...ConnOption) (*Conn, error) {
	conn, err := DialUnix(uconn, opts...)
	if err != nil {
		return nil, err
	}
	if err = conn.Auth(conn.auth); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err = conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
