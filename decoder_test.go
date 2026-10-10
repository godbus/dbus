package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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

type panicErrorReader struct {
	err error
}

func (r panicErrorReader) Read([]byte) (int, error) {
	panic(r.err)
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

// TestDecodeErrorPanic checks that a panic with an error value
// from an underlying reader is not swallowed by Decode.
func TestDecodeErrorPanic(t *testing.T) {
	expectedErr := errors.New("boom error")
	defer func() {
		v := recover()
		if v != expectedErr {
			t.Fatalf("expected panic %v, got %v", expectedErr, v)
		}
	}()
	dec := newDecoder(panicErrorReader{err: expectedErr}, binary.LittleEndian, nil)
	vs, err := dec.Decode(Signature{"u"})
	t.Fatalf("expected panic, got %v, %v", vs, err)
}

// TestDecodeEOFPanic checks that a panic with io.EOF is propagated
// rather than translated to a FormatError return.
func TestDecodeEOFPanic(t *testing.T) {
	defer func() {
		v := recover()
		if v != io.EOF {
			t.Fatalf("expected panic %v, got %v", io.EOF, v)
		}
	}()
	dec := newDecoder(panicErrorReader{err: io.EOF}, binary.LittleEndian, nil)
	vs, err := dec.Decode(Signature{"u"})
	t.Fatalf("expected panic, got %v, %v", vs, err)
}

// TestDecodeNormalReaderError checks that standard (non-panicking) reader errors
// are returned gracefully by Decode.
func TestDecodeNormalReaderError(t *testing.T) {
	expectedErr := errors.New("read failed")
	dec := newDecoder(errorReader{err: expectedErr}, binary.LittleEndian, nil)
	_, err := dec.Decode(Signature{"u"})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected error %v, got %v", expectedErr, err)
	}
}

// TestDecodeNormalEOF checks that standard (non-panicking) EOF
// is converted to FormatError("unexpected EOF").
func TestDecodeNormalEOF(t *testing.T) {
	dec := newDecoder(errorReader{err: io.EOF}, binary.LittleEndian, nil)
	_, err := dec.Decode(Signature{"u"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var fmtErr FormatError
	if !errors.As(err, &fmtErr) {
		t.Fatalf("expected FormatError, got %T: %v", err, err)
	}
}

