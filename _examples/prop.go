package main

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

type foo string

func (f foo) Foo() (string, *dbus.Error) {
	fmt.Println(f)
	return string(f), nil
}

type Foo struct {
	Id    int
	Value string
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	reply, err := conn.RequestName("com.github.guelfey.Demo",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	propsSpec := map[string]map[string]*prop.Prop{
		"com.github.guelfey.Demo": {
			"SomeInt": {
				int32(0),
				true,
				prop.EmitTrue,
				func(c *prop.Change) *dbus.Error {
					fmt.Println(c.Name, "changed to", c.Value)
					return nil
				},
			},
			"FooStruct": {
				Foo{Id: 1, Value: "First"},
				true,
				prop.EmitTrue,
				func(c *prop.Change) *dbus.Error {
					var foo Foo
					dbus.Store([]interface{}{c.Value}, &foo)
					fmt.Println(c.Name, "changed to", foo)
					return nil
				},
			},
		},
	}
	f := foo("Bar")
	conn.Export(f, "/com/github/guelfey/Demo", "com.github.guelfey.Demo")
	props := prop.New(conn, "/com/github/guelfey/Demo", propsSpec)
	n := &introspect.Node{
		Name: "/com/github/guelfey/Demo",
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       "com.github.guelfey.Demo",
				Methods:    introspect.Methods(f),
				Properties: props.Introspection("com.github.guelfey.Demo"),
			},
		},
	}
	conn.Export(introspect.NewIntrospectable(n), "/com/github/guelfey/Demo",
		"org.freedesktop.DBus.Introspectable")
	fmt.Println("Listening on com.github.guelfey.Demo / /com/github/guelfey/Demo ...")

	c := make(chan *dbus.Signal)
	conn.Signal(c)
	for _ = range c {
	}
}
