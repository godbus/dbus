/*
Package dbus implements bindings to the DBus message bus system, as well as the
corresponding encoding format.

For the message bus API, you first need to connect to a bus (usually the Session
or System bus). Then, you can call methods with Call() and receive signals over
the channel returned by Signals(). Handling method calls is even easier; using
Handle(), you can arrange DBus message calls to be directly translated to method
calls on a Go value.

Decoder and Encoder provide direct access to the DBus wire format. You usually
don't need to use them directly. For the rules and caveats, see their respective
documentations. While you may use them directly on the socket as they accept the
standard io interfaces, it is not advised to do so as this would generate many
small reads / writes that could limit performance.

Because encoding and decoding of message needs special
handling, they are also implemented here.

*/
package dbus

// BUG(guelfey): Unix file descriptor passing is not implemented.

// BUG(guelfey): The implementation does not conform to the official
// specification in that most restrictions of the protocol (structure depth,
// maximum messegae size etc.) are not checked.

// BUG(guelfey): Emitting signals is not implemented yet.

// BUG(guelfey): This package needs new reflection features that are only
// availabe from the hg tip until Go 1.1 is released.
