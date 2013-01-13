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

	reply, err := conn.Call("org.freedesktop.DBus", "/org/freedesktop/DBus",
		"org.freedesktop.DBus", "ListNames", 0).Reply()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to get list of owned names:", err)
		os.Exit(1)
	}

	list, ok := reply[0].([]string)
	if !ok {
		fmt.Fprintln(os.Stderr, "ListNames has invalid response type")
		os.Exit(1)
	}

	fmt.Println("Currently owned names on the session bus:")
	for _, v := range list {
		fmt.Println(v)
	}
}
