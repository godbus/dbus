package dbus

import "fmt"

func ExampleConnection_Emit() {
	conn, err := ConnectSessionBus()
	if err != nil {
		panic(err)
	}

	conn.Emit("/foo/bar", "foo.bar.Baz", uint32(0xDAEDBEEF))
}

func ExampleCookie() {
	conn, err := ConnectSessionBus()
	if err != nil {
		panic(err)
	}

	c := conn.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0)
	select {
	case reply := <-c:
		if reply.Err != nil {
			panic(err)
		}
		list := reply.Body[0].([]string)
		for _, v := range list {
			fmt.Println(v)
		}
		// put some other cases here
	}
}

func ExampleObject_Call() {
	var list []string

	conn, err := ConnectSessionBus()
	if err != nil {
		panic(err)
	}

	err = conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&list)
	if err != nil {
		panic(err)
	}
	for _, v := range list {
		fmt.Println(v)
	}
}
