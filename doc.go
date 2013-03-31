/*
Package dbus implements bindings to the DBus message bus system, as well as the
corresponding encoding format.

For the message bus API, you first need to connect to a bus (usually the session
or system bus). Then, call methods by getting an Object and then calling Go or
Call on it. Signals can be received by passing a channel to (*Connection).Signal
and can be emitted via (*Connection).Emit.

Handling method calls is even easier; using (*Connection).Export, you can
arrange DBus message calls to be directly translated to method calls on a Go
value.

Unix FD passing deserves special mention. To use it, you should first check that
it is supported on a connection by calling SupportsUnixFDs. If it returns true,
all method of Connection will translate messages containing UnixFD's to messages
that are accompanied by the given file descriptors with the UnixFD values being
substituted by the correct indices. Similarily, the indices of incoming messages
are automatically resolved. It shouldn't be necessary to use UnixFDIndex.

Decoder and Encoder provide direct access to the DBus wire format. You usually
don't need to use them. While you may use them directly on the socket
as they accept the standard io interfaces, it is not advised to do so as this
would generate many small reads / writes that could limit performance. See their
respective documentation for the conversion rules.

Because encoding and decoding of messages need special handling, they are also
implemented here.

*/
package dbus

// BUG(guelfey): This package needs new reflection features that are only
// availabe from the hg tip until Go 1.1 is released.
