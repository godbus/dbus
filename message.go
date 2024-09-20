package dbus

import (
	"bytes"
	"encoding/binary"
	"io"
	"reflect"
	"strconv"
)

const protoVersion byte = 1

// Flags represents the possible flags of a D-Bus message.
type Flags byte

const (
	// FlagNoReplyExpected signals that the message is not expected to generate
	// a reply. If this flag is set on outgoing messages, any possible reply
	// will be discarded.
	FlagNoReplyExpected Flags = 1 << iota
	// FlagNoAutoStart signals that the message bus should not automatically
	// start an application when handling this message.
	FlagNoAutoStart
	// FlagAllowInteractiveAuthorization may be set on a method call
	// message to inform the receiving side that the caller is prepared
	// to wait for interactive authorization, which might take a
	// considerable time to complete. For instance, if this flag is set,
	// it would be appropriate to query the user for passwords or
	// confirmation via Polkit or a similar framework.
	FlagAllowInteractiveAuthorization
)

// Type represents the possible types of a D-Bus message.
type Type byte

const (
	TypeMethodCall Type = 1 + iota
	TypeMethodReply
	TypeError
	TypeSignal
	typeMax
)

func (t Type) String() string {
	switch t {
	case TypeMethodCall:
		return "method call"
	case TypeMethodReply:
		return "reply"
	case TypeError:
		return "error"
	case TypeSignal:
		return "signal"
	}
	return "invalid"
}

// HeaderField represents the possible byte codes for the headers
// of a D-Bus message.
type HeaderField byte

const (
	FieldPath HeaderField = 1 + iota
	FieldInterface
	FieldMember
	FieldErrorName
	FieldReplySerial
	FieldDestination
	FieldSender
	FieldSignature
	FieldUnixFDs
	fieldMax
)

// An InvalidMessageError describes the reason why a D-Bus message is regarded as
// invalid.
type InvalidMessageError string

func (e InvalidMessageError) Error() string {
	return "dbus: invalid message: " + string(e)
}

// fieldType are the types of the various header fields.
var fieldTypes = [fieldMax]reflect.Type{
	FieldPath:        objectPathType,
	FieldInterface:   stringType,
	FieldMember:      stringType,
	FieldErrorName:   stringType,
	FieldReplySerial: uint32Type,
	FieldDestination: stringType,
	FieldSender:      stringType,
	FieldSignature:   signatureType,
	FieldUnixFDs:     uint32Type,
}

// requiredFields lists the header fields that are required by the different
// message types.
var requiredFields = [typeMax][]HeaderField{
	TypeMethodCall:  {FieldPath, FieldMember},
	TypeMethodReply: {FieldReplySerial},
	TypeError:       {FieldErrorName, FieldReplySerial},
	TypeSignal:      {FieldPath, FieldInterface, FieldMember},
}

var reuseDecoder *decoder

// Message represents a single D-Bus message.
type Message struct {
	Type
	Flags
	Headers map[HeaderField]Variant
	Body    []interface{}

	serial uint32
}

type header struct {
	Field byte
	Variant
}

func DecodeMessageWithFDs(rd io.Reader, fds []int) (msg *Message, err error) {
	var order binary.ByteOrder

	b := make([]byte, 1)
	_, err = rd.Read(b)
	if err != nil {
		return
	}
	switch b[0] {
	case 'l':
		order = binary.LittleEndian
	case 'B':
		order = binary.BigEndian
	default:
		return nil, InvalidMessageError("invalid byte order")
	}

	if reuseDecoder == nil || reuseDecoder.order != order {
		reuseDecoder = newDecoder(rd, order, fds)
	} else {
		reuseDecoder.Reset(rd, order, fds)
	}
	dec := reuseDecoder
	dec.pos = 1

	msg = new(Message)
	msg.Type = Type(dec.decodeY())
	msg.Flags = Flags(dec.decodeY())
	// Right now we don't store the proto version
	_ = dec.decodeY()
	length := dec.decodeU()
	msg.serial = dec.decodeU()

	// get the header length separately because we need it later
	headerLength := dec.decodeU()
	if headerLength+length+16 > 1<<27 {
		return nil, InvalidMessageError("message is too long")
	}
	// Signals have 3 required headers. This will over alloc for the other message types, but not much
	msg.Headers = make(map[HeaderField]Variant, 3)
	spos := dec.pos
	header := header{}
	for dec.pos < spos+int(headerLength) {
		dec.align(8)
		header.Field = dec.decodeY()
		header.Variant = dec.decodeV(0)
		msg.Headers[HeaderField(header.Field)] = header.Variant
	}

	dec.align(8)
	body := make([]byte, int(length))
	if length != 0 {
		_, err := io.ReadFull(rd, body)
		if err != nil {
			return nil, err
		}
	}

	if err = msg.validateHeader(); err != nil {
		return nil, err
	}
	sig, _ := msg.Headers[FieldSignature].value.(Signature)
	if sig.str != "" {
		buf := bytes.NewBuffer(body)
		dec.Reset(buf, order, fds)
		vs, err := dec.Decode(sig)
		if err != nil {
			return nil, err
		}
		msg.Body = vs
	}

	return
}

// DecodeMessage tries to decode a single message in the D-Bus wire format
// from the given reader. The byte order is figured out from the first byte.
// The possibly returned error can be an error of the underlying reader, an
// InvalidMessageError or a FormatError.
func DecodeMessage(rd io.Reader) (msg *Message, err error) {
	return DecodeMessageWithFDs(rd, make([]int, 0))
}

func (msg *Message) CountFds() (int, error) {
	if len(msg.Body) == 0 {
		return 0, nil
	}
	return CountFDs(msg.Body...)
}

