package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"reflect"
	"testing"
)

var protoTests = []struct {
	vs           []interface{}
	bigEndian    []byte
	littleEndian []byte
}{
	{
		[]interface{}{int32(0)},
		[]byte{0, 0, 0, 0},
		[]byte{0, 0, 0, 0},
	},
	{
		[]interface{}{true, false},
		[]byte{0, 0, 0, 1, 0, 0, 0, 0},
		[]byte{1, 0, 0, 0, 0, 0, 0, 0},
	},
	{
		[]interface{}{byte(0), uint16(12), int16(32), uint32(43)},
		[]byte{0, 0, 0, 12, 0, 32, 0, 0, 0, 0, 0, 43},
		[]byte{0, 0, 12, 0, 32, 0, 0, 0, 43, 0, 0, 0},
	},
	{
		[]interface{}{int64(-1), uint64(1<<64 - 1)},
		bytes.Repeat([]byte{255}, 16),
		bytes.Repeat([]byte{255}, 16),
	},
	{
		[]interface{}{math.Inf(+1)},
		[]byte{0x7f, 0xf0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 0, 0, 0, 0, 0, 0xf0, 0x7f},
	},
	{
		[]interface{}{"foo"},
		[]byte{0, 0, 0, 3, 'f', 'o', 'o', 0},
		[]byte{3, 0, 0, 0, 'f', 'o', 'o', 0},
	},
	{
		[]interface{}{Signature{"ai"}},
		[]byte{2, 'a', 'i', 0},
		[]byte{2, 'a', 'i', 0},
	},
	{
		[]interface{}{[]int16{42, 256}},
		[]byte{0, 0, 0, 4, 0, 42, 1, 0},
		[]byte{4, 0, 0, 0, 42, 0, 0, 1},
	},
	{
		[]interface{}{MakeVariant("foo")},
		[]byte{1, 's', 0, 0, 0, 0, 0, 3, 'f', 'o', 'o', 0},
		[]byte{1, 's', 0, 0, 3, 0, 0, 0, 'f', 'o', 'o', 0},
	},
	{
		[]interface{}{MakeVariant(MakeVariant(Signature{"v"}))},
		[]byte{1, 'v', 0, 1, 'g', 0, 1, 'v', 0},
		[]byte{1, 'v', 0, 1, 'g', 0, 1, 'v', 0},
	},
	{
		[]interface{}{map[int32]bool{42: true}},
		[]byte{0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 42, 0, 0, 0, 1},
		[]byte{8, 0, 0, 0, 0, 0, 0, 0, 42, 0, 0, 0, 1, 0, 0, 0},
	},
	{
		[]interface{}{map[string]Variant{}, byte(42)},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
	},
	{
		[]interface{}{[]uint64{}, byte(42)},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
		[]byte{0, 0, 0, 0, 0, 0, 0, 0, 42},
	},
	{
		[]interface{}{uint16(1), true},
		[]byte{0, 1, 0, 0, 0, 0, 0, 1},
		[]byte{1, 0, 0, 0, 1, 0, 0, 0},
	},
}

func TestProto(t *testing.T) {
	for i, v := range protoTests {
		buf := new(bytes.Buffer)
		fds := make([]int, 0)
		bigEnc := newEncoder(buf, binary.BigEndian, fds)
		if err := bigEnc.Encode(v.vs...); err != nil {
			t.Fatal(err)
		}
		marshalled := buf.Bytes()
		if !bytes.Equal(marshalled, v.bigEndian) {
			t.Errorf("test %d (marshal be): got '%v', but expected '%v'\n", i+1, marshalled,
				v.bigEndian)
		}
		buf.Reset()
		fds = make([]int, 0)
		litEnc := newEncoder(buf, binary.LittleEndian, fds)
		if err := litEnc.Encode(v.vs...); err != nil {
			t.Fatal(err)
		}
		marshalled = buf.Bytes()
		if !bytes.Equal(marshalled, v.littleEndian) {
			t.Errorf("test %d (marshal le): got '%v', but expected '%v'\n", i+1, marshalled,
				v.littleEndian)
		}
		unmarshalled := reflect.MakeSlice(reflect.TypeOf(v.vs),
			0, 0)
		for i := range v.vs {
			unmarshalled = reflect.Append(unmarshalled,
				reflect.New(reflect.TypeOf(v.vs[i])))
		}
		bigDec := newDecoder(bytes.NewReader(v.bigEndian), binary.BigEndian, make([]int, 0))
		vs, err := bigDec.Decode(SignatureOf(v.vs...))
		if err != nil {
			t.Errorf("test %d (unmarshal be): %s\n", i+1, err)
			continue
		}
		if !reflect.DeepEqual(vs, v.vs) {
			t.Errorf("test %d (unmarshal be): got %#v, but expected %#v\n", i+1, vs, v.vs)
		}
		litDec := newDecoder(bytes.NewReader(v.littleEndian), binary.LittleEndian, make([]int, 0))
		vs, err = litDec.Decode(SignatureOf(v.vs...))
		if err != nil {
			t.Errorf("test %d (unmarshal le): %s\n", i+1, err)
			continue
		}
		if !reflect.DeepEqual(vs, v.vs) {
			t.Errorf("test %d (unmarshal le): got %#v, but expected %#v\n", i+1, vs, v.vs)
		}

	}
}

