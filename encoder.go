package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"unicode"
)

/*
An Encoder encodes values to the DBus wire format. Rules for encoding are as
follows:

1. Any primitive Go type that has a direct equivalent in the wire format
is directly converted. This includes all fixed size integers
except for int8, as well as float64, bool and string.

2. Slices and maps are converted to arrays and dicts, respectively.

3. Most structs are converted to the expected DBus struct. The
exceptions are all types and structs defined in this package
that have a custom wire format. These are ObjectPath, Signature
and Variant. Also, fields whose tag contains dbus:"-" will be skipped.

4. Trying to encode any other type (including int and uint!) will result
in a panic.
*/
type Encoder struct {
	buf   *bytes.Buffer
	out   io.Writer
	order binary.ByteOrder
	pos   int
}

// NewEncoder returns a new encoder that writes to out in the given
// byte order.
func NewEncoder(out io.Writer, order binary.ByteOrder) *Encoder {
	enc := new(Encoder)
	enc.buf = new(bytes.Buffer)
	enc.out = out
	enc.order = order
	return enc
}

func (enc *Encoder) align(n int) {
	newpos := enc.pos
	if newpos%n != 0 {
		newpos += (n - (newpos % n))
	}
	empty := make([]byte, newpos-enc.pos)
	enc.buf.Write(empty)
	enc.pos = newpos
}

// Encode encodes a single value to the underyling reader. All written values
// are aligned properly as required by the DBus spec.
func (enc *Encoder) Encode(v interface{}) error {
	enc.encode(reflect.ValueOf(v))
	return enc.flush()
}

// Encode is a shorthand for multiple Encode calls.
func (enc *Encoder) EncodeMulti(vs ...interface{}) error {
	for _, v := range vs {
		enc.encode(reflect.ValueOf(v))
	}
	return enc.flush()
}

// Encode the given value to the internal buffer.
func (enc *Encoder) encode(v reflect.Value) {
	n := alignment(v.Type())
	if n != -1 {
		enc.align(n)
	}
	switch v.Kind() {
	case reflect.Uint8:
		enc.buf.Write([]byte{byte(v.Uint())})
		enc.pos++
	case reflect.Bool:
		if v.Bool() {
			enc.encode(reflect.ValueOf(uint32(1)))
		} else {
			enc.encode(reflect.ValueOf(uint32(0)))
		}
	case reflect.Int16:
		binary.Write(enc.buf, enc.order, int16(v.Int()))
		enc.pos += 2
	case reflect.Uint16:
		binary.Write(enc.buf, enc.order, uint16(v.Uint()))
		enc.pos += 2
	case reflect.Int32:
		binary.Write(enc.buf, enc.order, int32(v.Int()))
		enc.pos += 4
	case reflect.Uint32:
		binary.Write(enc.buf, enc.order, uint32(v.Uint()))
		enc.pos += 4
	case reflect.Int64:
		binary.Write(enc.buf, enc.order, int64(v.Int()))
		enc.pos += 8
	case reflect.Uint64:
		binary.Write(enc.buf, enc.order, uint64(v.Uint()))
		enc.pos += 8
	case reflect.Float64:
		binary.Write(enc.buf, enc.order, v.Float())
		enc.pos += 8
	case reflect.String:
		enc.encode(reflect.ValueOf(uint32(len(v.String()))))
		n, _ := enc.buf.Write([]byte(v.String()))
		enc.buf.Write([]byte{0})
		enc.pos += n + 1
	case reflect.Ptr:
		enc.encode(v.Elem())
	case reflect.Slice, reflect.Array:
		buf := new(bytes.Buffer)
		bufenc := NewEncoder(buf, enc.order)

		for i := 0; i < v.Len(); i++ {
			bufenc.encode(v.Index(i))
		}
		bufenc.flush()
		enc.encode(reflect.ValueOf(uint32(buf.Len())))
		length := buf.Len()
		enc.align(alignment(v.Type().Elem()))
		buf.WriteTo(enc.buf)
		enc.pos += length
	case reflect.Struct:
		switch t := v.Type(); t {
		case signatureType:
			str := v.Field(0)
			enc.encode(reflect.ValueOf(byte(str.Len())))
			n, _ := enc.buf.Write([]byte(str.String()))
			enc.buf.Write([]byte{0})
			enc.pos += n + 1
		case variantType:
			variant := v.Interface().(Variant)
			enc.encode(reflect.ValueOf(variant.sig))
			enc.encode(reflect.ValueOf(variant.value))
		default:
			for i := 0; i < v.Type().NumField(); i++ {
				field := t.Field(i)
				if unicode.IsUpper([]rune(field.Name)[0]) &&
					field.Tag.Get("dbus") != "-" {

					enc.encode(v.Field(i))
				}
			}
		}
	case reflect.Map:
		keys := v.MapKeys()
		buf := new(bytes.Buffer)
		bufenc := NewEncoder(buf, enc.order)
		for _, k := range keys {
			bufenc.align(8)
			bufenc.encode(k)
			bufenc.encode(v.MapIndex(k))
		}
		bufenc.flush()
		enc.encode(reflect.ValueOf(uint32(buf.Len())))
		length := buf.Len()
		enc.align(8)
		buf.WriteTo(enc.buf)
		enc.pos += length
	default:
		panic("(*dbus.Encoder): can't encode " + v.Type().String())
	}
}

func (enc *Encoder) flush() error {
	expected := enc.buf.Len()
	n, err := io.Copy(enc.out, enc.buf)
	defer enc.buf.Reset()
	if err != nil {
		return err
	}
	if int(n) != expected {
		return io.ErrShortWrite
	}
	return nil
}
