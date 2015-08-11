package dbus

import "testing"

func TestSessionBus(t *testing.T) {
	_, err := SessionBus()
	if err != nil {
		t.Error(err)
	}
}

// TestSessionBusPrivate creates two session bus connections and verifies that
// objects exported via one connection are visible via the other.
func TestSessionBusPrivate(t *testing.T) {
	const objName = "com.github.godbus.BusTest"

	conn1, err := SessionBusPrivate()
	if err != nil {
		t.Fatal(err)
	}
	conn2, err := SessionBusPrivate()
	if err != nil {
		t.Fatal(err)
	}
	if conn1 == conn2 {
		t.Fatal("SessionBusPrivate returned the same object twice")
	}
	if err = conn1.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err = conn1.Hello(); err != nil {
		t.Fatal(err)
	}
	if err = conn2.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err = conn2.Hello(); err != nil {
		t.Fatal(err)
	}

	reply, err := conn1.RequestName(objName, NameFlagDoNotQueue)
	if err != nil {
		t.Fatal(err)
	}
	if reply != RequestNameReplyPrimaryOwner {
		t.Fatalf("%s: name already taken", objName)
	}

	var names []string
	err = conn2.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names)
	if err != nil {
		t.Fatalf("Failed to get list of owned names:", err)
	}

	for _, name := range names {
		if objName == name {
			return
		}
	}
	t.Errorf("%s is missing from the list of known objects: %s", objName, names)
}

func TestSystemBus(t *testing.T) {
	_, err := SystemBus()
	if err != nil {
		t.Error(err)
	}
}

func TestSend(t *testing.T) {
	bus, err := SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan *Call, 1)
	msg := &Message{
		Type:  TypeMethodCall,
		Flags: 0,
		Headers: map[HeaderField]Variant{
			FieldDestination: MakeVariant(bus.Names()[0]),
			FieldPath:        MakeVariant(ObjectPath("/org/freedesktop/DBus")),
			FieldInterface:   MakeVariant("org.freedesktop.DBus.Peer"),
			FieldMember:      MakeVariant("Ping"),
		},
	}
	call := bus.Send(msg, ch)
	<-ch
	if call.Err != nil {
		t.Error(call.Err)
	}
}

type server struct{}

func (server) Double(i int64) (int64, *Error) {
	return 2 * i, nil
}

func BenchmarkCall(b *testing.B) {
	b.StopTimer()
	var s string
	bus, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}
	name := bus.Names()[0]
	obj := bus.BusObject()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err := obj.Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&s)
		if err != nil {
			b.Fatal(err)
		}
		if s != name {
			b.Errorf("got %s, wanted %s", s, name)
		}
	}
}

func BenchmarkCallAsync(b *testing.B) {
	b.StopTimer()
	bus, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}
	name := bus.Names()[0]
	obj := bus.BusObject()
	c := make(chan *Call, 50)
	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			v := <-c
			if v.Err != nil {
				b.Error(v.Err)
			}
			s := v.Body[0].(string)
			if s != name {
				b.Errorf("got %s, wanted %s", s, name)
			}
		}
		close(done)
	}()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		obj.Go("org.freedesktop.DBus.GetNameOwner", 0, c, name)
	}
	<-done
}

func BenchmarkServe(b *testing.B) {
	b.StopTimer()
	srv, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}
	cli, err := SessionBusPrivate()
	if err != nil {
		b.Fatal(err)
	}
	if err = cli.Auth(nil); err != nil {
		b.Fatal(err)
	}
	if err = cli.Hello(); err != nil {
		b.Fatal(err)
	}
	benchmarkServe(b, srv, cli)
}

func BenchmarkServeAsync(b *testing.B) {
	b.StopTimer()
	srv, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}
	cli, err := SessionBusPrivate()
	if err != nil {
		b.Fatal(err)
	}
	if err = cli.Auth(nil); err != nil {
		b.Fatal(err)
	}
	if err = cli.Hello(); err != nil {
		b.Fatal(err)
	}
	benchmarkServeAsync(b, srv, cli)
}

func BenchmarkServeSameConn(b *testing.B) {
	b.StopTimer()
	bus, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}

	benchmarkServe(b, bus, bus)
}

func BenchmarkServeSameConnAsync(b *testing.B) {
	b.StopTimer()
	bus, err := SessionBus()
	if err != nil {
		b.Fatal(err)
	}

	benchmarkServeAsync(b, bus, bus)
}

func benchmarkServe(b *testing.B, srv, cli *Conn) {
	var r int64
	var err error
	dest := srv.Names()[0]
	srv.Export(server{}, "/org/guelfey/DBus/Test", "org.guelfey.DBus.Test")
	obj := cli.Object(dest, "/org/guelfey/DBus/Test")
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err = obj.Call("org.guelfey.DBus.Test.Double", 0, int64(i)).Store(&r)
		if err != nil {
			b.Fatal(err)
		}
		if r != 2*int64(i) {
			b.Errorf("got %d, wanted %d", r, 2*int64(i))
		}
	}
}

func benchmarkServeAsync(b *testing.B, srv, cli *Conn) {
	dest := srv.Names()[0]
	srv.Export(server{}, "/org/guelfey/DBus/Test", "org.guelfey.DBus.Test")
	obj := cli.Object(dest, "/org/guelfey/DBus/Test")
	c := make(chan *Call, 50)
	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			v := <-c
			if v.Err != nil {
				b.Fatal(v.Err)
			}
			i, r := v.Args[0].(int64), v.Body[0].(int64)
			if 2*i != r {
				b.Errorf("got %d, wanted %d", r, 2*i)
			}
		}
		close(done)
	}()
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		obj.Go("org.guelfey.DBus.Test.Double", 0, c, int64(i))
	}
	<-done
}
