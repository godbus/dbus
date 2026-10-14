//go:build !windows && !solaris && !js && !wasip1

package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestUnixTransportFDReadErrors(t *testing.T) {
	for _, scenario := range []string{"partial-header", "partial-body", "invalid-byte-order", "invalid-body", "unsupported", "success"} {
		t.Run(scenario, func(t *testing.T) {
			sender, receiver := fdCleanupSocketPair(t)
			tr := newUnixTransportFromConn(receiver).(*unixTransport)
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			msg := &Message{
				Type: TypeMethodReply,
				Headers: map[HeaderField]Variant{
					FieldReplySerial: MakeVariant(uint32(1)),
					FieldSignature:   MakeVariant(SignatureOf(UnixFD(0))),
					FieldUnixFDs:     MakeVariant(uint32(1)),
				},
				Body:   []any{UnixFD(writer.Fd())},
				serial: 1,
			}
			var buffer bytes.Buffer
			if _, err := msg.EncodeToWithFDs(&buffer, binary.LittleEndian); err != nil {
				t.Fatal(err)
			}
			wire := buffer.Bytes()
			switch scenario {
			case "partial-header":
				wire = wire[:1]
			case "partial-body":
				wire = wire[:len(wire)-1]
			case "invalid-byte-order":
				wire[0] = 'x'
			case "invalid-body":
				// A Unix FD value must occupy four bytes; advertise and send only one.
				binary.LittleEndian.PutUint32(wire[4:8], 1)
				wire = wire[:len(wire)-3]
			case "unsupported":
				tr.hasUnixFDs = false
			}
			if err := sender.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, _, err := sender.WriteMsgUnix(wire, syscall.UnixRights(int(writer.Fd())), nil); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := sender.Close(); err != nil {
				t.Fatal(err)
			}
			if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			got, err := tr.ReadMessage()
			if scenario == "success" {
				if err != nil {
					t.Fatal(err)
				}
				fd := int(got.Body[0].(UnixFD))
				defer syscall.Close(fd)
				// Neither a later failed read nor connection close may revoke delivery.
				if _, err := tr.ReadMessage(); !errors.Is(err, io.EOF) {
					t.Fatalf("next read: %v", err)
				}
				if err := tr.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := syscall.Write(fd, []byte{'x'}); err != nil {
					t.Fatalf("delivered FD closed: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected a read or decode error")
			}
			if err := reader.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			var data [1]byte
			if n, err := reader.Read(data[:]); n != 0 || !errors.Is(err, io.EOF) {
				t.Errorf("FD retained after ReadMessage failed: n=%d err=%v", n, err)
			}
			if err := tr.Close(); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if n, err := reader.Read(data[:]); n != 0 || !errors.Is(err, io.EOF) {
				t.Errorf("FD retained after transport close: n=%d err=%v", n, err)
			}
		})
	}
}

func fdCleanupSocketPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	conns := make([]*net.UnixConn, 0, 2)
	for _, fd := range fds {
		file := os.NewFile(uintptr(fd), "socket")
		conn, err := net.FileConn(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conns = append(conns, conn.(*net.UnixConn))
	}
	return conns[0], conns[1]
}

func TestUnixTransportFDOwnership(t *testing.T) {
	fd := UnixFDIndex(0)
	second := UnixFDIndex(1)
	scalarFD := func(msg *Message) []UnixFD { return []UnixFD{msg.Body[0].(UnixFD)} }
	arrayFDs := func(msg *Message) []UnixFD { return msg.Body[0].([]UnixFD) }
	variantFD := func(msg *Message) []UnixFD { return []UnixFD{msg.Body[0].(Variant).Value().(UnixFD)} }
	structFD := func(msg *Message) []UnixFD { return []UnixFD{msg.Body[0].([]any)[0].(UnixFD)} }
	for _, tc := range []struct {
		name      string
		body      []any
		count     uint32
		sent      int
		omitCount bool
		split     bool
		wantError bool
		wantFDs   []int
		getFDs    func(*Message) []UnixFD
		signature string
	}{
		{name: "missing-header", body: []any{"text"}, sent: 1, omitCount: true},
		{name: "zero-count", body: []any{"text"}, sent: 1},
		{name: "unreferenced", body: []any{"text"}, count: 2, sent: 2},
		{name: "extra-rights", body: []any{fd}, count: 1, sent: 2, wantFDs: []int{0}, getFDs: scalarFD},
		{name: "unused-declared", body: []any{fd}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: scalarFD},
		{name: "multiple-reads", body: []any{[]UnixFDIndex{fd, second}}, count: 2, sent: 2, split: true, wantFDs: []int{0, 1}, getFDs: arrayFDs},
		{name: "repeated-index", body: []any{[]UnixFDIndex{fd, fd}}, count: 1, sent: 1, wantFDs: []int{0, 0}, getFDs: arrayFDs},
		{name: "variant", body: []any{MakeVariant(fd)}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: variantFD},
		{name: "struct", body: []any{struct{ FD UnixFDIndex }{fd}}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: structFD},
		{name: "nested-array", body: []any{[][]UnixFDIndex{{fd, second}}}, count: 2, sent: 2, wantFDs: []int{0, 1}, getFDs: func(msg *Message) []UnixFD { return msg.Body[0].([][]UnixFD)[0] }},
		{name: "dictionary", body: []any{map[string]UnixFDIndex{"key": fd}}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: func(msg *Message) []UnixFD { return []UnixFD{msg.Body[0].(map[string]UnixFD)["key"]} }},
		{name: "dictionary-key", body: []any{map[UnixFDIndex]string{fd: "value"}}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: func(msg *Message) []UnixFD {
			for key := range msg.Body[0].(map[UnixFD]string) {
				return []UnixFD{key}
			}
			return nil
		}},
		{name: "nested-variant", body: []any{MakeVariant(struct{ FD Variant }{MakeVariant(fd)})}, count: 2, sent: 2, wantFDs: []int{0}, getFDs: func(msg *Message) []UnixFD {
			return []UnixFD{msg.Body[0].(Variant).Value().([]any)[0].(Variant).Value().(UnixFD)}
		}},
		{name: "overwritten-dictionary-entry", body: []any{[]struct {
			Key string
			FD  UnixFDIndex
		}{{"key", fd}, {"key", second}}}, signature: "a{sh}", count: 2, sent: 2, wantFDs: []int{1}, getFDs: func(msg *Message) []UnixFD {
			return []UnixFD{msg.Body[0].(map[string]UnixFD)["key"]}
		}},
		{name: "overstated-count", body: []any{second}, count: 2, sent: 1, wantError: true},
		{name: "invalid-scalar", body: []any{second}, count: 1, sent: 1, wantError: true},
		{name: "invalid-array", body: []any{[]UnixFDIndex{fd, second}}, count: 1, sent: 1, wantError: true},
		{name: "invalid-variant", body: []any{MakeVariant(second)}, count: 1, sent: 1, wantError: true},
		{name: "invalid-struct", body: []any{struct{ FD UnixFDIndex }{second}}, count: 1, sent: 1, wantError: true},
		{name: "invalid-dictionary", body: []any{map[string]UnixFDIndex{"key": second}}, count: 1, sent: 1, wantError: true},
		{name: "largest-index", body: []any{UnixFDIndex(^uint32(0))}, count: 1, sent: 1, wantError: true},
		{name: "index-without-header", body: []any{fd}, sent: 1, omitCount: true, wantError: true},
		{name: "index-with-zero-count", body: []any{fd}, sent: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender, receiver := fdCleanupSocketPair(t)
			tr := newUnixTransportFromConn(receiver).(*unixTransport)
			readers, writers := fdCleanupPipes(t, tc.sent)
			rights := make([]int, len(writers))
			for i, writer := range writers {
				rights[i] = int(writer.Fd())
			}
			msg := &Message{
				Type: TypeMethodReply,
				Headers: map[HeaderField]Variant{
					FieldReplySerial: MakeVariant(uint32(1)),
					FieldSignature:   MakeVariant(SignatureOf(tc.body...)),
				},
				Body: tc.body, serial: 1,
			}
			if !tc.omitCount {
				msg.Headers[FieldUnixFDs] = MakeVariant(tc.count)
			}
			if tc.signature != "" {
				sig, err := ParseSignature(tc.signature)
				if err != nil {
					t.Fatal(err)
				}
				msg.Headers[FieldSignature] = MakeVariant(sig)
			}
			var wire bytes.Buffer
			if err := msg.EncodeTo(&wire, binary.LittleEndian); err != nil {
				t.Fatal(err)
			}
			if err := sender.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if tc.split {
				// ReadMessage reads the fixed header separately, so these rights
				// must arrive through distinct recvmsg calls.
				fdCleanupSend(t, sender, wire.Bytes()[:16], rights[:1])
				fdCleanupSend(t, sender, wire.Bytes()[16:], rights[1:])
			} else {
				fdCleanupSend(t, sender, wire.Bytes(), rights)
			}
			for _, writer := range writers {
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if err := sender.Close(); err != nil {
				t.Fatal(err)
			}
			if err := receiver.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			got, err := tr.ReadMessage()
			if tc.wantError {
				if _, ok := err.(InvalidMessageError); !ok || got != nil {
					t.Fatalf("ReadMessage = %v, %v; want InvalidMessageError and no message", got, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var delivered []UnixFD
			if tc.getFDs != nil && got != nil {
				delivered = tc.getFDs(got)
			}
			if len(delivered) != len(tc.wantFDs) {
				t.Fatalf("delivered %d FDs, want %d", len(delivered), len(tc.wantFDs))
			}
			owned := make(map[UnixFD]bool)
			for _, fd := range delivered {
				if !owned[fd] {
					fd := fd
					owned[fd] = true
					t.Cleanup(func() { _ = syscall.Close(int(fd)) })
				}
			}
			kept := make(map[int]bool)
			for _, index := range tc.wantFDs {
				kept[index] = true
			}
			for i, reader := range readers {
				if !kept[i] {
					fdCleanupExpectEOF(t, reader)
				}
			}
			if _, err := tr.ReadMessage(); !errors.Is(err, io.EOF) {
				t.Fatalf("next read: %v", err)
			}
			if err := tr.Close(); err != nil {
				t.Fatal(err)
			}
			for i, fd := range delivered {
				if _, err := syscall.Write(int(fd), []byte{'x'}); err != nil {
					t.Fatalf("delivered FD closed: %v", err)
				}
				reader := readers[tc.wantFDs[i]]
				if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				var data [1]byte
				if _, err := io.ReadFull(reader, data[:]); err != nil || data[0] != 'x' {
					t.Fatalf("delivered FD targets wrong pipe: %q, %v", data, err)
				}
			}
		})
	}
}

func TestUnixTransportFDControlData(t *testing.T) {
	for _, scenario := range []string{"multiple-rights", "other-control-message", "malformed-tail", "truncated-rights", "partial-fd"} {
		t.Run(scenario, func(t *testing.T) {
			readers, writers := fdCleanupPipes(t, 2)
			// Duplicate actual descriptors: the parser's ownership is separate
			// from os.File, just as for rights installed by recvmsg.
			rights := make([]int, 2)
			for i, writer := range writers {
				fd, err := syscall.Dup(int(writer.Fd()))
				if err != nil {
					t.Fatal(err)
				}
				rights[i] = fd
				i := i
				t.Cleanup(func() {
					if rights[i] >= 0 {
						_ = syscall.Close(rights[i])
					}
				})
			}
			first, last := syscall.UnixRights(rights[0]), syscall.UnixRights(rights[1])
			wantError := false
			switch scenario {
			case "other-control-message":
				other := syscall.UnixRights()
				(*syscall.Cmsghdr)(unsafe.Pointer(&other[0])).Type = -1
				first = append(other, first...)
			case "malformed-tail":
				(*syscall.Cmsghdr)(unsafe.Pointer(&last[0])).SetLen(syscall.CmsgLen(0) - 1)
				wantError = true
			case "truncated-rights":
				(*syscall.Cmsghdr)(unsafe.Pointer(&last[0])).SetLen(len(last) + 8)
				last = last[:syscall.CmsgLen(4)]
				wantError = true
			case "partial-fd":
				last = append(last[:syscall.CmsgLen(4)], byte(0))
				(*syscall.Cmsghdr)(unsafe.Pointer(&last[0])).SetLen(len(last))
				wantError = true
			}
			var rdr oobReader
			err := rdr.readFDs(append(first, last...))
			if (err != nil) != wantError {
				t.Fatalf("readFDs error = %v, want error %v", err, wantError)
			}
			wantCount := 2
			if scenario == "malformed-tail" {
				wantCount = 1
			}
			if len(rdr.fds) != wantCount {
				t.Fatalf("collected %d FDs, want %d", len(rdr.fds), wantCount)
			}
			for i, fd := range rdr.fds {
				if fd != rights[i] {
					t.Fatalf("FD %d = %d, want %d", i, fd, rights[i])
				}
				if err := syscall.Close(fd); err != nil {
					t.Fatal(err)
				}
				rights[i] = -1
				if err := writers[i].Close(); err != nil {
					t.Fatal(err)
				}
				fdCleanupExpectEOF(t, readers[i])
			}
		})
	}
}

func fdCleanupPipes(t *testing.T, count int) ([]*os.File, []*os.File) {
	t.Helper()
	readers, writers := make([]*os.File, count), make([]*os.File, count)
	for i := range readers {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { reader.Close(); writer.Close() })
		readers[i], writers[i] = reader, writer
	}
	return readers, writers
}

func fdCleanupExpectEOF(t *testing.T, reader *os.File) {
	t.Helper()
	if err := reader.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if n, err := reader.Read(data[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("FD retained: n=%d err=%v", n, err)
	}
}

func fdCleanupSend(t *testing.T, sender *net.UnixConn, wire []byte, fds []int) {
	t.Helper()
	if n, _, err := sender.WriteMsgUnix(wire, syscall.UnixRights(fds...), nil); err != nil || n != len(wire) {
		t.Fatalf("send: n=%d err=%v", n, err)
	}
}
