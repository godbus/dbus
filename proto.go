package dbus

import (
	"reflect"
	"strings"
)

var (
	byteType       = reflect.TypeOf(byte(0))
	boolType       = reflect.TypeOf(false)
	uint8Type      = reflect.TypeOf(uint8(0))
	int16Type      = reflect.TypeOf(int16(0))
	uint16Type     = reflect.TypeOf(uint16(0))
	int32Type      = reflect.TypeOf(int32(0))
	uint32Type     = reflect.TypeOf(uint32(0))
	int64Type      = reflect.TypeOf(int64(0))
	uint64Type     = reflect.TypeOf(uint64(0))
	float64Type    = reflect.TypeOf(float64(0))
	stringType     = reflect.TypeOf("")
	signatureType  = reflect.TypeOf(Signature{""})
	objectPathType = reflect.TypeOf(ObjectPath(""))
	variantType    = reflect.TypeOf(Variant{Signature{""}, nil})
	interfacesType = reflect.TypeOf([]interface{}{})
)

type invalidTypeError struct {
	reflect.Type
}

func (err invalidTypeError) Error() string {
	return "dbus: invalid type " + err.Type.String()
}

var sigToType = map[byte]reflect.Type{
	'y': byteType,
	'b': boolType,
	'n': int16Type,
	'q': uint16Type,
	'i': int32Type,
	'u': uint32Type,
	'x': int64Type,
	't': uint64Type,
	'd': float64Type,
	's': stringType,
	'g': signatureType,
	'o': objectPathType,
	'v': variantType,
}

// Signature represents a correct type signature as specified
// by the DBus specification.
type Signature struct {
	str string
}

// GetSignature returns the concatenation of all the signatures
// of the given values. It panics if one of them is not representable
// in DBus.
func GetSignature(vs ...interface{}) Signature {
	var s string
	for _, v := range vs {
		s += getSignature(reflect.TypeOf(v))
	}
	return Signature{s}
}

// GetSignatureType returns the signature of the given type. It panics if the
// type is not representable in DBus.
func GetSignatureType(t reflect.Type) Signature {
	return Signature{getSignature(t)}
}

// getSignature returns the signature of the given type and panics on unknown types.
func getSignature(v reflect.Type) string {
	// handle simple types first
	switch v.Kind() {
	case reflect.Uint8:
		return "y"
	case reflect.Bool:
		return "b"
	case reflect.Int16:
		return "n"
	case reflect.Uint16:
		return "q"
	case reflect.Int32, reflect.Int:
		return "i"
	case reflect.Uint32, reflect.Uint:
		return "u"
	case reflect.Int64:
		return "x"
	case reflect.Uint64:
		return "t"
	case reflect.Float64:
		return "d"
	case reflect.Ptr:
		return getSignature(v.Elem())
	case reflect.String:
		if v == objectPathType {
			return "o"
		}
		return "s"
	case reflect.Struct:
		if v == variantType {
			return "v"
		} else if v == signatureType {
			return "g"
		}
		var s string
		for i := 0; i < v.NumField(); i++ {
			s += getSignature(v.Field(i).Type)
		}
		return "(" + s + ")"
	case reflect.Array, reflect.Slice:
		return "a" + getSignature(v.Elem())
	case reflect.Map:
		return "a{" + getSignature(v.Key()) + getSignature(v.Elem()) + "}"
	}
	panic("unknown type " + v.String())
}

// StringToSig returns the signature represented by this string, or a
// SignatureError if the string is not a valid signature.
func StringToSig(s string) (sig Signature, err error) {
	if len(s) == 0 {
		return
	}
	if len(s) > 255 {
		return Signature{""}, SignatureError{s, "too long"}
	}
	sig.str = s
	for err == nil && len(s) != 0 {
		err, s = validSingle(s, 0)
	}
	if err != nil {
		sig = Signature{""}
	}

	return
}

// SrintToSigMust behaves like StringToSig, except that it panics if s is not
// valid.
func StringToSigMust(s string) Signature {
	sig, err := StringToSig(s)
	if err != nil {
		panic(err)
	}
	return sig
}

// Empty retruns whether the signature is the empty signature.
func (s Signature) Empty() bool {
	return s.str == ""
}

// Single returns whether the signature represents a single, complete type.
func (s Signature) Single() bool {
	_, r := validSingle(s.str, 0)
	return r == ""
}

// String returns the signature's string representation.
func (s Signature) String() string {
	return s.str
}

// Values returns a slice of pointers to values that match the given signature.
func (s Signature) Values() []interface{} {
	slice := make([]interface{}, 0)
	str := s.str
	for str != "" {
		slice = append(slice, reflect.New(value(str)).Interface())
		_, str = validSingle(str, 0)
	}
	return slice
}

