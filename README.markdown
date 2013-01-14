go.dbus
-------

go.dbus is a simple library that implements native Go client bindings for the
DBus message bus system

### Installation

Because this package need some new reflection features, it currently requires a
Go runtime built from the hg tip. (This will change once Go 1.1 is released.)

If you have this, just run:

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

See the [documentation](http://godoc.org/github.com/guelfey/go.dbus) and _examples for
more information.

Please note that the API is considered unstable for now and may change without
further notice.

### License

go.dbus is available under the Simplified BSD License; see LICENSE for the full
text.
