package main

import (
	"encoding/json"
	"github.com/guelfey/go.dbus"
	"os"
)

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	node, err := conn.Introspect("/org/freedesktop/DBus", "org.freedesktop.DBus")
	if err != nil {
		panic(err)
	}
	data, _ := json.MarshalIndent(node, "", "    ")
	os.Stdout.Write(data)
}
