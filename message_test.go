package dbus

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestMessage(t *testing.T) {
	buf := new(bytes.Buffer)
	message := new(Message)
	message.Order = binary.LittleEndian
	message.Type = TypeMethodCall
	message.Serial = 32
	message.Headers = map[HeaderField]Variant{
		FieldPath:   MakeVariant(ObjectPath("/org/foo/bar")),
		FieldMember: MakeVariant("baz"),
	}
	message.Body = make([]interface{}, 0)
	err := message.EncodeTo(buf)
	if err != nil {
		t.Error(err)
	}
	_, err = DecodeMessage(buf)
	if err != nil {
		t.Error(err)
	}
}
