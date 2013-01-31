go.dbus
-------

go.dbus is a simple library that implements native Go client bindings for the
DBus message bus system.

### Installation

Because this package needs some new reflection features, it currently requires a
Go runtime built from the hg tip. (This will change once Go 1.1 is released.)

If you have this, just run:

```
go get github.com/guelfey/go.dbus
```

### Basic usage

```go
package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}

	// Synchronous method call:
	var list []string
	err = conn.BusObj.Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}

	// Asynchronous method call:
	c := conn.BusObj.Call("org.freedesktop.DBus.ListActivatableNames", 0)
	select {
	case reply := <-c:
		if reply.Err != nil {
			panic(err)
		}
		list = reply.Values[0].([]string)
		for _, v := range list {
			fmt.Println(v)
		}
	// other cases...
	}

	// Signal emission:
	conn.Emit("/foo/bar", "foo.bar", "Baz", uint32(0xDEADBEEF))
}
```

See the [documentation](http://godoc.org/github.com/guelfey/go.dbus) and the 
[examples](https://github.com/guelfey/go.dbus/tree/master/_examples) for more
information.

Please note that the API is considered unstable for now and may change without
further notice.

### License

go.dbus is available under the Simplified BSD License; see LICENSE for the full
text.
