/*
Package dbus implements bindings to the DBus message bus system, as well as the
corresponding encoding format.

To use the message bus API, you first need to connect to a bus (usually the
session or system bus). The acquired connection then can be used to call methods
on remote objects and emit or receive signals. Using the Export method, you can
arrange D-Bus methods calls to be directly translated to method calls on a Go
value.

Conversion Rules

For outgoing messages, Go types are automatically converted to the
corresponding D-Bus types. The following types are directly encoded as their
respective D-Bus equivalents:

     Go type     | D-Bus type
     ------------+-----------
     byte        | BYTE
     bool        | BOOLEAN
     int16       | INT16
     uint16      | UINT16
     int32       | INT32
     uint32      | UINT32
     int64       | INT64
     uint64      | UINT64
     float64     | DOUBLE
     string      | STRING
     ObjectPath  | OBJECT_PATH
     Signature   | SIGNATURE
     Variant     | VARIANT
     UnixFDIndex | UNIX_FD

Slices and arrays encode as ARRAYs of their element type.

Maps encode as DICTs, provided that their key type can be used as a key for
a DICT.

Structs other than Variant and Signature encode as a STRUCT containing their
exported fields. Fields whose tags contain `dbus:"-"` and unexported fields will
be skipped.

Pointers encode as the value they're pointed to.

Trying to encode any other type or a slice, map or struct containing an
unsupported type will result in an InvalidTypeError.

For incoming messages, the inverse of these rules are used, with the following
exceptions:

    - If an incoming STRUCT can't be directly converted to a Go struct (because
      it is, for example, wrapped in a VARIANT), it is stored as a []interface{}
      containing the struct fields in the correct order. The Store function can
      be used to convert such values to the proper structs.
    - When decoding into a pointer, the pointer is followed unless it is nil, in
      which case a new value for it to point to is allocated.
    - When decoding into a slice, the decoded values are appended to it.
    - Arrays cannot be decoded into.

Unix FD passing

Handling Unix file descriptors deserves special mention. To use them, you should
first check that they are supported on a connection by calling SupportsUnixFDs.
If it returns true, all method of Connection will translate messages containing
UnixFD's to messages that are accompanied by the given file descriptors with the
UnixFD values being substituted by the correct indices. Similarily, the indices
of incoming messages are automatically resolved. It shouldn't be necessary to use
UnixFDIndex.

*/
package dbus
