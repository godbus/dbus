package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
)

func main() {
	conn, err := dbus.Dial("unix:path=./socket")
	if err != nil {
		panic(err)
	}
	fmt.Println("Connected on unix:path=./socket")

	err = conn.Auth(nil)
	if err != nil {
		panic(err)
	}

	obj := conn.Object("", "/com/github/guelfey/Demo/PeerServer")
	var r string
	err = obj.Call("com.github.guelfey.PeerServer.Foo", 0, int32(42)).Store(&r)
	fmt.Printf("Got reply: %s\n", r)
}
