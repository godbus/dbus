package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
	"os"
)

const intro = `
<node>
	<interface name="com.github.guelfey.Demo">
		<method name="Foo">
			<arg direction="out" type="s"/>
		</method>
	</interface>
</node>
`

type foo string

func (f foo) Foo() (string, *dbus.Error) {
	fmt.Println(f)
	return string(f), nil
}

func main() {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	reply, err := conn.RequestName("com.github.guelfey.Demo",
		dbus.NameFlagDoNotQueue)
	if err != nil {
		panic(err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		fmt.Fprintln(os.Stderr, "name already taken")
		os.Exit(1)
	}
	f := foo("Bar!")
	conn.Export(f, "/com/github/guelfey/Demo", "com.github.guelfey.Demo")
	conn.SetIntrospect("/com/github/guelfey/Demo", intro)
	fmt.Println("Listening on com.github.guelfey.Demo / /com/github/guelfey/Demo ...")
	select {}
}
