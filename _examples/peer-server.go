package main

import (
	"fmt"
	"github.com/guelfey/go.dbus"
	"github.com/guelfey/go.dbus/introspect"
	"os"
)

const intro = `
<node>
	<interface name="com.github.guelfey.PeerServer">
		<method name="Foo">
			<arg direction="in" type="i"/>
			<arg direction="out" type="s"/>
		</method>
	</interface>` + introspect.IntrospectDataString + `</node> `

type foo string

func (f foo) Foo(val int32) (string, *dbus.Error) {
	s := fmt.Sprintf("%s %d", f, val)
	fmt.Println(s)
	return s, nil
}

type handler struct {
	f foo
}

func (h handler) GotConnection(server dbus.Server, conn *dbus.Conn) {
	conn.Export(h.f, "/com/github/guelfey/Demo/PeerServer", "com.github.guelfey.PeerServer")
	conn.Export(introspect.Introspectable(intro), "/com/github/guelfey/Demo/PeerServer",
		"org.freedesktop.DBus.Introspectable")
	if err := conn.ServerAuth(nil, server.Uuid()); err != nil {
		panic(err)
	}
}

func main() {
	err := os.Remove("socket")
	if err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	server, err := dbus.NewServer("unix:path=./socket", "1234567890123456")
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on unix:path=./socket")

	h := handler{foo("Bar!")}
	dbus.Serve(server, h)
}
