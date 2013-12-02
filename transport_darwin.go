package dbus

func (t *unixTransport) SendNullByte() error {
	_, err := t.Write([]byte{0})
	return err
}

func (t *unixTransport) ReadNullByte() error {
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
