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

	err = conn.Call(&dbus.CallMessage{"org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "AddMatch",
		[]interface{}{"type='signal',path='/org/freedesktop/DBus'"}}, 0).WaitReply()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to add match:", err)
		os.Exit(1)
	}
	fmt.Println("Listening for signals on /org/freedesktop/DBus...")
	for v := range conn.Signals() {
		if v.Name == "" {
			break
		}
		fmt.Println(v)
	}
}
