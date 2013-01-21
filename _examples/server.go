package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
	"os"
)

type foo string

func (f foo) Foo() (string, *dbus.ErrorMessage) {
	fmt.Println(f)
	return string(f), nil
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	reply, err := conn.RequestName("com.github.guelfey.Demo", dbus.FlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.NameReplyPrimaryOwner {
		fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	f := foo("Bar!")
	conn.Export(f, "/com/github/guelfey/Demo",
		&dbus.Interface{Name: "com.github.guelfey.Demo"})
	fmt.Println("Listening on com.github.guelfey.Demo / /com/github/guelfey/Demo ...")
	select {}
}
