package dbus

import (
	"encoding/binary"
	"io"
	"reflect"
)

type decoder struct {
	in    io.Reader
	order binary.ByteOrder
	pos   int
}

// newDecoder returns a new decoder that reads values from in. The input is
// expected to be in the given byte order.
func newDecoder(in io.Reader, order binary.ByteOrder) *decoder {
	dec := new(decoder)
	dec.in = in
	dec.order = order
	return dec
}

// align aligns the input to the given boundary and panics on error.
func (dec *decoder) align(n int) {
	if dec.pos%n != 0 {
		newpos := (dec.pos + n - 1) & ^(n - 1)
		empty := make([]byte, newpos-dec.pos)
		if _, err := io.ReadFull(dec.in, empty); err != nil {
			panic(err)
		}
		dec.pos = newpos
	}
}

// Calls binary.Read(dec.in, dec.order, v) and panics on read errors.
func (dec *decoder) binread(v interface{}) {
	if err := binary.Read(dec.in, dec.order, v); err != nil {
		panic(err)
	}
}

// Decode decodes values from the decoder and stores them in the locations
// pointed to by vs. If one element of vs isn't a pointer, Decode panics. For
// the details of decoding, see the documentation of Decoder.
//
// The input is expected to be aligned as required by the D-Bus spec.
func (dec *decoder) Decode(vs ...interface{}) (err error) {
	defer func() {
		var ok bool
		if err, ok = recover().(error); ok {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = FormatError("unexpected EOF")
			}
		}
	}()
	for _, v := range vs {
		dec.decode(reflect.ValueOf(v), 0)
	}
	return nil
}

func (dec *decoder) DecodeSig(sig Signature) (vs []interface{}, err error) {
	defer func() {
		var ok bool
		v := recover()
		if err, ok = v.(error); ok {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				err = FormatError("unexpected EOF")
			}
		}
	}()
	vs = make([]interface{}, 0)
	s := sig.str
	for s != "" {
		err, rem := validSingle(s, 0)
		if err != nil {
			return nil, err
		}
		v := dec.decodeSig(s[:len(s)-len(rem)], 0)
		vs = append(vs, v)
		s = rem
	}
	return vs, nil
}

func (dec *decoder) decodeSig(s string, depth int) interface{} {
	if len(s) == 1 {
		if t, ok := sigToType[s[0]]; ok {
			v := reflect.New(t)
			dec.decode(v, depth)
			return v.Elem().Interface()
		}
	}
	switch s[0] {
	case 'a':
		if len(s) > 1 && s[1] == '{' {
			ksig := s[2:3]
			vsig := s[3 : len(s)-1]
			v := reflect.MakeMap(reflect.MapOf(typeFor(ksig), typeFor(vsig)))
			if depth >= 63 {
				panic(FormatError("input exceeds container depth limit"))
			}
			var length uint32
			dec.decode(reflect.ValueOf(&length), depth)
			spos := dec.pos
			for dec.pos < spos+int(length) {
				dec.align(8)
				if !isKeyType(v.Type().Key()) {
					panic(InvalidTypeError{v.Type()})
				}
				kv := dec.decodeSig(ksig, depth+2)
				vv := dec.decodeSig(vsig, depth+2)
				v.SetMapIndex(reflect.ValueOf(kv), reflect.ValueOf(vv))
			}
			return v.Interface()
		}
		var length uint32
		if depth >= 64 {
			panic(FormatError("input exceeds container depth limit"))
		}
		dec.decode(reflect.ValueOf(&length), depth)
		v := reflect.MakeSlice(reflect.SliceOf(typeFor(s[1:])), 0, int(length))
		spos := dec.pos
		for dec.pos < spos+int(length) {
			ev := dec.decodeSig(s[1:], depth+1)
			v = reflect.Append(v, reflect.ValueOf(ev))
		}
		return v.Interface()
	case '(':
		dec.align(8)
		v := make([]interface{}, 0)
		for _, c := range s[1 : len(s)-1] {
			ev := dec.decodeSig(string(c), depth+1)
			v = append(v, ev)
		}
		return v
	default:
		panic(SignatureError{Sig: s})
	}
}

