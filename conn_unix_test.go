//go:build !windows && !solaris && !darwin && !js

package dbus

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSystemBusPathsOrder(t *testing.T) {
	if len(defaultSystemBusPaths) != 2 {
		t.Fatalf("expected 2 default system bus paths, got %d", len(defaultSystemBusPaths))
	}
	if defaultSystemBusPaths[0] != "/run/dbus/system_bus_socket" {
		t.Errorf("expected first path to be /run/dbus/system_bus_socket, got %s", defaultSystemBusPaths[0])
	}
	if defaultSystemBusPaths[1] != "/var/run/dbus/system_bus_socket" {
		t.Errorf("expected second path to be /var/run/dbus/system_bus_socket, got %s", defaultSystemBusPaths[1])
	}
}

func TestGetSystemBusPlatformAddressFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	runSocket := filepath.Join(tmpDir, "run_socket")
	varRunSocket := filepath.Join(tmpDir, "var_run_socket")

	if err := os.WriteFile(runSocket, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(varRunSocket, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	origPaths := defaultSystemBusPaths
	defaultSystemBusPaths = []string{runSocket, varRunSocket}
	defer func() { defaultSystemBusPaths = origPaths }()

	customAddr := "unix:path=/custom/bus/socket"
	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", customAddr)

	addr := getSystemBusPlatformAddress()
	if addr != customAddr {
		t.Errorf("expected %q, got %q", customAddr, addr)
	}
}

func TestGetSystemBusPlatformAddressRunPriority(t *testing.T) {
	tmpDir := t.TempDir()
	runSocket := filepath.Join(tmpDir, "run_socket")
	varRunSocket := filepath.Join(tmpDir, "var_run_socket")

	// Create both sockets
	l1, err := net.Listen("unix", runSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Close()

	l2, err := net.Listen("unix", varRunSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()

	origPaths := defaultSystemBusPaths
	defaultSystemBusPaths = []string{runSocket, varRunSocket}
	defer func() { defaultSystemBusPaths = origPaths }()

	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "")

	expected := fmt.Sprintf("unix:path=%s", EscapeBusAddressValue(runSocket))
	addr := getSystemBusPlatformAddress()
	if addr != expected {
		t.Errorf("expected priority %q, got %q", expected, addr)
	}

	discovered := tryDiscoverDbusSystemBusAddress()
	if discovered != expected {
		t.Errorf("expected discovered %q, got %q", expected, discovered)
	}
}

func TestGetSystemBusPlatformAddressVarRunFallback(t *testing.T) {
	tmpDir := t.TempDir()
	runSocket := filepath.Join(tmpDir, "non_existent_run_socket")
	varRunSocket := filepath.Join(tmpDir, "var_run_socket")

	l, err := net.Listen("unix", varRunSocket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	origPaths := defaultSystemBusPaths
	defaultSystemBusPaths = []string{runSocket, varRunSocket}
	defer func() { defaultSystemBusPaths = origPaths }()

	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "")

	expected := fmt.Sprintf("unix:path=%s", EscapeBusAddressValue(varRunSocket))
	addr := getSystemBusPlatformAddress()
	if addr != expected {
		t.Errorf("expected fallback %q, got %q", expected, addr)
	}

	discovered := tryDiscoverDbusSystemBusAddress()
	if discovered != expected {
		t.Errorf("expected discovered %q, got %q", expected, discovered)
	}
}

func TestGetSystemBusPlatformAddressNeitherExists(t *testing.T) {
	tmpDir := t.TempDir()
	runSocket := filepath.Join(tmpDir, "non_existent_run_socket")
	varRunSocket := filepath.Join(tmpDir, "non_existent_var_run_socket")

	origPaths := defaultSystemBusPaths
	defaultSystemBusPaths = []string{runSocket, varRunSocket}
	defer func() { defaultSystemBusPaths = origPaths }()

	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "")

	addr := getSystemBusPlatformAddress()
	if addr != defaultSystemBusAddress {
		t.Errorf("expected default fallback %q, got %q", defaultSystemBusAddress, addr)
	}

	discovered := tryDiscoverDbusSystemBusAddress()
	if discovered != "" {
		t.Errorf("expected empty discovery string, got %q", discovered)
	}
}

func TestGetSystemBusPlatformAddressEscapePath(t *testing.T) {
	tmpDir := t.TempDir()
	specialSocket := filepath.Join(tmpDir, "run path with spaces")

	if err := os.WriteFile(specialSocket, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	origPaths := defaultSystemBusPaths
	defaultSystemBusPaths = []string{specialSocket}
	defer func() { defaultSystemBusPaths = origPaths }()

	t.Setenv("DBUS_SYSTEM_BUS_ADDRESS", "")

	expected := fmt.Sprintf("unix:path=%s", EscapeBusAddressValue(specialSocket))
	addr := getSystemBusPlatformAddress()
	if addr != expected {
		t.Errorf("expected escaped path %q, got %q", expected, addr)
	}
}
