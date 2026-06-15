package dbus

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// TestStartSocketpair tests bidirectional method calls over a socketpair
// using Start() to skip authentication.
func TestStartSocketpair(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}

	fileA := os.NewFile(uintptr(fds[0]), "a")
	fileB := os.NewFile(uintptr(fds[1]), "b")

	connA, err := NewConn(fileA)
	if err != nil {
		fileA.Close()
		fileB.Close()
		t.Fatalf("NewConn A: %v", err)
	}
	connA.Start()

	connB, err := NewConn(fileB)
	if err != nil {
		connA.Close()
		fileB.Close()
		t.Fatalf("NewConn B: %v", err)
	}
	connB.Start()

	defer connA.Close()
	defer connB.Close()

	// B exports a method that A can call
	handler := &testP2PHandler{}
	if err := connB.Export(handler, "/test", "com.example.Test"); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A calls B's method
	call := connA.Object("", "/test").Call("com.example.Test.Echo", 0, "hello")
	if call.Err != nil {
		t.Fatalf("Call: %v", call.Err)
	}

	var result string
	if err := call.Store(&result); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}

	// verify bidirectional: A exports, B calls
	if err := connA.Export(handler, "/test", "com.example.Test"); err != nil {
		t.Fatalf("Export on A: %v", err)
	}

	call = connB.Object("", "/test").Call("com.example.Test.Echo", 0, "world")
	if call.Err != nil {
		t.Fatalf("Call B->A: %v", call.Err)
	}
	if err := call.Store(&result); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if result != "world" {
		t.Errorf("got %q, want %q", result, "world")
	}
}

type testP2PHandler struct{}

func (h *testP2PHandler) Echo(msg string) (string, *Error) {
	return msg, nil
}
