//go:build !windows && !solaris && !js

package dbus

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"syscall"
)

// msghead represents the part of the message header
// that has a constant size (byte order + 15 bytes).
type msghead struct {
	Type      Type
	Flags     Flags
	Proto     byte
	BodyLen   uint32
	Serial    uint32
	HeaderLen uint32
}

type oobReader struct {
	conn       *net.UnixConn
	fds        []int
	buf        [4096]byte
	controlErr error

	// The following fields are used to reduce memory allocs.
	headers  []header
	csheader []byte
	b        *bytes.Buffer
	r        *bytes.Reader
	dec      *decoder
	msghead
}

func (o *oobReader) Read(b []byte) (n int, err error) {
	if o.controlErr != nil {
		return 0, o.controlErr
	}
	n, oobn, flags, _, err := o.conn.ReadMsgUnix(b, o.buf[:])
	// Collect rights before checking errors: recvmsg has already installed
	// these descriptors, including any complete rights in truncated data.
	o.controlErr = o.readFDs(o.buf[:oobn])
	if flags&syscall.MSG_CTRUNC != 0 {
		o.controlErr = errors.New("dbus: control data truncated (too many fds received)")
	}
	if err != nil {
		return n, err
	}
	return n, o.controlErr
}

type unixTransport struct {
	*net.UnixConn
	rdr        *oobReader
	hasUnixFDs bool
}

func newUnixTransportFromConn(conn *net.UnixConn) transport {
	t := new(unixTransport)
	t.UnixConn = conn
	t.hasUnixFDs = true

	return t
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
	// and collects the file descriptors from each read.
	if t.rdr == nil {
		t.rdr = &oobReader{
			conn: t.UnixConn,
			// This buffer is used to decode the part of the header that has a constant size.
			csheader: make([]byte, 16),
			b:        &bytes.Buffer{},
			// The reader helps to read from the buffer several times.
			r:   &bytes.Reader{},
			dec: &decoder{},
		}
	} else {
		t.rdr.headers = t.rdr.headers[:0]
	}
	var delivered map[int]struct{}
	defer func() {
		// Ownership transfers only after the entire message succeeds. Until
		// then, every received descriptor belongs to this read.
		for _, fd := range t.rdr.fds {
			if _, ok := delivered[fd]; !ok {
				_ = syscall.Close(fd)
			}
		}
		t.rdr.fds = t.rdr.fds[:0]
		t.rdr.controlErr = nil
	}()
	var (
		r   = t.rdr.r
		b   = t.rdr.b
		dec = t.rdr.dec
	)

	_, err := io.ReadFull(t.rdr, t.rdr.csheader)
	if err != nil {
		return nil, err
	}

	var order binary.ByteOrder
	switch t.rdr.csheader[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, InvalidMessageError("invalid byte order")
	}

	r.Reset(t.rdr.csheader[1:])
	if err := binary.Read(r, order, &t.rdr.msghead); err != nil {
		return nil, err
	}

	msg := &Message{
		Type:   t.rdr.Type,
		Flags:  t.rdr.Flags,
		serial: t.rdr.Serial,
	}
	// Length of header fields (without alignment).
	hlen := t.rdr.HeaderLen
	if hlen%8 != 0 {
		hlen += 8 - (hlen % 8)
	}
	if hlen+t.rdr.BodyLen+16 > 1<<27 {
		return nil, InvalidMessageError("message is too long")
	}

	// Decode headers and look for unix fds.
	b.Reset()
	if _, err = b.Write(t.rdr.csheader[12:]); err != nil {
		return nil, err
	}
	if _, err = io.CopyN(b, t.rdr, int64(hlen)); err != nil {
		return nil, err
	}
	dec.Reset(b, order, nil)
	dec.pos = 12
	vs, err := dec.Decode(Signature{"a(yv)"})
	if err != nil {
		return nil, err
	}
	if err = Store(vs, &t.rdr.headers); err != nil {
		return nil, err
	}
	var unixfds uint32
	for _, v := range t.rdr.headers {
		if v.Field == byte(FieldUnixFDs) {
			unixfds, _ = v.value.(uint32)
		}
	}

	msg.Headers = make(map[HeaderField]Variant)
	for _, v := range t.rdr.headers {
		msg.Headers[HeaderField(v.Field)] = v.Variant
	}

	dec.align(8)
	body, err := readFull(t.rdr, nil, int(t.rdr.BodyLen))
	if err != nil {
		return nil, err
	}
	r.Reset(body)
	// io.ReadFull can discard an error when the read also fills its buffer.
	// Ancillary-data errors must still prevent successful FD delivery.
	if t.rdr.controlErr != nil {
		return nil, t.rdr.controlErr
	}

	var fds []int
	if unixfds != 0 {
		if !t.hasUnixFDs {
			return nil, errors.New("dbus: got unix fds on unsupported transport")
		}
		if uint64(unixfds) > uint64(len(t.rdr.fds)) {
			return nil, InvalidMessageError("not enough unix file descriptors")
		}
		fds = t.rdr.fds[:unixfds]
	}

	dec.Reset(r, order, fds)
	if err = decodeMessageBody(msg, dec); err != nil {
		return nil, err
	}
	used := make(map[int]struct{})
	for _, value := range msg.Body {
		if err = collectUnixFDs(value, used); err != nil {
			return nil, err
		}
	}
	delivered = used
	return msg, nil
}

func decodeMessageBody(msg *Message, dec *decoder) error {
	if err := msg.validateHeader(); err != nil {
		return err
	}

	sig, _ := msg.Headers[FieldSignature].value.(Signature)
	if sig.str == "" {
		return nil
	}

	var err error
	msg.Body, err = dec.Decode(sig)
	return err
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
		n, oobn, err := t.WriteMsgUnix(buf.Bytes(), oob, nil)
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
