package dbus

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

func TestDecodeUnixFDValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     any
		fds       []int
		want      any
		wantError bool
	}{
		{name: "raw-index", input: UnixFDIndex(1), want: UnixFDIndex(1)},
		{name: "raw-array", input: []UnixFDIndex{0, 1}, want: []UnixFDIndex{0, 1}},
		{name: "scalar", input: UnixFDIndex(1), fds: []int{10, 20}, want: UnixFD(20)},
		{name: "array", input: []UnixFDIndex{0, 1}, fds: []int{10, 20}, want: []UnixFD{10, 20}},
		{name: "nested-array", input: [][]UnixFDIndex{{0, 1}}, fds: []int{10, 20}, want: [][]UnixFD{{10, 20}}},
		{name: "dictionary", input: map[string]UnixFDIndex{"key": 1}, fds: []int{10, 20}, want: map[string]UnixFD{"key": 20}},
		{name: "dictionary-key", input: map[UnixFDIndex]string{1: "value"}, fds: []int{10, 20}, want: map[UnixFD]string{20: "value"}},
		{name: "variant", input: MakeVariant(UnixFDIndex(1)), fds: []int{10, 20}, want: MakeVariant(UnixFD(20))},
		{name: "empty-fds", input: UnixFDIndex(0), fds: []int{}, wantError: true},
		{name: "invalid-scalar", input: UnixFDIndex(1), fds: []int{10}, wantError: true},
		{name: "invalid-array", input: []UnixFDIndex{0, 1}, fds: []int{10}, wantError: true},
		{name: "invalid-variant", input: MakeVariant(UnixFDIndex(1)), fds: []int{10}, wantError: true},
		{name: "largest-index", input: UnixFDIndex(^uint32(0)), fds: []int{10}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wire bytes.Buffer
			enc := newEncoder(&wire, binary.LittleEndian, nil)
			if err := enc.Encode(tc.input); err != nil {
				t.Fatal(err)
			}
			dec := newDecoder(&wire, binary.LittleEndian, tc.fds)
			got, err := dec.Decode(SignatureOf(tc.input))
			if tc.wantError {
				if _, ok := err.(InvalidMessageError); !ok {
					t.Fatalf("Decode error = %v, want InvalidMessageError", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || !reflect.DeepEqual(got[0], tc.want) {
				t.Fatalf("Decode = %#v, want %#v", got, tc.want)
			}
		})
	}
}
