//go:build linux

package dbus

import (
	"bytes"
	"encoding/binary"
	"syscall"
	"testing"
	"time"
)

func TestUnixTransportFDCredentials(t *testing.T) {
	sender, receiver := fdCleanupSocketPair(t)
	raw, err := receiver.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		optionErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_PASSCRED, 1)
	}); err != nil {
		t.Fatal(err)
	}
	if optionErr != nil {
		t.Fatal(optionErr)
	}
	readers, writers := fdCleanupPipes(t, 1)
	msg := &Message{
		Type: TypeMethodReply,
		Headers: map[HeaderField]Variant{
			FieldReplySerial: MakeVariant(uint32(1)),
			FieldSignature:   MakeVariant(SignatureOf(UnixFDIndex(0))),
			FieldUnixFDs:     MakeVariant(uint32(1)),
		},
		Body: []any{UnixFDIndex(0)}, serial: 1,
	}
	var wire bytes.Buffer
	if err := msg.EncodeTo(&wire, binary.LittleEndian); err != nil {
		t.Fatal(err)
	}
	if err := sender.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	fdCleanupSend(t, sender, wire.Bytes(), []int{int(writers[0].Fd())})
	if err := writers[0].Close(); err != nil {
		t.Fatal(err)
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	tr := newUnixTransportFromConn(receiver).(*unixTransport)
	got, err := tr.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	fd, ok := got.Body[0].(UnixFD)
	if !ok {
		t.Fatalf("body FD has type %T", got.Body[0])
	}
	if err := tr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Close(int(fd)); err != nil {
		t.Fatal(err)
	}
	fdCleanupExpectEOF(t, readers[0])
}

func TestUnixTransportFDTruncatedControlData(t *testing.T) {
	sender, receiver := fdCleanupSocketPair(t)
	readers, writers := fdCleanupPipes(t, 3)
	rights := make([]int, len(writers))
	for i, writer := range writers {
		rights[i] = int(writer.Fd())
	}
	if err := sender.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	fdCleanupSend(t, sender, []byte{'x'}, rights)
	for _, writer := range writers {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	oob := make([]byte, syscall.CmsgLen(4))
	_, oobn, flags, _, err := receiver.ReadMsgUnix(data[:], oob)
	if err != nil {
		t.Fatal(err)
	}
	var rdr oobReader
	defer func() {
		for _, fd := range rdr.fds {
			_ = syscall.Close(fd)
		}
	}()
	_ = rdr.readFDs(oob[:oobn])
	if flags&syscall.MSG_CTRUNC == 0 {
		t.Fatal("expected truncated control data")
	}
	if len(rdr.fds) == 0 {
		t.Fatal("expected at least one received FD")
	}
	for _, fd := range rdr.fds {
		if err := syscall.Close(fd); err != nil {
			t.Fatal(err)
		}
	}
	rdr.fds = nil
	// The kernel disposes of rights that did not fit; our parser must retain
	// every descriptor that did fit so it can also be released.
	for _, reader := range readers {
		fdCleanupExpectEOF(t, reader)
	}
}
