package dbus

import (
	"bytes"
	"encoding/binary"
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
		Body: []interface{}{
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

func BenchmarkDecodeArrayEmptyStruct(b *testing.B) {
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
		Body: []interface{}{
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
		b.Fatal(err)
	}
	data := buf.Bytes()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		buf.Write(data)
		_, err = DecodeMessage(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePropertyChanged(b *testing.B) {
	intVal := int32(1)
	buf := bytes.NewBuffer(nil)
	msg := &Message{
		Type:  TypeSignal,
		Flags: 0x00,
		Headers: map[HeaderField]Variant{
			FieldSignature: {
				sig: Signature{
					str: "g",
				},
				value: Signature{
					str: "sa{sv}as",
				},
			},
			FieldInterface: {
				sig: Signature{
					str: "s",
				},
				value: "org.freedesktop.DBus.Properties",
			},
			FieldMember: {
				sig: Signature{
					str: "s",
				},
				value: "PropertiesChanged",
			},
			FieldPath: {
				sig: Signature{
					str: "o",
				},
				value: ObjectPath("/com/github/pboyd/Stress"),
			},
		},
		Body: []interface{}{
			"com.github.pboyd.Stress",
			map[string]Variant{
				"SomeInt": {sig: Signature{str: "i"}, value: &intVal},
			},
			[]string{},
		},
		serial: 0x000029f5,
	}
	err := msg.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	for n := 0; n < b.N; n++ {
		buf.Reset()
		buf.Write(data)
		_, err = DecodeMessage(buf)
		if err != nil {
			b.Fatal(err)
		}
	}
}
