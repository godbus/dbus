package dbus

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

var variantFormatTests = []struct {
	v interface{}
	s string
}{
	{int32(1), `1`},
	{"foo", `"foo"`},
	{ObjectPath("/org/foo"), `@o "/org/foo"`},
	{Signature{"i"}, `@g "i"`},
	{[]byte{}, `@ay []`},
	{[]int32{1, 2}, `[1, 2]`},
	{[]int64{1, 2}, `@ax [1, 2]`},
	{[][]int32{{3, 4}, {5, 6}}, `[[3, 4], [5, 6]]`},
	{[]Variant{MakeVariant(int32(1)), MakeVariant(1.0)}, `[<1>, <@d 1>]`},
	{map[string]int32{"one": 1, "two": 2}, `{"one": 1, "two": 2}`},
	{map[int32]ObjectPath{1: "/org/foo"}, `@a{io} {1: "/org/foo"}`},
	{map[string]Variant{}, `@a{sv} {}`},
}

func TestFormatVariant(t *testing.T) {
	for i, v := range variantFormatTests {
		if s := MakeVariant(v.v).String(); s != v.s {
			t.Errorf("test %d: got %q, wanted %q", i+1, s, v.s)
		}
	}
}

var variantParseTests = []struct {
	s string
	v interface{}
}{
	{"1", int32(1)},
	{"true", true},
	{"false", false},
	{"1.0", float64(1.0)},
	{"0x10", int32(16)},
	{"1e1", float64(10)},
	{`"foo"`, "foo"},
	{`"\a\b\f\n\r\t"`, "\x07\x08\x0c\n\r\t"},
	{`"\u00e4\U0001f603"`, "\u00e4\U0001f603"},
	{"[1]", []int32{1}},
	{"[1, 2, 3]", []int32{1, 2, 3}},
	{"@ai []", []int32{}},
	{"[1, 5.0]", []float64{1, 5.0}},
	{"[[1, 2], [3, 4.0]]", [][]float64{{1, 2}, {3, 4}}},
	{`[@o "/org/foo", "/org/bar"]`, []ObjectPath{"/org/foo", "/org/bar"}},
	{"<1>", MakeVariant(int32(1))},
	{"[<1>, <2.0>]", []Variant{MakeVariant(int32(1)), MakeVariant(2.0)}},
	{`[[], [""]]`, [][]string{{}, {""}}},
	{`@a{ss} {}`, map[string]string{}},
	{`{"foo": 1}`, map[string]int32{"foo": 1}},
	{`[{}, {"foo": "bar"}]`, []map[string]string{{}, {"foo": "bar"}}},
	{`{"a": <1>, "b": <"foo">}`,
		map[string]Variant{"a": MakeVariant(int32(1)), "b": MakeVariant("foo")}},
	{`b''`, []byte{0}},
	{`b"abc"`, []byte{'a', 'b', 'c', 0}},
	{`b"\x01\0002\a\b\f\n\r\t"`, []byte{1, 2, 0x7, 0x8, 0xc, '\n', '\r', '\t', 0}},
	{`[[0], b""]`, [][]byte{{0}, {0}}},
	{"int16 0", int16(0)},
	{"byte 0", byte(0)},
}

func TestParseVariant(t *testing.T) {
	for i, v := range variantParseTests {
		nv, err := ParseVariant(v.s, Signature{})
		if err != nil {
			t.Errorf("test %d: parsing failed: %s", i+1, err)
			continue
		}
		if !reflect.DeepEqual(nv.value, v.v) {
			t.Errorf("test %d: got %q, wanted %q", i+1, nv, v.v)
		}
	}
}

func TestVariantStore(t *testing.T) {
	str := "foo bar"
	v := MakeVariant(str)
	var result string
	err := v.Store(&result)
	if err != nil {
		t.Fatal(err)
	}
	if result != str {
		t.Fatalf("expected %s, got %s\n", str, result)
	}

}

func TestJson(t *testing.T) {
	str := "uint64 123456789"
	v, err := ParseVariant(str, Signature{})
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) == "{}" {
		t.Fatal("Can't marshal the variant value!")
	}

	var v1 Variant
	err = json.Unmarshal(bytes, &v1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.value == nil {
		t.Fatal("Can't unmarshal the variant value!")
	}
	if v1.value != v.value {
		t.Fatalf("expected %s, got %s\n", v.value, v1.value)
	}
}

func TestJsonSimple(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
	}{
		{
			name: "int",
			val:  100,
		},
		{
			name: "float",
			val:  100.3,
		},
		{
			name: "string",
			val:  "lfbzhm",
		},
		{
			name: "byte",
			val:  'l',
		},
	}
	for _, tc := range cases {
		val := tc.val
		v := MakeVariant(val)
		bytes, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if string(bytes) == "{}" {
			t.Fatal("Can't marshal the variant value!")
		}

		var v1 Variant
		err = json.Unmarshal(bytes, &v1)
		if err != nil {
			t.Fatal(err)
		}
		if v1.value == nil {
			t.Fatal("Can't unmarshal the variant value!")
		}

		str, _ := v1.format()
		valstr := fmt.Sprintf("%v", val)
		if !strings.Contains(str, valstr) {
			t.Fatalf("expected %v, got %v\n", valstr, str)
		}
	}
}

func TestJsonArray(t *testing.T) {
	arr := []string{"lfb", "zhm", "nn", "tt", "hello"}
	v := MakeVariant(arr)
	bytes, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) == "{}" {
		t.Fatal("Can't marshal the variant value!")
	}

	var v1 Variant
	err = json.Unmarshal(bytes, &v1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.value == nil {
		t.Fatal("Can't unmarshal the variant value!")
	}

	var arr1 []string
	err = v1.Store(&arr1)
	if err != nil {
		t.Fatal(err)
	}
	if len(arr1) != len(arr) {
		t.Fatalf("expected %v, got %v\n", v.value, v1.value)
	}
}

func TestJsonMap(t *testing.T) {
	mapstr := map[string]string{
		"lfb": "zhm",
		"nn":  "tt",
	}
	v := MakeVariant(mapstr)
	bytes, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(bytes) == "{}" {
		t.Fatal("Can't marshal the variant value!")
	}

	var v1 Variant
	err = json.Unmarshal(bytes, &v1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.value == nil {
		t.Fatal("Can't unmarshal the variant value!")
	}

	var map1 map[string]string
	err = v1.Store(&map1)
	if err != nil {
		t.Fatal(err)
	}
	if len(map1) != len(mapstr) {
		t.Fatalf("expected %v, got %v\n", v.value, v1.value)
	}
}

func TestGob(t *testing.T) {
	str := "uint64 123456789"
	v, err := ParseVariant(str, Signature{})
	if err != nil {
		t.Fatal(err)
	}

	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	err = encoder.Encode(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffer.Bytes()) == "{}" {
		t.Fatal("Can't marshal the variant value!")
	}

	decoder := gob.NewDecoder(&buffer)
	var v1 Variant
	err = decoder.Decode(&v1)
	if err != nil {
		t.Fatal(err)
	}
	if v1.value == nil {
		t.Fatal("Can't unmarshal the variant value!")
	}
	if v1.value != v.value {
		t.Fatalf("expected %s, got %s\n", v.value, v1.value)
	}
}
