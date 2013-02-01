package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
	"os"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to session bus:", err)
		os.Exit(1)
	}

	for _, v := range []string{"method_call", "method_return", "error", "signal"} {
		reply := <-conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
			"eavesdrop='true',type='"+v+"'")
		if reply.Err != nil {
			fmt.Fprintln(os.Stderr, "Failed to add match:", reply.Err)
			os.Exit(1)
		}
	}
	c := make(chan *dbus.Message, 10)
	conn.Eavesdrop(c)
	fmt.Println("Listening for everything")
	for v := range c {
		fmt.Println(v)
	}
}
