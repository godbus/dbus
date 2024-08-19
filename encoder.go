package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"
)

// An encoder encodes values to the D-Bus wire format.
type encoder struct {
	out   io.Writer
	fds   []int
	order binary.ByteOrder
	pos   int

	// This is used to reduce memory allocs.
	intBuff            [8]byte
	intBuffer          *bytes.Buffer
	emptyBuff          [8]byte // This needs to stay all 0's for padding
	childEncoderBuffer *bytes.Buffer
	childEncoder       *encoder
}

// NewEncoder returns a new encoder that writes to out in the given byte order.
func newEncoder(out io.Writer, order binary.ByteOrder, fds []int) *encoder {
	enc := newEncoderAtOffset(out, 0, order, fds)
	return enc
}

// newEncoderAtOffset returns a new encoder that writes to out in the given
// byte order. Specify the offset to initialize pos for proper alignment
// computation.
func newEncoderAtOffset(out io.Writer, offset int, order binary.ByteOrder, fds []int) *encoder {
	enc := new(encoder)
	enc.out = out
	enc.order = order
	enc.pos = offset
	enc.fds = fds
	enc.intBuffer = bytes.NewBuffer(make([]byte, 0, 256))
	return enc
}

func (enc *encoder) Reset(out io.Writer, order binary.ByteOrder, fds []int) {
	enc.out = out
	enc.order = order
	enc.pos = 0
	enc.fds = fds
	enc.intBuffer.Reset()
}

func (enc *encoder) resetEncoderWithOffset(out io.Writer, offset int, order binary.ByteOrder, fds []int) {
	enc.Reset(out, order, fds)
	enc.pos = offset
}

// Aligns the next output to be on a multiple of n. Panics on write errors.
func (enc *encoder) align(n int) {
	pad := enc.padding(0, n)
	if pad > 0 {
		if _, err := enc.out.Write(enc.emptyBuff[:pad]); err != nil {
			panic(err)
		}
		enc.pos += pad
	}
}

// pad returns the number of bytes of padding, based on current position and additional offset.
// and alignment.
func (enc *encoder) padding(offset, algn int) int {
	abs := enc.pos + offset
	if abs%algn != 0 {
		newabs := (abs + algn - 1) & ^(algn - 1)
		return newabs - abs
	}
	return 0
}

// Copied from encoding/binary (stdlib) and modified to return size
func encodeFast(bs []byte, order binary.ByteOrder, data any) int {
	switch v := data.(type) {
	case *bool:
		if *v {
			bs[0] = 1
		} else {
			bs[0] = 0
		}
		return 1
	case bool:
		if v {
			bs[0] = 1
		} else {
			bs[0] = 0
		}
		return 1
	case []bool:
		for i, x := range v {
			if x {
				bs[i] = 1
			} else {
				bs[i] = 0
			}
		}
		return len(v)
	case *int8:
		bs[0] = byte(*v)
		return 1
	case int8:
		bs[0] = byte(v)
		return 1
	case []int8:
		for i, x := range v {
			bs[i] = byte(x)
		}
		return len(v)
	case *uint8:
		bs[0] = *v
		return 1
	case uint8:
		bs[0] = v
		return 1
	case []uint8:
		copy(bs, v)
		return len(v)
	case *int16:
		order.PutUint16(bs, uint16(*v))
		return 2
	case int16:
		order.PutUint16(bs, uint16(v))
		return 2
	case []int16:
		for i, x := range v {
			order.PutUint16(bs[2*i:], uint16(x))
		}
		return 2 * len(v)
	case *uint16:
		order.PutUint16(bs, *v)
		return 2
	case uint16:
		order.PutUint16(bs, v)
		return 2
	case []uint16:
		for i, x := range v {
			order.PutUint16(bs[2*i:], x)
		}
		return 2 * len(v)
	case *int32:
		order.PutUint32(bs, uint32(*v))
		return 4
	case int32:
		order.PutUint32(bs, uint32(v))
		return 4
	case []int32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], uint32(x))
		}
		return 4 * len(v)
	case *uint32:
		order.PutUint32(bs, *v)
		return 4
	case uint32:
		order.PutUint32(bs, v)
		return 4
	case []uint32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], x)
		}
		return 4 * len(v)
	case *int64:
		order.PutUint64(bs, uint64(*v))
		return 8
	case int64:
		order.PutUint64(bs, uint64(v))
		return 8
	case []int64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], uint64(x))
		}
		return 8 * len(v)
	case *uint64:
		order.PutUint64(bs, *v)
		return 8
	case uint64:
		order.PutUint64(bs, v)
		return 8
	case []uint64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], x)
		}
		return 8 * len(v)
	case *float32:
		order.PutUint32(bs, math.Float32bits(*v))
		return 4
	case float32:
		order.PutUint32(bs, math.Float32bits(v))
		return 4
	case []float32:
		for i, x := range v {
			order.PutUint32(bs[4*i:], math.Float32bits(x))
		}
		return 4 * len(v)
	case *float64:
		order.PutUint64(bs, math.Float64bits(*v))
		return 8
	case float64:
		order.PutUint64(bs, math.Float64bits(v))
		return 8
	case []float64:
		for i, x := range v {
			order.PutUint64(bs[8*i:], math.Float64bits(x))
		}
		return 8 * len(v)
	}
	panic("binary.Write: invalid type " + reflect.TypeOf(data).String())
}

