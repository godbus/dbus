//go:build !windows && !solaris && !js

package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
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
				if _, err := tr.ReadMessage(); err != io.EOF {
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
			if n, err := reader.Read(data[:]); n != 0 || err != io.EOF {
				t.Errorf("FD retained after ReadMessage failed: n=%d err=%v", n, err)
			}
			if err := tr.Close(); err != nil {
				t.Fatal(err)
			}
			if err := reader.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if n, err := reader.Read(data[:]); n != 0 || err != io.EOF {
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
