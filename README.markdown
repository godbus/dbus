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

This packages requires Go 1.1. If you installed it and set up your GOPATH, just run:

```
go get github.com/guelfey/go.dbus
```

### Usage

The complete package documentation and some simple examples are available at
[godoc.org](http://godoc.org/github.com/guelfey/go.dbus). Also, the
[_examples](https://github.com/guelfey/go.dbus/tree/master/_examples) directory
gives a short overview over the basic usage. 

Please note that the API is considered unstable for now and may change without
further notice.

### License

go.dbus is available under the Simplified BSD License; see LICENSE for the full
text.