func (enc *encoder) binWriteIntType(v interface{}) {
	length := encodeFast(enc.intBuff[:], enc.order, v)
	if _, err := enc.out.Write(enc.intBuff[:length]); err != nil {
		panic(err)
	}
}

// Calls binary.Write(enc.out, enc.order, v) and panics on write errors.
func (enc *encoder) encodeString(str string, strLenSize int) {
	length := len(str)
	if strLenSize == 1 {
		enc.binWriteIntType(byte(length))
	} else {
		enc.binWriteIntType(uint32(length))
	}
	enc.pos += strLenSize
	if enc.intBuffer.Cap() < length+1 {
		enc.intBuffer.Grow(length + 1)
	}
	enc.intBuffer.Reset()
	enc.intBuffer.WriteString(str)
	enc.intBuffer.WriteByte(0)
	n, err := enc.out.Write(enc.intBuffer.Bytes())
	if err != nil {
		panic(err)
	}
	enc.pos += n
}

// Encode encodes the given values to the underlying reader. All written values
// are aligned properly as required by the D-Bus spec.
func (enc *encoder) Encode(vs ...interface{}) (err error) {
	defer func() {
		err, _ = recover().(error)
	}()
	for _, v := range vs {
		enc.encode(reflect.ValueOf(v), 0)
	}
	return nil
}

