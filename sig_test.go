package dbus

import (
	"reflect"
	"testing"
)

var sigTests = []struct {
	vs  []interface{}
	sig Signature
}{
	{
		[]interface{}{new(int32)},
		Signature{"i"},
	},
	{
		[]interface{}{new(string)},
		Signature{"s"},
	},
	{
		[]interface{}{new(Signature)},
		Signature{"g"},
	},
	{
		[]interface{}{new([]int16)},
		Signature{"an"},
	},
	{
		[]interface{}{new(int16), new(uint32)},
		Signature{"nu"},
	},
	{
		[]interface{}{new(map[byte]Variant)},
		Signature{"a{yv}"},
	},
	{
		[]interface{}{new(Variant), new([]map[int32]string)},
		Signature{"vaa{is}"},
	},
}

func TestSig(t *testing.T) {
	for i, v := range sigTests {
		sig := GetSignature(v.vs...)
		if sig != v.sig {
			t.Errorf("test %d: got %q, expected %q", i+1, sig.str, v.sig.str)
		}
		svs := v.sig.Values()
		if len(svs) != len(v.vs) {
			t.Errorf("test %d: got %d values, expected %d", i+1, len(svs), len(v.vs))
			continue
		}
		for j := range svs {
			if t1, t2 := reflect.TypeOf(svs[j]), reflect.TypeOf(v.vs[j]); t1 != t2 {
				t.Errorf("test %d: got %s, expected %s", i+1, t1, t2)
			}
		}
	}
}

func TestSigStructSlice(t *testing.T) {
	sig := Signature{"a(i)"}
	if reflect.TypeOf(sig.Values()[0]) != reflect.TypeOf(new([][]interface{})) {
		t.Errorf("got type: %s", reflect.TypeOf(sig.Values()[0]))
	}
}

var getSigTest = []interface{}{
	[]struct {
		b byte
		i int32
		t uint64
		s string
	}{},
	map[string]Variant{},
}

func BenchmarkGetSignatureSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetSignature("", int32(0))
	}
}

func BenchmarkGetSignatureLong(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetSignature(getSigTest...)
	}
}

func BenchmarkSignatureValuesSimple(b *testing.B) {
	s := Signature{"si"}
	for i := 0; i < b.N; i++ {
		s.Values()
	}
}

func BenchmarkSignatureValuesLong(b *testing.B) {
	s := Signature{"a(bits)a{sv}"}
	for i := 0; i < b.N; i++ {
		s.Values()
	}
}
