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
		err = conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
			"eavesdrop='true',type='" + v + "'").WaitReply()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to add match:", err)
			os.Exit(1)
		}
	}
	fmt.Println("Listening for everything")
	for {
		select {
		case v := <-conn.Signals():
			fmt.Println(v)
		case v := <-conn.Eavesdropped():
			fmt.Println(v)
		}
	}
}