// encode encodes the given value to the writer and panics on error. depth holds
// the depth of the container nesting.
func (enc *encoder) encode(v reflect.Value, depth int) {
	if depth > 64 {
		panic(FormatError("input exceeds depth limitation"))
	}
	enc.align(alignment(v.Type()))
	switch v.Kind() {
	case reflect.Uint8:
		enc.binWriteIntType(byte(v.Uint()))
		enc.pos++
	case reflect.Bool:
		if v.Bool() {
			enc.binWriteIntType(uint32(1))
		} else {
			enc.binWriteIntType(uint32(1))
		}
		enc.pos += 4
	case reflect.Int16:
		enc.binWriteIntType(int16(v.Int()))
		enc.pos += 2
	case reflect.Uint16:
		enc.binWriteIntType(uint16(v.Uint()))
		enc.pos += 2
	case reflect.Int, reflect.Int32:
		if v.Type() == unixFDType {
			fd := v.Int()
			idx := len(enc.fds)
			enc.fds = append(enc.fds, int(fd))
			enc.binWriteIntType(uint32(idx))
		} else {
			enc.binWriteIntType(int32(v.Int()))
		}
		enc.pos += 4
	case reflect.Uint, reflect.Uint32:
		enc.binWriteIntType(uint32(v.Uint()))
		enc.pos += 4
	case reflect.Int64:
		enc.binWriteIntType(v.Int())
		enc.pos += 8
	case reflect.Uint64:
		enc.binWriteIntType(v.Uint())
		enc.pos += 8
	case reflect.Float64:
		enc.binWriteIntType(v.Float())
		enc.pos += 8
	case reflect.String:
		str := v.String()
		if !utf8.ValidString(str) {
			panic(FormatError("input has a not-utf8 char in string"))
		}
		if strings.IndexByte(str, byte(0)) != -1 {
			panic(FormatError("input has a null char('\\000') in string"))
		}
		if v.Type() == objectPathType {
			if !ObjectPath(str).IsValid() {
				panic(FormatError("invalid object path"))
			}
		}
		enc.encodeString(str, 4)
	case reflect.Ptr:
		enc.encode(v.Elem(), depth)
	case reflect.Slice, reflect.Array:
		// Lookahead offset: 4 bytes for uint32 length (with alignment),
		// plus alignment for elements.
		n := enc.padding(0, 4) + 4
		offset := enc.pos + n + enc.padding(n, alignment(v.Type().Elem()))

		bufenc := enc.childEncoder
		if bufenc == nil {
			buf := bytes.NewBuffer(make([]byte, 0, 256))
			bufenc = newEncoderAtOffset(buf, offset, enc.order, enc.fds)
			enc.childEncoder = bufenc
			enc.childEncoderBuffer = buf
		} else {
			enc.childEncoderBuffer.Reset()
			bufenc.resetEncoderWithOffset(enc.childEncoderBuffer, offset, enc.order, enc.fds)
		}

		for i := 0; i < v.Len(); i++ {
			bufenc.encode(v.Index(i), depth+1)
		}

		if enc.childEncoderBuffer.Len() > 1<<26 {
			panic(FormatError("input exceeds array size limitation"))
		}

		enc.fds = bufenc.fds
		enc.binWriteIntType(uint32(enc.childEncoderBuffer.Len()))
		enc.pos += 4
		length := enc.childEncoderBuffer.Len()
		enc.align(alignment(v.Type().Elem()))
		if _, err := enc.childEncoderBuffer.WriteTo(enc.out); err != nil {
			panic(err)
		}
		enc.pos += length
	case reflect.Struct:
		switch t := v.Type(); t {
		case signatureType:
			str := v.Field(0)
			enc.encodeString(str.String(), 1)
		case variantType:
			variant := v.Interface().(Variant)
			enc.encodeString(variant.sig.String(), 1)
			enc.encode(reflect.ValueOf(variant.value), depth+1)
		default:
			for i := 0; i < v.Type().NumField(); i++ {
				field := t.Field(i)
				if field.PkgPath == "" && field.Tag.Get("dbus") != "-" {
					enc.encode(v.Field(i), depth+1)
				}
			}
		}
	case reflect.Map:
		// Maps are arrays of structures, so they actually increase the depth by
		// 2.
		if !isKeyType(v.Type().Key()) {
			panic(InvalidTypeError{v.Type()})
		}
		// Lookahead offset: 4 bytes for uint32 length (with alignment),
		// plus 8-byte alignment
		n := enc.padding(0, 4) + 4
		offset := enc.pos + n + enc.padding(n, 8)

		bufenc := enc.childEncoder
		if bufenc == nil {
			buf := bytes.NewBuffer(make([]byte, 0, 256))
			bufenc = newEncoderAtOffset(buf, offset, enc.order, enc.fds)
			enc.childEncoder = bufenc
			enc.childEncoderBuffer = buf
		} else {
			enc.childEncoderBuffer.Reset()
			bufenc.resetEncoderWithOffset(enc.childEncoderBuffer, offset, enc.order, enc.fds)
		}
		iter := v.MapRange()
		for iter.Next() {
			bufenc.align(8)
			bufenc.encode(iter.Key(), depth+2)
			bufenc.encode(iter.Value(), depth+2)
		}

		enc.fds = bufenc.fds
		enc.binWriteIntType(uint32(enc.childEncoderBuffer.Len()))
		enc.pos += 4
		length := enc.childEncoderBuffer.Len()
		enc.align(8)
		if _, err := enc.childEncoderBuffer.WriteTo(enc.out); err != nil {
			panic(err)
		}
		enc.pos += length
	case reflect.Interface:
		enc.encode(reflect.ValueOf(MakeVariant(v.Interface())), depth)
	default:
		panic(InvalidTypeError{v.Type()})
	}
}
