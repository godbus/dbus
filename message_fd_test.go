package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"testing"
)

func TestDecodeMessageUnixFDs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		input    any
		resolved any
		empty    bool
	}{
		{name: "scalar", input: UnixFDIndex(1), resolved: UnixFD(20)},
		{name: "array", input: []UnixFDIndex{0, 1}, resolved: []UnixFD{10, 20}},
		{name: "nested-array", input: [][]UnixFDIndex{{1}}, resolved: [][]UnixFD{{20}}},
		{name: "dictionary", input: map[string]UnixFDIndex{"key": 1}, resolved: map[string]UnixFD{"key": 20}},
		{name: "variant", input: MakeVariant(UnixFDIndex(1)), resolved: MakeVariant(UnixFD(20))},
		{name: "empty-array", input: []UnixFDIndex{}, resolved: []UnixFD{}, empty: true},
	} {
		for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
			t.Run(tc.name+"/"+order.String(), func(t *testing.T) {
				msg := &Message{
					Type: TypeMethodReply,
					Headers: map[HeaderField]Variant{
						FieldReplySerial: MakeVariant(uint32(1)),
						FieldSignature:   MakeVariant(SignatureOf(tc.input)),
						FieldUnixFDs:     MakeVariant(uint32(2)),
					},
					Body:   []any{tc.input},
					serial: 1,
				}
				var wire bytes.Buffer
				if err := msg.EncodeTo(&wire, order); err != nil {
					t.Fatal(err)
				}
				for _, mode := range []struct {
					name      string
					decode    func(io.Reader) (*Message, error)
					want      any
					wantError bool
				}{
					{name: "raw", decode: DecodeMessage, want: tc.input},
					{name: "nil-fds", decode: func(r io.Reader) (*Message, error) {
						return DecodeMessageWithFDs(r, nil)
					}, want: tc.input},
					{name: "resolved", decode: func(r io.Reader) (*Message, error) {
						return DecodeMessageWithFDs(r, []int{10, 20})
					}, want: tc.resolved},
					{name: "empty-fds", decode: func(r io.Reader) (*Message, error) {
						return DecodeMessageWithFDs(r, []int{})
					}, want: tc.resolved, wantError: !tc.empty},
				} {
					t.Run(mode.name, func(t *testing.T) {
						got, err := mode.decode(bytes.NewReader(wire.Bytes()))
						if mode.wantError {
							if _, ok := err.(InvalidMessageError); !ok || got != nil {
								t.Fatalf("decode = %v, %v; want no message and InvalidMessageError", got, err)
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						if len(got.Body) != 1 || !reflect.DeepEqual(got.Body[0], mode.want) {
							t.Fatalf("body = %#v, want %#v", got.Body, []any{mode.want})
						}
					})
				}
			})
		}
	}
}