func (msg *Message) EncodeToWithFDs(out io.Writer, order binary.ByteOrder) (fds []int, err error) {
	if err := msg.validateHeader(); err != nil {
		return nil, err
	}
	endianByte := byte('l')
	if order == binary.BigEndian {
		endianByte = byte('B')
	}
	body := new(bytes.Buffer)
	fds = make([]int, 0)
	enc := newEncoder(body, order, fds)
	if len(msg.Body) != 0 {
		err = enc.Encode(msg.Body...)
		if err != nil {
			return
		}
	}
	headers := make([]header, 0, len(msg.Headers))
	for k, v := range msg.Headers {
		headers = append(headers, header{byte(k), v})
	}
	buf := bytes.NewBuffer(make([]byte, 0, 128))
	// No need to alloc a new encoder, just reset the old one
	enc.Reset(buf, order, enc.fds)
	buf.WriteByte(endianByte)
	buf.WriteByte(byte(msg.Type))
	buf.WriteByte(byte(msg.Flags))
	buf.WriteByte(protoVersion)
	enc.binWriteIntType(uint32(len(body.Bytes())))
	enc.binWriteIntType(msg.serial)
	enc.pos = 12
	err = enc.Encode(headers)
	if err != nil {
		return
	}
	enc.align(8)
	if buf.Len()+body.Len() > 1<<27 {
		return nil, InvalidMessageError("message is too long")
	}
	if _, err := buf.WriteTo(out); err != nil {
		return nil, err
	}
	if _, err := body.WriteTo(out); err != nil {
		return nil, err
	}
	return enc.fds, nil
}

// EncodeTo encodes and sends a message to the given writer. The byte order must
// be either binary.LittleEndian or binary.BigEndian. If the message is not
// valid or an error occurs when writing, an error is returned.
func (msg *Message) EncodeTo(out io.Writer, order binary.ByteOrder) (err error) {
	_, err = msg.EncodeToWithFDs(out, order)
	return err
}

// IsValid checks whether msg is a valid message and returns an
// InvalidMessageError or FormatError if it is not.
func (msg *Message) IsValid() error {
	return msg.EncodeTo(io.Discard, nativeEndian)
}

func (msg *Message) validateHeader() error {
	if msg.Flags & ^(FlagNoAutoStart|FlagNoReplyExpected|FlagAllowInteractiveAuthorization) != 0 {
		return InvalidMessageError("invalid flags")
	}
	if msg.Type == 0 || msg.Type >= typeMax {
		return InvalidMessageError("invalid message type")
	}
	for k, v := range msg.Headers {
		if k == 0 || k >= fieldMax {
			return InvalidMessageError("invalid header")
		}
		if reflect.TypeOf(v.value) != fieldTypes[k] {
			return InvalidMessageError("invalid type of header field")
		}
	}
	for _, v := range requiredFields[msg.Type] {
		if _, ok := msg.Headers[v]; !ok {
			return InvalidMessageError("missing required header")
		}
	}
	if path, ok := msg.Headers[FieldPath]; ok {
		if !path.value.(ObjectPath).IsValid() {
			return InvalidMessageError("invalid path name")
		}
	}
	if iface, ok := msg.Headers[FieldInterface]; ok {
		if !isValidInterface(iface.value.(string)) {
			return InvalidMessageError("invalid interface name")
		}
	}
	if member, ok := msg.Headers[FieldMember]; ok {
		if !isValidMember(member.value.(string)) {
			return InvalidMessageError("invalid member name")
		}
	}
	if errname, ok := msg.Headers[FieldErrorName]; ok {
		if !isValidInterface(errname.value.(string)) {
			return InvalidMessageError("invalid error name")
		}
	}
	if len(msg.Body) != 0 {
		if _, ok := msg.Headers[FieldSignature]; !ok {
			return InvalidMessageError("missing signature")
		}
	}

	return nil
}

// Serial returns the message's serial number. The returned value is only valid
// for messages received by eavesdropping.
func (msg *Message) Serial() uint32 {
	return msg.serial
}

// String returns a string representation of a message similar to the format of
// dbus-monitor.
func (msg *Message) String() string {
	if err := msg.IsValid(); err != nil {
		return "<invalid>"
	}
	s := msg.Type.String()
	if v, ok := msg.Headers[FieldSender]; ok {
		s += " from " + v.value.(string)
	}
	if v, ok := msg.Headers[FieldDestination]; ok {
		s += " to " + v.value.(string)
	}
	s += " serial " + strconv.FormatUint(uint64(msg.serial), 10)
	if v, ok := msg.Headers[FieldReplySerial]; ok {
		s += " reply_serial " + strconv.FormatUint(uint64(v.value.(uint32)), 10)
	}
	if v, ok := msg.Headers[FieldUnixFDs]; ok {
		s += " unixfds " + strconv.FormatUint(uint64(v.value.(uint32)), 10)
	}
	if v, ok := msg.Headers[FieldPath]; ok {
		s += " path " + string(v.value.(ObjectPath))
	}
	if v, ok := msg.Headers[FieldInterface]; ok {
		s += " interface " + v.value.(string)
	}
	if v, ok := msg.Headers[FieldErrorName]; ok {
		s += " error " + v.value.(string)
	}
	if v, ok := msg.Headers[FieldMember]; ok {
		s += " member " + v.value.(string)
	}
	if len(msg.Body) != 0 {
		s += "\n"
	}
	for i, v := range msg.Body {
		s += "  " + MakeVariant(v).String()
		if i != len(msg.Body)-1 {
			s += "\n"
		}
	}
	return s
}
