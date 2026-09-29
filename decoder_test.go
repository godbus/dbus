package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"runtime"
	"testing"
)

type pixmap struct {
	Width  int
	Height int
	Pixels []uint8
}

type property struct {
	IconName    string
	Pixmaps     []pixmap
	Title       string
	Description string
}

func TestDecodeArrayEmptyStruct(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	msg := &Message{
		Type:  0x02,
		Flags: 0x00,
		Headers: map[HeaderField]Variant{
			0x06: {
				sig: Signature{
					str: "s",
				},
				value: ":1.391",
			},
			0x05: {
				sig: Signature{
					str: "u",
				},
				value: uint32(2),
			},
			0x08: {
				sig: Signature{
					str: "g",
				},
				value: Signature{
					str: "v",
				},
			},
		},
		Body: []any{
			Variant{
				sig: Signature{
					str: "(sa(iiay)ss)",
				},
				value: property{
					IconName:    "iconname",
					Pixmaps:     []pixmap{},
					Title:       "title",
					Description: "description",
				},
			},
		},
		serial: 0x00000003,
	}
	err := msg.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeMessage(buf)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSigByteSize(t *testing.T) {
	for sig, want := range map[string]int{
		"b":       4,
		"t":       8,
		"(yy)":    2,
		"(y(uu))": 9,
		"(y(xs))": 0,
		"s":       0,
		"ao":      0,
	} {
		if have := sigByteSize(sig); have != want {
			t.Errorf("sigByteSize(%q) = %d, want %d", sig, have, want)
		}
	}
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) {
	panic("boom")
}

// TestDecodeNonErrorPanic checks that a panic with a non-error value
// is not swallowed by Decode.
func TestDecodeNonErrorPanic(t *testing.T) {
	defer func() {
		if v := recover(); v != "boom" {
			t.Fatalf("expected panic %q, got %v", "boom", v)
		}
	}()
	dec := newDecoder(panicReader{}, binary.LittleEndian, nil)
	vs, err := dec.Decode(Signature{"u"})
	t.Fatalf("expected panic, got %v, %v", vs, err)
}

func TestReadFull(t *testing.T) {
	data := make([]byte, 5*readChunk+123)
	for i := range data {
		data[i] = byte(i)
	}
	for _, n := range []int{0, 1, readChunk - 1, readChunk, readChunk + 1, 2 * readChunk, len(data)} {
		for _, buf := range [][]byte{nil, make([]byte, 10), make([]byte, 0, 3*readChunk)} {
			got, err := readFull(bytes.NewReader(data), buf, n)
			if err != nil {
				t.Fatalf("n=%d: %v", n, err)
			}
			if !bytes.Equal(got, data[:n]) {
				t.Fatalf("n=%d: data mismatch", n)
			}
		}
	}
	// Short input.
	for _, n := range []int{2, readChunk + 1, 3 * readChunk} {
		_, err := readFull(bytes.NewReader(data[:n-1]), nil, n)
		if err != io.ErrUnexpectedEOF {
			t.Errorf("n=%d: expected %v, got %v", n, io.ErrUnexpectedEOF, err)
		}
	}
	_, err := readFull(bytes.NewReader(nil), nil, 3*readChunk)
	if err != io.EOF {
		t.Errorf("expected %v, got %v", io.EOF, err)
	}
}

// TestDecodeHugeLength checks that decoding a value with a bogus huge
// length, which is not backed by the actual data, does not result in
// a huge memory allocation.
func TestDecodeHugeLength(t *testing.T) {
	for _, tc := range []struct {
		sig  string
		data []byte
	}{
		{"s", []byte{0xf0, 0xff, 0xff, 0xff, 'a', 'b', 'c'}},
		{"ay", []byte{0x00, 0x00, 0xff, 0x03, 1, 2, 3}},
		{"ad", []byte{0x00, 0x00, 0xff, 0x03, 0, 0, 0, 0, 1, 2, 3}},
	} {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		dec := newDecoder(bytes.NewReader(tc.data), binary.LittleEndian, nil)
		_, err := dec.Decode(Signature{tc.sig})
		runtime.ReadMemStats(&after)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.sig)
		}
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 1<<20 {
			t.Errorf("%s: allocated %d bytes", tc.sig, alloc)
		}
	}
}

func TestDecodeArrayTooLong(t *testing.T) {
	for _, sig := range []string{"ay", "a{yy}"} {
		data := []byte{0x01, 0x00, 0x00, 0x04} // 1<<26 + 1
		dec := newDecoder(bytes.NewReader(data), binary.LittleEndian, nil)
		_, err := dec.Decode(Signature{sig})
		if err != FormatError("input exceeds array size limitation") {
			t.Errorf("%s: unexpected error: %v", sig, err)
		}
	}
}
