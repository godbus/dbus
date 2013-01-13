package main

import "github.com/guelfey/go.dbus"

func main() {
    conn, err := dbus.ConnectSessionBus()
    if err != nil {
        panic(err)
    }
    err = conn.Call("org.freedesktop.Notifications",
		"/org/freedesktop/Notifications", "org.freedesktop.Notifications",
		"Notify", 0, "", uint32(0), "", "Test",
		"This is a test of the DBus bindings for Go.", []string{},
		map[string]dbus.Variant{}, int32(5000)).WaitReply()
    if err != nil {
        panic(err)
    }
}
