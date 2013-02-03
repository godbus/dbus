go.dbus
-------

go.dbus is a simple library that implements native Go client bindings for the
DBus message bus system.

### Features

* Connections are safe to use by multiple goroutines
* Multiple (as in "2") authentication mechanisms; you can also implement your own
* Support for the "server" side (handling method calls from peers)
* Asynchronous method calls
* Documentation

### Installation

Because this package needs some new reflection features, it currently requires a
Go runtime built from the hg tip. (This will change once Go 1.1 is released.)

If you have this, just run:

```
go get github.com/guelfey/go.dbus
```

### Usage

The snippets below and the
[examples](https://github.com/guelfey/go.dbus/tree/master/_examples) give a
short overview over the basic usage. The complete package documentation is
available at [godoc.org](http://godoc.org/github.com/guelfey/go.dbus).

Please note that the API is considered unstable for now and may change without
further notice.

### Some usage snippets

#### Connecting

```go
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
```

#### Synchronous method calls

```go
	var list []string
	err = conn.BusObj.Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}
```

#### Asynchronous method calls

```go
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
```

#### Signal emission

```go
	conn.Emit("/foo/bar", "foo.bar", "Baz", uint32(0xDEADBEEF))
```

#### Handling remote method calls
```go

import "github.com/guelfey/go.dbus"

type Arith struct{}

func (a Arith) Add(n, m uint32) (uint32, *dbus.Error) {
	return n+m, nil
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	var a Arith
	conn.Export(a, "/foo/bar", "foo.bar")

	// ...
}
```

### License

go.dbus is available under the Simplified BSD License; see LICENSE for the full
text.
