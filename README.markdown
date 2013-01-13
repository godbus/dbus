go.dbus
-------

go.dbus is a simple library that implements native Go client bindings for the
DBus message bus system

### Installation

```
go get github.com/guelfey/go.dbus
```

### Basic usage

```go
package main

import "github.com/guelfey/go.dbus"

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}

	// do stuff with conn
}
```

See the [documentation](godoc.org/github.com/guelfey/go.dbus) and _examples for
more information.

### License

go.dbus is available under the Simplified BSD License; see LICENSE for the full
text.
