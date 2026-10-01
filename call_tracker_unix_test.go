//go:build !windows && !solaris

package dbus

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestUnixFDReplyCancellationRace(t *testing.T) {
	for _, typ := range []Type{TypeMethodReply, TypeError} {
		for _, order := range []string{"unclaimed", "canceled", "delivered", "racing"} {
			t.Run(typ.String()+"/"+order, func(t *testing.T) {
				n := 1
				if order == "racing" {
					n = 100
				}
				for i := 0; i < n; i++ {
					testUnixFDReplyCancellationRace(t, typ, order)
				}
			})
		}
	}
}

func testUnixFDReplyCancellationRace(t *testing.T, typ Type, order string) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	fd, err := syscall.Dup(int(writer.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	msg := &Message{
		Type: typ,
		Headers: map[HeaderField]Variant{
			FieldReplySerial: MakeVariant(uint32(1)),
			FieldErrorName:   MakeVariant("org.godbus.Test.Error"),
		},
		// Multiple body values reference one received descriptor. Closing
		// each body reference would close the same descriptor more than once.
		Body: []any{UnixFD(fd), MakeVariant([]UnixFD{UnixFD(fd), UnixFD(fd)})},
		fds:  []int{fd},
	}
	defer msg.closeFDs()
	if order == "unclaimed" || order == "canceled" {
		// Discarding must also close received descriptors absent from Body.
		unreferenced, err := syscall.Dup(int(writer.Fd()))
		if err != nil {
			t.Fatal(err)
		}
		msg.fds = append(msg.fds, unreferenced)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	tracker := newCallTracker()
	call := &Call{Done: make(chan *Call, 2)}
	if order != "unclaimed" {
		tracker.track(1, call)
	}
	finish := func() {
		if typ == TypeMethodReply {
			tracker.handleReply(1, msg)
		} else {
			tracker.handleDBusError(1, msg)
		}
	}
	cancel := func() { tracker.finalizeWithError(1, NoSequence, context.Canceled) }
	switch order {
	case "unclaimed":
		finish()
	case "canceled":
		cancel()
		finish()
	case "delivered":
		finish()
		cancel()
	case "racing":
		start := make(chan struct{})
		done := make(chan struct{}, 2)
		go func() { <-start; finish(); done <- struct{}{} }()
		go func() { <-start; cancel(); done <- struct{}{} }()
		close(start)
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for i := 0; i < 2; i++ {
			select {
			case <-done:
			case <-timer.C:
				t.Fatal("call finalization did not finish")
			}
		}
	}
	if order == "unclaimed" {
		checkUnixFDPipeEOF(t, reader)
		return
	}
	var result *Call
	select {
	case result = <-call.Done:
	default:
		t.Fatal("call was not finalized")
	}
	select {
	case <-call.Done:
		t.Fatal("call was finalized more than once")
	default:
	}
	if errors.Is(result.Err, context.Canceled) {
		checkUnixFDPipeEOF(t, reader)
		return
	}
	// If the reply won the race, all references must remain usable by the
	// caller. Cancellation must not close a successfully delivered FD.
	body := result.Body
	if typ == TypeError {
		body = result.Err.(Error).Body
	} else if result.Err != nil {
		t.Fatal(result.Err)
	}
	for _, reference := range append([]UnixFD{body[0].(UnixFD)}, body[1].(Variant).Value().([]UnixFD)...) {
		if _, err := syscall.Write(int(reference), []byte{'x'}); err != nil {
			t.Fatalf("delivered descriptor was closed: %v", err)
		}
	}
	if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [3]byte
	if n, err := reader.Read(data[:]); n != len(data) || err != nil || string(data[:]) != "xxx" {
		t.Fatalf("cannot read delivered data: %q, %v", data[:n], err)
	}
	// The synthetic caller owns the descriptors on successful delivery.
	msg.closeFDs()
	checkUnixFDPipeEOF(t, reader)
}
