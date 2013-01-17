package main

import "github.com/guelfey/go.dbus"

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	msg := &dbus.CallMessage{
		Destination: "org.freedesktop.Notifications",
		Path:        "/org/freedesktop/Notifications",
		Interface:   "org.freedesktop.Notifications",
		Name:        "Notify",
		Args: []interface{}{"", uint32(0), "", "Test",
			"This is a test of the DBus bindings for go.", []string{},
			map[string]dbus.Variant{}, int32(5000)},
	}
	err = conn.Call(msg, 0).WaitReply()
	if err != nil {
		panic(err)
	}
}
