package dbus

import (
	"bytes"
	"encoding/binary"
	"io/ioutil"
	"reflect"
	"testing"
)

var protoTests = []struct {
	vs         []interface{}
	marshalled []byte
}{
	{
		[]interface{}{int32(0)},
		[]byte{0, 0, 0, 0},
	},
	{
		[]interface{}{int16(32)},
		[]byte{0, 32},
	},
	{
		[]interface{}{"foo"},
		[]byte{0, 0, 0, 3, 'f', 'o', 'o', 0},
	},
	{
		[]interface{}{Signature{"ai"}},
		[]byte{2, 'a', 'i', 0},
	},
	{
		[]interface{}{[]int16{42, 256}},
		[]byte{0, 0, 0, 4, 0, 42, 1, 0},
	},
	{
		[]interface{}{int16(42), int32(42)},
		[]byte{0, 42, 0, 0, 0, 0, 0, 42},
	},
	{
		[]interface{}{MakeVariant("foo")},
		[]byte{1, 's', 0, 0, 0, 0, 0, 3, 'f', 'o', 'o', 0},
	},
	{
		[]interface{}{struct {
			A int32
			B int16
		}{10752, 256}},
		[]byte{0, 0, 42, 0, 1, 0},
	},
}

func TestProto(t *testing.T) {
	for i, v := range protoTests {
		buf := new(bytes.Buffer)
		enc := NewEncoder(buf, binary.BigEndian)
		enc.EncodeMulti(v.vs...)
		marshalled := buf.Bytes()
		if bytes.Compare(marshalled, v.marshalled) != 0 {
			t.Errorf("test %d (marshal): got '%v', but expected '%v'\n", i+1, marshalled,
				v.marshalled)
		}
		unmarshalled := reflect.MakeSlice(reflect.TypeOf(v.vs),
			0, 0)
		for i := range v.vs {
			unmarshalled = reflect.Append(unmarshalled,
				reflect.New(reflect.TypeOf(v.vs[i])))
		}
		dec := NewDecoder(bytes.NewReader(v.marshalled), binary.BigEndian)
		unmarshal := reflect.ValueOf(dec).MethodByName("DecodeMulti")
		ret := unmarshal.CallSlice([]reflect.Value{unmarshalled})
		err := ret[0].Interface()
		if err != nil {
			t.Errorf("test %d: %s\n", i+1, err)
			continue
		}
		for j := range v.vs {
			if !reflect.DeepEqual(unmarshalled.Index(j).Elem().Elem().Interface(), v.vs[j]) {
				t.Errorf("test %d (unmarshal): got '%v'/'%T', but expected '%v'/'%T'\n",
					i+1, unmarshalled.Index(j).Elem().Elem().Interface(),
					unmarshalled.Index(j).Elem().Elem().Interface(), v.vs[j], v.vs[j])
			}
		}
	}
}

func TestProtoPointer(t *testing.T) {
	var n *uint32
	buf := bytes.NewBuffer([]byte{42, 1, 0, 0})
	dec := NewDecoder(buf, binary.LittleEndian)
	if err := dec.Decode(&n); err != nil {
		t.Fatal(err)
	}
	if *n != 298 {
		t.Error("pointer test: got", *n)
	}
}

func TestProtoMap(t *testing.T) {
	m := map[string]uint8{
		"foo": 23,
		"bar": 2,
	}
	var n map[string]uint8
	buf := new(bytes.Buffer)
	enc := NewEncoder(buf, binary.LittleEndian)
	enc.Encode(m)
	dec := NewDecoder(buf, binary.LittleEndian)
	dec.Decode(&n)
	if len(n) != 2 || n["foo"] != 23 || n["bar"] != 2 {
		t.Error("map test: got", n)
	}
}

func TestProtoVariantStruct(t *testing.T) {
	var variant Variant
	v := MakeVariant(struct {
		A int32
		B int16
	}{1, 2})
	buf := new(bytes.Buffer)
	enc := NewEncoder(buf, binary.LittleEndian)
	enc.Encode(v)
	dec := NewDecoder(buf, binary.LittleEndian)
	dec.Decode(&variant)
	sl := variant.Value().([]interface{})
	v1, v2 := sl[0].(int32), sl[1].(int16)
	if v1 != int32(1) {
		t.Error("variant struct test: got", v1)
	}
	if v2 != int16(2) {
		t.Error("variant struct test: got", v2)
	}
}

func TestStructTag(t *testing.T) {
	type Bar struct {
		A int32
		B chan interface{} `dbus:"-"`
		C int32
	}
	var bar1, bar2 Bar
	bar1.A = 234
	bar2.C = 345
	buf := new(bytes.Buffer)
	enc := NewEncoder(buf, binary.LittleEndian)
	enc.Encode(bar1)
	dec := NewDecoder(buf, binary.LittleEndian)
	dec.Decode(&bar2)
	if bar1 != bar2 {
		t.Error("struct tag test: got", bar2)
	}
}

var someMessage = &Message{
	Order:  binary.LittleEndian,
	Type:   TypeMethodCall,
	Flags:  0,
	serial: 42,
	Headers: map[HeaderField]Variant{
		FieldPath:        MakeVariant(ObjectPath("/foo/bar")),
		FieldInterface:   MakeVariant("foo.bar"),
		FieldMember:      MakeVariant("Baz"),
		FieldDestination: MakeVariant("foo.bar"),
		FieldSignature:   MakeVariant(Signature{"ai(uu)"}),
	},
	Body: []interface{}{
		[]int32{12643, 17888, 20569},
		struct{ n, m uint32 }{0, 1},
	},
}

func BenchmarkDecodeMessage(b *testing.B) {
	var err error
	var rd *bytes.Reader

	b.StopTimer()
	buf := new(bytes.Buffer)
	err = someMessage.EncodeTo(buf)
	if err != nil {
		b.Fatal(err)
	}
	decoded := buf.Bytes()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		rd = bytes.NewReader(decoded)
		_, err = DecodeMessage(rd)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeMessage(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		err = someMessage.EncodeTo(ioutil.Discard)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetSignature(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetSignature(someMessage.Body...)
	}
}

func BenchmarkSignatureValues(b *testing.B) {
	s := Signature{"a(bits)a{sv}"}
	for i := 0; i < b.N; i++ {
		s.Values()
	}
}
