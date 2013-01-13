package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
	"os"
)

func handler(msg *dbus.CallMessage) (dbus.ReplyMessage, *dbus.ErrorMessage) {
	fmt.Println(msg)
	return []interface{}{}, nil
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
	conn.HandleCall("/com/github/guelfey/Demo", dbus.Handler(handler))
	select {}
}