// A SignatureError indicates that a signature passed to a function or received
// on a connection is not a valid signature.
type SignatureError struct {
	Sig    string
	Reason string
}

func (err SignatureError) Error() string {
	return "invalid signature: '" + err.Sig + "' (" + err.Reason + ")"
}

// An ObjectPath is an object path as defined by the DBus spec.
type ObjectPath string

// IsValid returns whether the path is valid.
func (o ObjectPath) IsValid() bool {
	s := string(o)
	if len(s) == 0 {
		return false
	}
	if s[0] != '/' {
		return false
	}
	if s[len(s)-1] == '/' && len(s) != 1 {
		return false
	}
	// probably not used, but technically possible
	if s == "/" {
		return true
	}
	split := strings.Split(s[1:], "/")
	for _, v := range split {
		if len(v) == 0 {
			return false
		}
		for _, c := range v {
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') ||
				(c >= 'a' && c <= 'z') || c == '_') {

				return false
			}
		}
	}
	return true
}

// Variant represents a DBus variant type.
type Variant struct {
	sig   Signature
	value interface{}
}

// MakeVariant converts the given value to a Variant.
func MakeVariant(v interface{}) Variant {
	return Variant{GetSignature(v), v}
}

// Signature returns the DBus signature of the underlying value of v.
func (v Variant) Signature() Signature {
	return v.sig
}

// Value returns the underlying value of v.
func (v Variant) Value() interface{} {
	return v.value
}

// alignment returns the alignment of values of type t.
func alignment(t reflect.Type) int {
	n, ok := map[reflect.Type]int{
		variantType:    1,
		objectPathType: 4,
		signatureType:  1,
	}[t]
	if ok {
		return n
	}
	switch t.Kind() {
	case reflect.Uint8:
		return 1
	case reflect.Uint16, reflect.Int16:
		return 2
	case reflect.Uint32, reflect.Int32, reflect.String, reflect.Array, reflect.Slice:
		return 4
	case reflect.Uint64, reflect.Int64, reflect.Float64, reflect.Struct:
		return 8
	case reflect.Ptr:
		return alignment(t.Elem())
	}
	return 1
}

// isKeyType returns whether t is a valid type for a DBus dict.
func isKeyType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float64,
		reflect.String:

		return true
	}
	return false
}

// Try to read a single type from this string. If it was successfull, err is nil
// and rem is the remaining unparsed part. Otherwise, err is a non-nil
// SignatureError and rem is "". depth is the current recursion depth which may
// not be greater than 64 and should be given as 0 on the first call.
func validSingle(s string, depth int) (err error, rem string) {
	if s == "" {
		return SignatureError{Sig: s, Reason: "empty signature"}, ""
	}
	if depth > 64 {
		return SignatureError{Sig: s, Reason: "container nesting too deep"}, ""
	}
	switch s[0] {
	case 'y', 'b', 'n', 'q', 'i', 'u', 'x', 't', 'd', 's', 'g', 'o', 'v':
		return nil, s[1:]
	case 'a':
		if len(s) > 1 && s[1] == '{' {
			i := strings.LastIndex(s, "}")
			if i == -1 {
				return SignatureError{Sig: s, Reason: "unmatched '{'"}, ""
			}
			rem = s[i+1:]
			s = s[2:i]
			if err, _ = validSingle(s[:1], depth+1); err != nil {
				return err, ""
			}
			err, nr := validSingle(s[1:], depth+1)
			if err != nil {
				return err, ""
			}
			if nr != "" {
				return SignatureError{Sig: s, Reason: "too many types in dict"}, ""
			}
			return nil, rem
		}
		return validSingle(s[1:], depth+1)
	case '(':
		i := strings.LastIndex(s, ")")
		if i == -1 {
			return SignatureError{Sig: s, Reason: "unmatched ')'"}, ""
		}
		rem = s[i+1:]
		s = s[1:i]
		for err == nil && s != "" {
			err, s = validSingle(s, depth+1)
		}
		if err != nil {
			rem = ""
		}
		return
	}
	return SignatureError{Sig: s, Reason: "invalid type character"}, ""
}

// value returns the type of the given signature. It ignores any left over
// characters and panics if s doesn't start with a valid type signature.
func value(s string) (t reflect.Type) {
	err, _ := validSingle(s, 0)
	if err != nil {
		panic(err)
	}

	if t, ok := sigToType[s[0]]; ok {
		return t
	}
	switch s[0] {
	case 'a':
		if s[1] == '{' {
			i := strings.LastIndex(s, "}")
			t = reflect.MapOf(sigToType[s[2]], value(s[3:i]))
		} else if s[1] == '(' {
			t = interfacesType
		} else {
			t = reflect.SliceOf(sigToType[s[1]])
		}
	case '(':
		t = interfacesType
	}
	return
}
