//go:build !windows && !solaris
// +build !windows,!solaris

package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"syscall"
)

type oobReader struct {
	conn *net.UnixConn
	oob  []byte
	buf  [4096]byte

	// The following fields are used to reduce memory allocs.
	headers []header
	b       []byte
	r       *bytes.Reader
	blength uint32
	hlength uint32
	proto   byte
}

func (o *oobReader) Read(b []byte) (n int, err error) {
	n, oobn, flags, _, err := o.conn.ReadMsgUnix(b, o.buf[:])
	if err != nil {
		return n, err
	}
	if flags&syscall.MSG_CTRUNC != 0 {
		return n, errors.New("dbus: control data truncated (too many fds received)")
	}
	o.oob = append(o.oob, o.buf[:oobn]...)
	return n, nil
}

type unixTransport struct {
	*net.UnixConn
	rdr        *oobReader
	hasUnixFDs bool
}

func newUnixTransport(keys string) (transport, error) {
	var err error

	t := new(unixTransport)
	abstract := getKey(keys, "abstract")
	path := getKey(keys, "path")
	switch {
	case abstract == "" && path == "":
		return nil, errors.New("dbus: invalid address (neither path nor abstract set)")
	case abstract != "" && path == "":
		t.UnixConn, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: "@" + abstract, Net: "unix"})
		if err != nil {
			return nil, err
		}
		return t, nil
	case abstract == "" && path != "":
		t.UnixConn, err = net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			return nil, err
		}
		return t, nil
	default:
		return nil, errors.New("dbus: invalid address (both path and abstract set)")
	}
}

func init() {
	transports["unix"] = newUnixTransport
}

func (t *unixTransport) EnableUnixFDs() {
	t.hasUnixFDs = true
}

func (t *unixTransport) ReadMessage() (*Message, error) {
	// To be sure that all bytes of out-of-band data are read, we use a special
	// reader that uses ReadUnix on the underlying connection instead of Read
	// and gathers the out-of-band data in a buffer.
	if t.rdr == nil {
		t.rdr = &oobReader{
			conn: t.UnixConn,
			// This buffer is used to decode headers and the body.
			// 16 bytes is enough to read the part of the header that has a constant size.
			b: make([]byte, 16),
			// The reader helps to read from the buffer several times.
			r: &bytes.Reader{},
		}
	} else {
		t.rdr.oob = t.rdr.oob[:0]
		t.rdr.headers = t.rdr.headers[:0]
	}

	var (
		b = t.rdr.b[:1]
		r = t.rdr.r
	)
	if _, err := t.rdr.Read(b); err != nil {
		return nil, err
	}
	var order binary.ByteOrder
	switch b[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, InvalidMessageError("invalid byte order")
	}

	// [4:8] is a length of message body,
	// [12:16] is a length of header fields (without alignment)
	dec := newDecoder(t.rdr, order, nil)
	dec.pos = 1
	vs, err := dec.Decode(Signature{"yyyuu"})
	if err != nil {
		return nil, err
	}
	msg := &Message{}
	if err = Store(vs, &msg.Type, &msg.Flags, &t.rdr.proto, &t.rdr.blength, &msg.serial); err != nil {
		return nil, err
	}

	// Get the header length.
	b = t.rdr.b[:4]
	if _, err = io.ReadFull(t.rdr, b); err != nil {
		return nil, err
	}
	r.Reset(b)
	if err = binary.Read(r, order, &t.rdr.hlength); err != nil {
		return nil, err
	}
	if t.rdr.hlength+t.rdr.blength+16 > 1<<27 {
		return nil, InvalidMessageError("message is too long")
	}

	// Decode headers and look for unix fds.
	if _, err = r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	dec = newDecoder(io.MultiReader(r, t.rdr), order, nil)
	dec.pos = 12
	vs, err = dec.Decode(Signature{"a(yv)"})
	if err != nil {
		return nil, err
	}
	if err = Store(vs, &t.rdr.headers); err != nil {
		return nil, err
	}
	var unixfds uint32
	for _, v := range t.rdr.headers {
		if v.Field == byte(FieldUnixFDs) {
			unixfds, _ = v.Variant.value.(uint32)
		}
	}

	msg.Headers = make(map[HeaderField]Variant)
	for _, v := range t.rdr.headers {
		msg.Headers[HeaderField(v.Field)] = v.Variant
	}

	dec.align(8)
	// Grow the buffer to accomodate for message body.
	if int(t.rdr.blength) > cap(t.rdr.b) {
		t.rdr.b = make([]byte, t.rdr.blength)
	}
	b = t.rdr.b[:t.rdr.blength]
	if _, err = io.ReadFull(t.rdr, b); err != nil {
		return nil, err
	}
	r.Reset(b)

	if unixfds != 0 {
		if !t.hasUnixFDs {
			return nil, errors.New("dbus: got unix fds on unsupported transport")
		}
		// read the fds from the OOB data
		scms, err := syscall.ParseSocketControlMessage(t.rdr.oob)
		if err != nil {
			return nil, err
		}
		if len(scms) != 1 {
			return nil, errors.New("dbus: received more than one socket control message")
		}
		fds, err := syscall.ParseUnixRights(&scms[0])
		if err != nil {
			return nil, err
		}
		if err = DecodeMessageBody(msg, r, order, fds); err != nil {
			return nil, err
		}
		// substitute the values in the message body (which are indices for the
		// array receiver via OOB) with the actual values
		for i, v := range msg.Body {
			switch index := v.(type) {
			case UnixFDIndex:
				if uint32(index) >= unixfds {
					return nil, InvalidMessageError("invalid index for unix fd")
				}
				msg.Body[i] = UnixFD(fds[index])
			case []UnixFDIndex:
				fdArray := make([]UnixFD, len(index))
				for k, j := range index {
					if uint32(j) >= unixfds {
						return nil, InvalidMessageError("invalid index for unix fd")
					}
					fdArray[k] = UnixFD(fds[j])
				}
				msg.Body[i] = fdArray
			}
		}
		return msg, nil
	}

	if err = DecodeMessageBody(msg, r, order, nil); err != nil {
		return nil, err
	}
	return msg, nil
}

func (t *unixTransport) SendMessage(msg *Message) error {
	fdcnt, err := msg.CountFds()
	if err != nil {
		return err
	}
	if fdcnt != 0 {
		if !t.hasUnixFDs {
			return errors.New("dbus: unix fd passing not enabled")
		}
		msg.Headers[FieldUnixFDs] = MakeVariant(uint32(fdcnt))
		buf := new(bytes.Buffer)
		fds, err := msg.EncodeToWithFDs(buf, nativeEndian)
		if err != nil {
			return err
		}
		oob := syscall.UnixRights(fds...)
		n, oobn, err := t.UnixConn.WriteMsgUnix(buf.Bytes(), oob, nil)
		if err != nil {
			return err
		}
		if n != buf.Len() || oobn != len(oob) {
			return io.ErrShortWrite
		}
	} else {
		if err := msg.EncodeTo(t, nativeEndian); err != nil {
			return err
		}
	}
	return nil
}

func (t *unixTransport) SupportsUnixFDs() bool {
	return true
}
