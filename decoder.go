package dbus

import (
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"unicode"
)

// A Decoder reads values that are encoded in the DBus wire format.
type Decoder struct {
	in    io.Reader
	order binary.ByteOrder
	pos   int
}

// NewDecoder returns a new decoder that reads values from in. The input is
// expected to be in the given byte order.
func NewDecoder(in io.Reader, order binary.ByteOrder) *Decoder {
	dec := new(Decoder)
	dec.in = in
	dec.order = order
	return dec
}

// align aligns the input to the given boundary and panics on error.
func (dec *Decoder) align(n int) {
	newpos := dec.pos
	if newpos%n != 0 {
		newpos += (n - (newpos % n))
		empty := make([]byte, newpos-dec.pos)
		_, err := dec.in.Read(empty)
		if err != nil {
			panic(err)
		}
		dec.pos = newpos
	}
}

// Calls binary.Read(dec.in, dec.order, v) and panics on read errors.
func (dec *Decoder) binread(v interface{}) {
	if err := binary.Read(dec.in, dec.order, v); err != nil {
		panic(err)
	}
}

// Decode decodes a single value from the decoder and stores it
// in v. If v isn't a pointer, Decode panics. For the details of decoding,
// see the package-level documentation.
//
// The input is expected to be aligned as required by the DBus spec.
func (dec *Decoder) Decode(v interface{}) (err error) {
	defer func() {
		if err, ok := recover().(error); ok {
			if _, ok := err.(invalidTypeError); ok {
				panic(err)
			}
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = FormatError("input too short (unexpected EOF)")
			}
		}
	}()
	dec.decode(reflect.ValueOf(v), 0)
	return nil
}

// DecodeMulti is a shorthand for decoding multiple values.
func (dec *Decoder) DecodeMulti(vs ...interface{}) error {
	for _, v := range vs {
		if err := dec.Decode(v); err != nil {
			return err
		}
	}
	return nil
}

// decode decodes a single value and stores it in *v. depth holds the depth of
// the container nesting.
func (dec *Decoder) decode(v reflect.Value, depth int) {
	if v.Kind() != reflect.Ptr {
		panic(invalidTypeError{v.Type()})
	}

	v = v.Elem()
	dec.align(alignment(v.Type()))
	switch v.Kind() {
	case reflect.Uint8:
		b := make([]byte, 1)
		if _, err := dec.in.Read(b); err != nil {
			panic(err)
		}
		dec.pos++
		v.SetUint(uint64(b[0]))
	case reflect.Bool:
		var i uint32
		dec.decode(reflect.ValueOf(&i), depth)
		switch {
		case i == 0:
			v.SetBool(false)
		case i == 1:
			v.SetBool(true)
		default:
			panic(errors.New("invalid value for boolean"))
		}
	case reflect.Int16:
		var i int16
		dec.binread(&i)
		dec.pos += 2
		v.SetInt(int64(i))
	case reflect.Int32:
		var i int32
		dec.binread(&i)
		dec.pos += 4
		v.SetInt(int64(i))
	case reflect.Int64:
		var i int64
		dec.binread(&i)
		dec.pos += 8
		v.SetInt(i)
	case reflect.Uint16:
		var i uint16
		dec.binread(&i)
		dec.pos += 2
		v.SetUint(uint64(i))
	case reflect.Uint32:
		var i uint32
		dec.binread(&i)
		dec.pos += 4
		v.SetUint(uint64(i))
	case reflect.Uint64:
		var i uint64
		dec.binread(&i)
		dec.pos += 8
		v.SetUint(i)
	case reflect.Float64:
		var f float64
		dec.binread(&f)
		dec.pos += 8
		v.SetFloat(f)
	case reflect.String:
		var length uint32
		dec.decode(reflect.ValueOf(&length), depth)
		b := make([]byte, int(length)+1)
		if _, err := dec.in.Read(b); err != nil {
			panic(err)
		}
		dec.pos += int(length) + 1
		v.SetString(string(b[:len(b)-1]))
	case reflect.Ptr:
		nv := reflect.New(v.Type().Elem())
		dec.decode(nv, depth)
		v.Set(nv)
	case reflect.Slice:
		var length uint32
		if depth >= 64 {
			panic(FormatError("input exceeds container depth limit"))
		}
		dec.decode(reflect.ValueOf(&length), depth)
		slice := reflect.MakeSlice(v.Type(), 0, 0)
		spos := dec.pos
		for dec.pos < spos+int(length) {
			nv := reflect.New(v.Type().Elem())
			dec.decode(nv, depth)
			slice = reflect.Append(slice, nv.Elem())
		}
		v.Set(slice)
	case reflect.Struct:
		if depth >= 64 {
			panic(FormatError("input exceeds container depth limit"))
		}
		switch t := v.Type(); t {
		case variantType:
			var variant Variant
			var sig Signature
			dec.decode(reflect.ValueOf(&sig), depth)
			variant.sig = sig
			if len(sig.str) == 0 {
				panic(FormatError("variant signature is empty"))
			}
			err, rem := validSingle(sig.str, 0)
			if err != nil {
				panic(FormatError(err.Error()))
			}
			if rem != "" {
				panic(FormatError("variant signature has multiple types"))
			}
			t = value(sig.str)
			if t == interfacesType {
				dec.align(8)
				s := sig.str[1 : len(sig.str)-1]
				slice := reflect.MakeSlice(t, 0, 0)
				for len(s) != 0 {
					err, rem := validSingle(s, 0)
					if err != nil {
						panic(FormatError(err.Error()))
					}
					t = value(s[:len(s)-len(rem)])
					nv := reflect.New(t)
					dec.decode(nv, depth+1)
					slice = reflect.Append(slice, nv.Elem())
					s = rem
				}
				variant.value = slice.Interface()
			} else {
				nv := reflect.New(t)
				dec.decode(nv, depth+1)
				variant.value = nv.Elem().Interface()
			}
			v.Set(reflect.ValueOf(variant))
		case signatureType:
			var length uint8
			dec.decode(reflect.ValueOf(&length), depth)
			b := make([]byte, int(length)+1)
			if _, err := dec.in.Read(b); err != nil {
				panic(err)
			}
			dec.pos += int(length) + 1
			sig, err := StringToSig(string(b[:len(b)-1]))
			if err != nil {
				panic(err)
			}
			v.Set(reflect.ValueOf(sig))
		default:
			for i := 0; i < v.NumField(); i++ {
				field := t.Field(i)
				if unicode.IsUpper([]rune(field.Name)[0]) &&
					field.Tag.Get("dbus") != "-" {

					dec.decode(v.Field(i).Addr(), depth+1)
				}
			}
		}
	case reflect.Map:
		var length uint32
		dec.decode(reflect.ValueOf(&length), depth)
		m := reflect.MakeMap(v.Type())
		spos := dec.pos
		for dec.pos < spos+int(length) {
			dec.align(8)
			if !isKeyType(v.Type().Key()) {
				panic(invalidTypeError{v.Type()})
			}
			kv := reflect.New(v.Type().Key())
			vv := reflect.New(v.Type().Elem())
			dec.decode(kv, depth+1)
			dec.decode(vv, depth+1)
			m.SetMapIndex(kv.Elem(), vv.Elem())
		}
		v.Set(m)
	default:
		panic(invalidTypeError{v.Type()})
	}
}

// A FormatError represents an error in the wire format (e.g. an invalid value
// for a boolean).
type FormatError string

func (e FormatError) Error() string {
	return "dbus format error: " + string(e)
}
