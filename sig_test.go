package dbus

import (
	"testing"
)

var sigTests = []struct {
	vs  []any
	sig Signature
}{
	{
		[]any{new(int32)},
		Signature{"i"},
	},
	{
		[]any{new(string)},
		Signature{"s"},
	},
	{
		[]any{new(Signature)},
		Signature{"g"},
	},
	{
		[]any{new([]int16)},
		Signature{"an"},
	},
	{
		[]any{new(int16), new(uint32)},
		Signature{"nu"},
	},
	{
		[]any{new(map[byte]Variant)},
		Signature{"a{yv}"},
	},
	{
		[]any{new(Variant), new([]map[int32]string)},
		Signature{"vaa{is}"},
	},
}

func TestSig(t *testing.T) {
	for i, v := range sigTests {
		sig := SignatureOf(v.vs...)
		if sig != v.sig {
			t.Errorf("test %d: got %q, expected %q", i+1, sig.str, v.sig.str)
		}
	}
}

var getSigTest = []any{
	[]struct {
		B byte
		I int32
		T uint64
		S string
	}{},
	map[string]Variant{},
}

func BenchmarkGetSignatureSimple(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SignatureOf("", int32(0))
	}
}

func BenchmarkGetSignatureLong(b *testing.B) {
	for i := 0; i < b.N; i++ {
		SignatureOf(getSigTest...)
	}
}
