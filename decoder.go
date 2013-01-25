package dbus

import (
	"encoding/binary"
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

func (dec *Decoder) align(n int) error {
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
	return nil
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
		}
	}()
	dec.decode(reflect.ValueOf(v))
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

func (dec *Decoder) decode(v reflect.Value) {
	if v.Kind() != reflect.Ptr {
		panic("(*dbus.Decoder): parameter is not a pointer")
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
		dec.decode(reflect.ValueOf(&i))
		if i == 0 {
			v.SetBool(false)
		} else {
			// XXX: official spec recommends to only accept 0 or 1; should we
			// return an error here?
			v.SetBool(true)
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
		dec.decode(reflect.ValueOf(&length))
		b := make([]byte, int(length)+1)
		if _, err := dec.in.Read(b); err != nil {
			panic(err)
		}
		dec.pos += int(length) + 1
		v.SetString(string(b[:len(b)-1]))
	case reflect.Ptr:
		nv := reflect.New(v.Type().Elem())
		dec.decode(nv)
		v.Set(nv)
	case reflect.Slice:
		var length uint32
		dec.decode(reflect.ValueOf(&length))
		slice := reflect.MakeSlice(v.Type(), 0, 0)
		spos := dec.pos
		for dec.pos < spos+int(length) {
			nv := reflect.New(v.Type().Elem())
			dec.decode(nv)
			slice = reflect.Append(slice, nv.Elem())
		}
		v.Set(slice)
	case reflect.Struct:
		switch t := v.Type(); t {
		case variantType:
			var variant Variant
			var sig Signature
			dec.decode(reflect.ValueOf(&sig))
			variant.sig = sig
			if len(sig.str) == 0 {
				panic(SignatureError{sig.str, "signature is empty"})
			}
			err, rem := validSingle(sig.str)
			if err != nil {
				panic(err)
			}
			if rem != "" {
				panic(SignatureError{sig.str, "got multiple types, but expected one"})
			}
			t = value(sig.str)
			if t == interfacesType {
				dec.align(8)
				s := sig.str[1 : len(sig.str)-1]
				slice := reflect.MakeSlice(t, 0, 0)
				for len(s) != 0 {
					err, rem := validSingle(s)
					if err != nil {
						panic(err)
					}
					t = value(s[:len(s)-len(rem)])
					nv := reflect.New(t)
					dec.decode(nv)
					slice = reflect.Append(slice, nv.Elem())
					s = rem
				}
				variant.value = slice.Interface()
			} else {
				nv := reflect.New(t)
				dec.decode(nv)
				variant.value = nv.Elem().Interface()
			}
			v.Set(reflect.ValueOf(variant))
		case signatureType:
			var length uint8
			dec.decode(reflect.ValueOf(&length))
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

					dec.decode(v.Field(i).Addr())
				}
			}
		}
	case reflect.Map:
		var length uint32
		dec.decode(reflect.ValueOf(&length))
		m := reflect.MakeMap(v.Type())
		spos := dec.pos
		for dec.pos < spos+int(length) {
			dec.align(8)
			if !isKeyType(v.Type().Key()) {
				panic(invalidTypeError{v.Type()})
			}
			kv := reflect.New(v.Type().Key())
			vv := reflect.New(v.Type().Elem())
			dec.decode(kv)
			dec.decode(vv)
			m.SetMapIndex(kv.Elem(), vv.Elem())
		}
		v.Set(m)
	default:
		panic(invalidTypeError{v.Type()})
	}
}