func TestProtoMap(t *testing.T) {
	m := map[string]uint8{
		"foo": 23,
		"bar": 2,
	}
	var n map[string]uint8
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"a{sy}"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &n); err != nil {
		t.Fatal(err)
	}
	if len(n) != 2 || n["foo"] != 23 || n["bar"] != 2 {
		t.Error("got", n)
	}
}

func TestProtoVariantStruct(t *testing.T) {
	var variant Variant
	v := MakeVariant(struct {
		A int32
		B int16
	}{1, 2})
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"v"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &variant); err != nil {
		t.Fatal(err)
	}
	sl := variant.Value().([]interface{})
	v1, v2 := sl[0].(int32), sl[1].(int16)
	if v1 != int32(1) {
		t.Error("got", v1, "as first int")
	}
	if v2 != int16(2) {
		t.Error("got", v2, "as second int")
	}
}

func TestProtoStructTag(t *testing.T) {
	type Bar struct {
		A int32
		B chan interface{} `dbus:"-"`
		C int32
	}
	var bar1, bar2 Bar
	bar1.A = 234
	bar2.C = 345
	buf := new(bytes.Buffer)
	fds := make([]int, 0)
	enc := newEncoder(buf, binary.LittleEndian, fds)
	if err := enc.Encode(bar1); err != nil {
		t.Fatal(err)
	}
	dec := newDecoder(buf, binary.LittleEndian, enc.fds)
	vs, err := dec.Decode(Signature{"(ii)"})
	if err != nil {
		t.Fatal(err)
	}
	if err = Store(vs, &bar2); err != nil {
		t.Fatal(err)
	}
	if bar1 != bar2 {
		t.Error("struct tag test: got", bar2)
	}
}

