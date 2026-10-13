//go:build !windows && !solaris

package dbus

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestUnixFDReplyOwnership(t *testing.T) {
	for _, typ := range []Type{TypeMethodReply, TypeError} {
		for _, canceled := range []bool{false, true} {
			name := typ.String() + "/delivered"
			if canceled {
				name = typ.String() + "/canceled"
			}
			t.Run(name, func(t *testing.T) {
				server, err := ConnectSessionBus()
				if err != nil {
					t.Fatal(err)
				}
				defer server.Close()
				// A signal sent after the reply is a barrier for the input
				// worker: the preceding reply has already been dispatched.
				barrier := make(chan struct{}, 1)
				client, err := ConnectSessionBus(WithIncomingInterceptor(func(msg *Message) {
					if msg.Type == TypeSignal && msg.Headers[FieldMember].Value() == "ReplySent" {
						barrier <- struct{}{}
					}
				}))
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				defer writer.Close()

				// As in TestStateCachingProxyPattern, dispatch manually so
				// the reply can be withheld without a blocked service method.
				requests := make(chan *Message, 8)
				server.Eavesdrop(requests)
				callCtx, cancelCall := context.WithCancel(ctx)
				defer cancelCall()
				call := client.Object(server.Names()[0], "/test").GoWithContext(callCtx, "org.godbus.Test.GetFD", 0, nil)
				var request *Message
				for request == nil {
					select {
					case msg, ok := <-requests:
						if !ok {
							t.Fatal("server disconnected")
						}
						if msg.Type == TypeMethodCall {
							request = msg
						}
					case <-ctx.Done():
						t.Fatal("server did not receive the request")
					}
				}
				if canceled {
					cancelCall()
					select {
					case result := <-call.Done:
						if !errors.Is(result.Err, context.Canceled) {
							t.Fatalf("expected cancellation, got %v", result.Err)
						}
					case <-ctx.Done():
						t.Fatal("call did not cancel")
					}
				}
				reply := &Message{
					Type: typ,
					Headers: map[HeaderField]Variant{
						FieldDestination: request.Headers[FieldSender],
						FieldReplySerial: MakeVariant(request.Serial()),
						FieldSignature:   MakeVariant(SignatureOf(UnixFD(0))),
					},
					Body: []any{UnixFD(writer.Fd())},
				}
				if typ == TypeError {
					reply.Headers[FieldErrorName] = MakeVariant("org.godbus.Test.Error")
				}
				if err := server.sendMessageAndIfClosed(reply, nil); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				if err := server.sendMessageAndIfClosed(&Message{
					Type: TypeSignal,
					Headers: map[HeaderField]Variant{
						FieldDestination: request.Headers[FieldSender],
						FieldPath:        MakeVariant(ObjectPath("/test")),
						FieldInterface:   MakeVariant("org.godbus.Test"),
						FieldMember:      MakeVariant("ReplySent"),
					},
				}, nil); err != nil {
					t.Fatal(err)
				}
				select {
				case <-barrier:
				case <-ctx.Done():
					t.Fatal("client did not process the reply barrier")
				}
				if !canceled {
					select {
					case result := <-call.Done:
						body := result.Body
						if typ == TypeError {
							dbusErr, ok := result.Err.(Error)
							if !ok || dbusErr.Name != "org.godbus.Test.Error" {
								t.Fatalf("unexpected error: %v", result.Err)
							}
							body = dbusErr.Body
						} else if result.Err != nil {
							t.Fatal(result.Err)
						}
						received := os.NewFile(uintptr(body[0].(UnixFD)), "received")
						defer received.Close()
						// Closing the connection must not revoke caller ownership.
						if err := client.Close(); err != nil {
							t.Fatal(err)
						}
						if _, err := received.Write([]byte{'x'}); err != nil {
							t.Fatalf("delivered descriptor was closed: %v", err)
						}
						if err := received.Close(); err != nil {
							t.Fatal(err)
						}
						var data [1]byte
						if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
							t.Fatal(err)
						}
						if _, err := io.ReadFull(reader, data[:]); err != nil || data[0] != 'x' {
							t.Fatalf("cannot read delivered data: %q, %v", data, err)
						}
					case <-ctx.Done():
						t.Fatal("call did not complete")
					}
				}
				checkUnixFDPipeEOF(t, reader)
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
				checkUnixFDPipeEOF(t, reader)
			})
		}
	}
}

func checkUnixFDPipeEOF(t *testing.T, reader *os.File) {
	t.Helper()
	if err := reader.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [1]byte
	if n, err := reader.Read(data[:]); n != 0 || err != io.EOF {
		t.Errorf("expected EOF after releasing the reply's descriptors, got %d bytes, %v", n, err)
	}
}
