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
