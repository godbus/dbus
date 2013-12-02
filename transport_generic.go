package dbus

import (
	"encoding/binary"
	"errors"
	"io"
)

type genericTransport struct {
	io.ReadWriteCloser
}

func (t genericTransport) SendNullByte() error {
	_, err := t.Write([]byte{0})
	return err
}

func (t genericTransport) ReadNullByte() error {
	res := []byte{0}
	n, err := t.Read(res)
	if err != nil {
		return err
	}
	if n == 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (t genericTransport) SupportsUnixFDs() bool {
	return false
}

func (t genericTransport) EnableUnixFDs() {}

func (t genericTransport) ReadMessage() (*Message, error) {
	return DecodeMessage(t)
}

func (t genericTransport) SendMessage(msg *Message) error {
	for _, v := range msg.Body {
		if _, ok := v.(UnixFD); ok {
			return errors.New("dbus: unix fd passing not enabled")
		}
	}
	return msg.EncodeTo(t, binary.LittleEndian)
}
