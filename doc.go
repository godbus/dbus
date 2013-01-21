/*
Package dbus implements bindings to the DBus message bus system, as well as the
corresponding encoding format.

For the message bus API, you first need to connect to a bus (usually the Session
or System bus). Then, you can call methods with Call() and receive signals over
the channel returned by Signals(). Handling method calls is even easier; using
Export(), you can arrange DBus message calls to be directly translated to method
calls on a Go value.

Decoder and Encoder provide direct access to the DBus wire format. You usually
don't need to use them directly. While you may use them directly on the socket
as they accept the standard io interfaces, it is not advised to do so as this
would generate many small reads / writes that could limit performance.

Rules for encoding are as follows:

1. Any primitive Go type that has a direct equivalent in the wire format
is directly converted. This includes all fixed size integers
except for int8, as well as float64, bool and string.

2. Slices and maps are converted to arrays and dicts, respectively.

3. Most structs are converted to the expected DBus struct (all exported members
are marshalled as a DBus struct). The exceptions are all types and structs
defined in this package that have a custom wire format. These are ObjectPath,
Signature and Variant. Also, fields whose tag contains dbus:"-" will be skipped.

4. Trying to encode any other type (including int and uint!) will result
in a panic. This applies to all functions that call (*Encoder).Encode somewhere.

The rules for decoding are mostly just the reverse of the encoding rules,
except for the handling of variants. If a struct is wrapped in a variant,
its decoded value will be a slice of interfaces which contain the struct
fields in the correct order.

Because encoding and decoding of messages need special handling, they are also
implemented here.

*/
package dbus

// BUG(guelfey): Unix file descriptor passing is not implemented.

// BUG(guelfey): The implementation does not conform to the official
// specification in that most restrictions of the protocol (structure depth,
// maximum message size etc.) are not checked.

// BUG(guelfey): Emitting signals is not implemented yet.

// BUG(guelfey): This package needs new reflection features that are only
// availabe from the hg tip until Go 1.1 is released.
