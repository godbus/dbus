package dbus

import "testing"

var variantTests = []struct {
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

func TestVariant(t *testing.T) {
	for i, v := range variantTests {
		if s := MakeVariant(v.v).String(); s != v.s {
			t.Errorf("test %d: got %q, wanted %q", i+1, s, v.s)
		}
	}
}