// decode decodes a single value and stores it in *v. depth holds the depth of
// the container nesting.
func (dec *decoder) decode(v reflect.Value, depth int) {
	if v.Kind() != reflect.Ptr {
		panic(InvalidTypeError{v.Type()})
	}

	v = v.Elem()
	dec.align(alignment(v.Type()))
	switch v.Kind() {
	case reflect.Uint8:
		var b [1]byte
		if _, err := dec.in.Read(b[:]); err != nil {
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
			panic(FormatError("invalid value for boolean"))
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
		if _, err := io.ReadFull(dec.in, b); err != nil {
			panic(err)
		}
		dec.pos += int(length) + 1
		v.SetString(string(b[:len(b)-1]))
	case reflect.Ptr:
		if v.IsNil() {
			nv := reflect.New(v.Type().Elem())
			dec.decode(nv, depth)
			v.Set(nv)
		} else {
			dec.decode(v, depth)
		}
	case reflect.Slice:
		var length uint32
		if depth >= 64 {
			panic(FormatError("input exceeds container depth limit"))
		}
		dec.decode(reflect.ValueOf(&length), depth)
		if v.IsNil() {
			v.Set(reflect.MakeSlice(v.Type(), 0, int(length)))
		}
		spos := dec.pos
		nv := reflect.New(v.Type().Elem())
		for dec.pos < spos+int(length) {
			dec.decode(nv, depth)
			v.Set(reflect.Append(v, nv.Elem()))
		}
	case reflect.Struct:
		if depth >= 64 && v.Type() != signatureType {
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
				panic(err)
			}
			if rem != "" {
				panic(FormatError("variant signature has multiple types"))
			}
			t = typeFor(sig.str)
			if t == interfacesType {
				dec.align(8)
				s := sig.str[1 : len(sig.str)-1]
				slice := reflect.MakeSlice(t, 0, 0)
				for len(s) != 0 {
					err, rem := validSingle(s, 0)
					if err != nil {
						panic(err)
					}
					t = typeFor(s[:len(s)-len(rem)])
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
			if _, err := io.ReadFull(dec.in, b); err != nil {
				panic(err)
			}
			dec.pos += int(length) + 1
			sig, err := ParseSignature(string(b[:len(b)-1]))
			if err != nil {
				panic(err)
			}
			v.Set(reflect.ValueOf(sig))
		default:
			for i := 0; i < v.NumField(); i++ {
				field := t.Field(i)
				if field.PkgPath == "" && field.Tag.Get("dbus") != "-" {
					dec.decode(v.Field(i).Addr(), depth+1)
				}
			}
		}
	case reflect.Map:
		// Maps are arrays of structures, so they actually increase the depth by
		// 2.
		if depth >= 63 {
			panic(FormatError("input exceeds container depth limit"))
		}
		var length uint32
		dec.decode(reflect.ValueOf(&length), depth)
		m := reflect.MakeMap(v.Type())
		spos := dec.pos
		kv := reflect.New(v.Type().Key())
		vv := reflect.New(v.Type().Elem())
		for dec.pos < spos+int(length) {
			dec.align(8)
			if !isKeyType(v.Type().Key()) {
				panic(InvalidTypeError{v.Type()})
			}
			dec.decode(kv, depth+2)
			dec.decode(vv, depth+2)
			m.SetMapIndex(kv.Elem(), vv.Elem())
		}
		v.Set(m)
	default:
		panic(InvalidTypeError{v.Type()})
	}
}

// A FormatError is an error in the wire format.
type FormatError string

func (e FormatError) Error() string {
	return "dbus: wire format error: " + string(e)
}