func TestProtoStoreStruct(t *testing.T) {
	var foo struct {
		A int32
		B string
		c chan interface{}
		D interface{} `dbus:"-"`
	}
	src := []interface{}{[]interface{}{int32(42), "foo"}}
	err := Store(src, &foo)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProtoStoreNestedStruct(t *testing.T) {
	var foo struct {
		A int32
		B struct {
			C string
			D float64
		}
	}
	src := []interface{}{
		[]interface{}{
			int32(42),
			[]interface{}{
				"foo",
				3.14,
			},
		},
	}
	err := Store(src, &foo)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMessage(t *testing.T) {
	buf := new(bytes.Buffer)
	message := new(Message)
	message.Type = TypeMethodCall
	message.serial = 32
	message.Headers = map[HeaderField]Variant{
		FieldPath:   MakeVariant(ObjectPath("/org/foo/bar")),
		FieldMember: MakeVariant("baz"),
	}
	message.Body = make([]interface{}, 0)
	err := message.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		t.Error(err)
	}
	_, err = DecodeMessage(buf)
	if err != nil {
		t.Error(err)
	}
}

func TestProtoStructInterfaces(t *testing.T) {
	b := []byte{42}
	vs, err := newDecoder(bytes.NewReader(b), binary.LittleEndian, make([]int, 0)).Decode(Signature{"(y)"})
	if err != nil {
		t.Fatal(err)
	}
	if vs[0].([]interface{})[0].(byte) != 42 {
		t.Errorf("wrongs results (got %v)", vs)
	}
}

// ordinary org.freedesktop.DBus.Hello call
var smallMessage = &Message{
	Type:   TypeMethodCall,
	serial: 1,
	Headers: map[HeaderField]Variant{
		FieldDestination: MakeVariant("org.freedesktop.DBus"),
		FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/DBus")),
		FieldInterface:   MakeVariant("org.freedesktop.DBus"),
		FieldMember:      MakeVariant("Hello"),
	},
}

// org.freedesktop.Notifications.Notify
var bigMessage = &Message{
	Type:   TypeMethodCall,
	serial: 2,
	Headers: map[HeaderField]Variant{
		FieldDestination: MakeVariant("org.freedesktop.Notifications"),
		FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/Notifications")),
		FieldInterface:   MakeVariant("org.freedesktop.Notifications"),
		FieldMember:      MakeVariant("Notify"),
		FieldSignature:   MakeVariant(Signature{"susssasa{sv}i"}),
	},
	Body: []interface{}{
		"app_name",
		uint32(0),
		"dialog-information",
		"Notification",
		"This is the body of a notification",
		[]string{"ok", "Ok"},
		map[string]Variant{
			"sound-name": MakeVariant("dialog-information"),
		},
		int32(-1),
	},
}

func BenchmarkDecodeMessageSmall(b *testing.B) {
	var err error
	var rd *bytes.Reader

	b.StopTimer()
	buf := new(bytes.Buffer)
	err = smallMessage.EncodeTo(buf, binary.LittleEndian)
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

func BenchmarkDecodeMessageBig(b *testing.B) {
	var err error
	var rd *bytes.Reader

	b.StopTimer()
	buf := new(bytes.Buffer)
	err = bigMessage.EncodeTo(buf, binary.LittleEndian)
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

func TestEncodeSmallMessage(t *testing.T) {
	buf := new(bytes.Buffer)
	err := smallMessage.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	expected := [][]byte{
		{0x6c, 0x1, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x6e, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x15, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x5, 0x0, 0x0, 0x0, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x0, 0x0, 0x0},
		{0x6c, 0x1, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x6e, 0x0, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x5, 0x0, 0x0, 0x0, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x15, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0},
		{0x6c, 0x1, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x6d, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x15, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x5, 0x0, 0x0, 0x0, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0},
		{0x6c, 0x1, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x6d, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x5, 0x0, 0x0, 0x0, 0x48, 0x65, 0x6c, 0x6c, 0x6f, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x15, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x14, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x44, 0x42, 0x75, 0x73, 0x0, 0x0, 0x0, 0x0},
	}
	found := false
	for _, e := range expected {
		if bytes.Equal(buf.Bytes(), e) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("got %#v, but expected one of %#v", buf.Bytes(), expected)
	}
}

func TestEncodeBigMessage(t *testing.T) {
	buf := new(bytes.Buffer)
	err := bigMessage.EncodeTo(buf, binary.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	expected := [][]byte{
		{0x6c, 0x1, 0x0, 0x1, 0xb0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x9b, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x1e, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x6, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x79, 0x0, 0x0, 0x8, 0x1, 0x67, 0x0, 0xd, 0x73, 0x75, 0x73, 0x73, 0x73, 0x61, 0x73, 0x61, 0x7b, 0x73, 0x76, 0x7d, 0x69, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x8, 0x0, 0x0, 0x0, 0x61, 0x70, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0x0, 0x0, 0x22, 0x0, 0x0, 0x0, 0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x74, 0x68, 0x65, 0x20, 0x62, 0x6f, 0x64, 0x79, 0x20, 0x6f, 0x66, 0x20, 0x61, 0x20, 0x6e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xf, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x6f, 0x6b, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x4f, 0x6b, 0x0, 0x0, 0x2b, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x73, 0x6f, 0x75, 0x6e, 0x64, 0x2d, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x1, 0x73, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff},
		{0x6c, 0x1, 0x0, 0x1, 0xb0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x9f, 0x0, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x6, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x79, 0x0, 0x0, 0x8, 0x1, 0x67, 0x0, 0xd, 0x73, 0x75, 0x73, 0x73, 0x73, 0x61, 0x73, 0x61, 0x7b, 0x73, 0x76, 0x7d, 0x69, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x1e, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x8, 0x0, 0x0, 0x0, 0x61, 0x70, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0x0, 0x0, 0x22, 0x0, 0x0, 0x0, 0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x74, 0x68, 0x65, 0x20, 0x62, 0x6f, 0x64, 0x79, 0x20, 0x6f, 0x66, 0x20, 0x61, 0x20, 0x6e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xf, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x6f, 0x6b, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x4f, 0x6b, 0x0, 0x0, 0x2b, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x73, 0x6f, 0x75, 0x6e, 0x64, 0x2d, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x1, 0x73, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff},
		{0x6c, 0x1, 0x0, 0x1, 0xb0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x9f, 0x0, 0x0, 0x0, 0x8, 0x1, 0x67, 0x0, 0xd, 0x73, 0x75, 0x73, 0x73, 0x73, 0x61, 0x73, 0x61, 0x7b, 0x73, 0x76, 0x7d, 0x69, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x1e, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x6, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x79, 0x0, 0x0, 0x8, 0x0, 0x0, 0x0, 0x61, 0x70, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0x0, 0x0, 0x22, 0x0, 0x0, 0x0, 0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x74, 0x68, 0x65, 0x20, 0x62, 0x6f, 0x64, 0x79, 0x20, 0x6f, 0x66, 0x20, 0x61, 0x20, 0x6e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xf, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x6f, 0x6b, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x4f, 0x6b, 0x0, 0x0, 0x2b, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x73, 0x6f, 0x75, 0x6e, 0x64, 0x2d, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x1, 0x73, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff},
		{0x6c, 0x1, 0x0, 0x1, 0xb0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x9e, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x1e, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x6, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x79, 0x0, 0x0, 0x8, 0x1, 0x67, 0x0, 0xd, 0x73, 0x75, 0x73, 0x73, 0x73, 0x61, 0x73, 0x61, 0x7b, 0x73, 0x76, 0x7d, 0x69, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x8, 0x0, 0x0, 0x0, 0x61, 0x70, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0x0, 0x0, 0x22, 0x0, 0x0, 0x0, 0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x74, 0x68, 0x65, 0x20, 0x62, 0x6f, 0x64, 0x79, 0x20, 0x6f, 0x66, 0x20, 0x61, 0x20, 0x6e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xf, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x6f, 0x6b, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x4f, 0x6b, 0x0, 0x0, 0x2b, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x73, 0x6f, 0x75, 0x6e, 0x64, 0x2d, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x1, 0x73, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff},
		{0x6c, 0x1, 0x0, 0x1, 0xb0, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x9e, 0x0, 0x0, 0x0, 0x3, 0x1, 0x73, 0x0, 0x6, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x79, 0x0, 0x0, 0x8, 0x1, 0x67, 0x0, 0xd, 0x73, 0x75, 0x73, 0x73, 0x73, 0x61, 0x73, 0x61, 0x7b, 0x73, 0x76, 0x7d, 0x69, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x6, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x1, 0x1, 0x6f, 0x0, 0x1e, 0x0, 0x0, 0x0, 0x2f, 0x6f, 0x72, 0x67, 0x2f, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2f, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x2, 0x1, 0x73, 0x0, 0x1d, 0x0, 0x0, 0x0, 0x6f, 0x72, 0x67, 0x2e, 0x66, 0x72, 0x65, 0x65, 0x64, 0x65, 0x73, 0x6b, 0x74, 0x6f, 0x70, 0x2e, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x73, 0x0, 0x0, 0x0, 0x8, 0x0, 0x0, 0x0, 0x61, 0x70, 0x70, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xc, 0x0, 0x0, 0x0, 0x4e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0x0, 0x0, 0x22, 0x0, 0x0, 0x0, 0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, 0x74, 0x68, 0x65, 0x20, 0x62, 0x6f, 0x64, 0x79, 0x20, 0x6f, 0x66, 0x20, 0x61, 0x20, 0x6e, 0x6f, 0x74, 0x69, 0x66, 0x69, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xf, 0x0, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x6f, 0x6b, 0x0, 0x0, 0x2, 0x0, 0x0, 0x0, 0x4f, 0x6b, 0x0, 0x0, 0x2b, 0x0, 0x0, 0x0, 0xa, 0x0, 0x0, 0x0, 0x73, 0x6f, 0x75, 0x6e, 0x64, 0x2d, 0x6e, 0x61, 0x6d, 0x65, 0x0, 0x1, 0x73, 0x0, 0x0, 0x0, 0x12, 0x0, 0x0, 0x0, 0x64, 0x69, 0x61, 0x6c, 0x6f, 0x67, 0x2d, 0x69, 0x6e, 0x66, 0x6f, 0x72, 0x6d, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff},
	}
	found := false
	for _, e := range expected {
		if bytes.Equal(buf.Bytes(), e) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("got %#v, but expected one of %#v", buf.Bytes(), expected)
	}
}

func BenchmarkEncodeMessageSmall(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		err = smallMessage.EncodeTo(io.Discard, binary.LittleEndian)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeMessageBig(b *testing.B) {
	var err error
	for i := 0; i < b.N; i++ {
		err = bigMessage.EncodeTo(io.Discard, binary.LittleEndian)
		if err != nil {
			b.Fatal(err)
		}
	}
}
