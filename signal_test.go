package dbus

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

const (
	interfaceName             = "com.github.godbus.SignalContentionTest"
	objectPath                = "/com/github/godbus/SignalContentionTest"
	getString                 = "GetString"
	emitSignalImmediately     = "EmitSignalImmediately"
	emitSignalAfterTwoSeconds = "EmitSignalAfterTwoSeconds"
	orderReceived             = "OrderReceived"
	orderReceivedFull         = interfaceName + "." + orderReceived
	intro                     = `
<node>
	<interface name="` + interfaceName + `">
		<method name="` + getString + `">
			<arg direction="out" type="s"/>
		</method>
		<method name="` + emitSignalImmediately + `"/>
		<method name="` + emitSignalAfterTwoSeconds + `"/>
		<signal name="` + orderReceived + `"/>
	</interface>`
)

type signalServer struct {
	conn *Conn
	t    *testing.T
}

func (server *signalServer) GetString() (string, *Error) {
	server.t.Log("Server:", getString, "called")
	defer server.t.Log("Server:", getString, "done")
	return "DBUS", nil
}

func (server *signalServer) EmitSignalImmediately() *Error {
	server.t.Log("Server:", emitSignalImmediately, "called")
	defer server.t.Log("Server:", emitSignalImmediately, "done")
	server.t.Log("Server: Emitting immediate", orderReceived, "signal")
	err := server.conn.Emit(objectPath, orderReceivedFull)
	if err != nil {
		panic(err)
	}
	return nil
}

func sleep(seconds int, ch chan<- struct{}) {
	time.Sleep(time.Second * time.Duration(seconds))
	if ch != nil {
		ch <- struct{}{}
	}
}

func (server *signalServer) EmitSignalAfterTwoSeconds() *Error {
	server.t.Log("Server:", emitSignalAfterTwoSeconds, "called")
	defer server.t.Log("Server:", emitSignalAfterTwoSeconds, "done")
	go func() {
		sleep(2, nil)
		server.t.Log("Server: Emitting belayed", orderReceived, "signal")
		err := server.conn.Emit(objectPath, orderReceivedFull)
		if err != nil {
			panic(err)
		}
	}()
	return nil
}

// privateSessionBus is like SessionBus but it creates new
// connection everytime instead of reusing the already created one.
func privateSessionBus() (conn *Conn, err error) {
	conn, err = SessionBusPrivate()
	if err != nil {
		return
	}
	if err = conn.Auth(nil); err != nil {
		conn.Close()
		conn = nil
		return
	}
	if err = conn.Hello(); err != nil {
		conn.Close()
		conn = nil
	}
	return
}

func getServer(t *testing.T) (*signalServer, error) {
	conn, err := privateSessionBus()
	if err != nil {
		return nil, err
	}
	reply, err := conn.RequestName(interfaceName,
		NameFlagDoNotQueue)
	if err != nil {
		return nil, err
	}
	if reply != RequestNameReplyPrimaryOwner {
		return nil, errors.New("name '" + interfaceName + "' already taken")
	}
	server := &signalServer{conn, t}
	err = conn.Export(server, objectPath, interfaceName)
	if err != nil {
		return nil, err
	}
	return server, nil
}

type TestData struct {
	conn   *Conn
	server *signalServer
	remote *Object
	ch1    chan *Signal
	ch2    chan *Signal
	t      *testing.T
}

// testSetup creates two separate connections to system bus, one for
// server, one for client, creates a server and adds two channels to
// client connection signal channels list.
//
// We are adding two unbufferred channels to signal channels list, so
// we can listen to both of them in select, handle one of them and
// then be sure that dbus signal handling is stuck on sending signal
// to other channel.
func testSetup(t *testing.T) *TestData {
	server, err := getServer(t)
	if err != nil {
		panic(err)
	}
	clientConn, err := privateSessionBus()
	if err != nil {
		panic(err)
	}
	clientConn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',path='"+objectPath+"',interface='"+interfaceName+"'")
	client := clientConn.Object(interfaceName, objectPath)
	ch1 := make(chan *Signal)
	ch2 := make(chan *Signal)
	clientConn.Signal(ch1)
	clientConn.Signal(ch2)

	return &TestData{
		conn:   clientConn,
		server: server,
		remote: client,
		ch1:    ch1,
		ch2:    ch2,
		t:      t,
	}
}

func cleanSetup(data *TestData) {
	data.server.conn.Close()
	data.conn.Close()
}

func logSignal(t *testing.T, signal *Signal) {
	if signal != nil {
		t.Log("Got signal")
		t.Log("Sender:", signal.Sender)
		t.Log("Path:", signal.Path)
		t.Log("Name:", signal.Name)
	} else {
		t.Log("Got a nil signal, possibly because channel got closed")
	}
}

func ensureOrderReceived(t *testing.T, signal *Signal) bool {
	path := string(signal.Path)
	if path != objectPath {
		t.Log(path, "!=", objectPath)
		return false
	}
	if signal.Name != orderReceivedFull {
		t.Log(signal.Name, "!=", orderReceivedFull)
		return false
	}
	t.Log("It is " + orderReceived + " signal")
	return true
}

// signalListener listens on both channels in test data. Panics if
// received signal is either nil or not an orderReceived one.
//
// The count parameter says how many signals it has to receive before
// quitting. Note that if there are two signal channels then passing 1
// for count means is going to receive a signal only from one of those
// channels.
//
// The got channel is optional and can be used to be notified when
// signal receiver got count signals.
//
// The stop channel is also optional and can be used to stop
// listening. Nothing is sent to got channel in this case.
func signalListener(data *TestData, count uint, got chan<- struct{}, stop <-chan struct{}) {
	for try := uint(0); try < count; try++ {
		var signal *Signal
		var ok bool

		select {
		case signal, ok = <-data.ch1:
		case signal, ok = <-data.ch2:
		case <-stop:
			return
		}

		logSignal(data.t, signal)
		if !ok || !ensureOrderReceived(data.t, signal) {
			panic(fmt.Sprint("Unknown signal received:", signal, "ok:", ok))
		}
	}
	if got != nil {
		got <- struct{}{}
	}
}

// bailOutCleanup tries to eat all the signals in order to avoid
// potential deadlock later during closing connections.
//
// We cannot assume that conn.Signal function works to remove channels
// from signal channels list and we cannot just close them, because
// conn.Close does that too. Double close results in panic.
func bailOutCleanup(data *TestData) {
	data.t.Log("Cleaning up to avoid potential deadlock during closing connection")
	go func() {
		defer func() {
			if err := recover(); err != nil {
				data.t.Log("Bail out, ignore panic:", err)
			}
		}()
		intMax := ^uint(0)
		signalListener(data, intMax, nil, nil)
	}()
}

// TestContentionClose tests if closing connection does not block on
// stuck sending to signal channel.
func TestContentionClose(t *testing.T) {
	data := testSetup(t)

	timeout := make(chan struct{}, 1)
	got := make(chan struct{}, 1)
	stop := make(chan struct{})
	go sleep(5, timeout)
	go signalListener(data, 1, got, stop)
	t.Log("Calling", emitSignalAfterTwoSeconds, "synchronously")
	data.remote.Call(emitSignalAfterTwoSeconds, 0)
	t.Log("Call done")
	select {
	case <-got:
	case <-timeout:
		stop <- struct{}{}
		cleanSetup(data)
		t.Fatal("Timeout triggered when waiting for " + orderReceived + " signal")
	}

	timeout = make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	go sleep(5, timeout)
	go func() {
		t.Log("Closing connection")
		data.conn.Close()
		t.Log("Close done")
		closed <- struct{}{}
	}()
	select {
	case <-closed:
		data.server.conn.Close()
	case <-timeout:
		t.Error("DEADLOCKED")
		bailOutCleanup(data)
		<-closed
		data.server.conn.Close()
		t.FailNow()
	}
}

// TestContentionAddSignal tests if adding new signal channel does not
// block on stuck sending to signal channel.
func TestContentionAddSignal(t *testing.T) {
	data := testSetup(t)
	defer cleanSetup(data)

	timeout := make(chan struct{}, 1)
	got := make(chan struct{}, 1)
	stop := make(chan struct{})
	go sleep(5, timeout)
	go signalListener(data, 1, got, stop)
	t.Log("Calling", emitSignalAfterTwoSeconds, "synchronously")
	data.remote.Call(emitSignalAfterTwoSeconds, 0)
	t.Log("Call done")
	select {
	case <-got:
	case <-timeout:
		stop <- struct{}{}
		t.Fatal("Timeout triggered when waiting for " + orderReceived + " signal")
	}

	timeout = make(chan struct{}, 1)
	added := make(chan struct{}, 1)
	go sleep(5, timeout)
	go func() {
		t.Log("Adding new signal channel")
		data.conn.Signal(make(chan *Signal, 1))
		t.Log("Adding done")
		added <- struct{}{}
	}()
	select {
	case <-added:
	case <-timeout:
		t.Error("DEADLOCKED")
		bailOutCleanup(data)
		t.FailNow()
	}
}

// TestContentionImmediateSignal tests if doing synchronous dbus call
// does not block on stuck sending to signal channel.
func TestContentionImmediateSignal(t *testing.T) {
	data := testSetup(t)
	defer cleanSetup(data)

	timeout := make(chan struct{}, 1)
	got := make(chan struct{}, 1)
	stop := make(chan struct{})
	done := make(chan struct{}, 1)
	go sleep(5, timeout)
	go signalListener(data, 1, got, stop)
	go func() {
		t.Log("Calling", emitSignalImmediately, "synchronously")
		data.remote.Call(emitSignalImmediately, 0)
		t.Log("Call done")
		done <- struct{}{}
	}()
	select {
	case <-got:
	case <-timeout:
		stop <- struct{}{}
		t.Fatal("Timeout triggered when waiting for " + orderReceived + " signal")
	}
	select {
	case <-timeout:
		t.Error("DEADLOCKED")
		bailOutCleanup(data)
		t.FailNow()
	case <-done:
		signalListener(data, 1, nil, nil)
	}
}

// TestContentionBelayedSignal tests if doing synchronous dbus call
// does not block on stuck sending to signal channel, more complicated
// edition.
func TestContentionBelayedSignal(t *testing.T) {
	data := testSetup(t)
	defer cleanSetup(data)

	timeout := make(chan struct{}, 1)
	got := make(chan struct{}, 1)
	stop := make(chan struct{})
	go sleep(7, timeout)
	go signalListener(data, 1, got, stop)
	t.Log("Calling", emitSignalAfterTwoSeconds, "synchronously")
	data.remote.Call(emitSignalAfterTwoSeconds, 0)
	t.Log("Call done")
	select {
	case <-timeout:
		stop <- struct{}{}
		t.Fatal("Timeout triggered when waiting for " + orderReceived + " signal")
	case <-got:
	}

	t.Log("Calling", getString, "asynchronously")
	call := data.remote.Go(getString, 0, make(chan *Call, 1))
	t.Log("Call done")
	timeout = make(chan struct{}, 1)
	go sleep(10, timeout)
	t.Log("Waiting for asynchronous call to finish")
	select {
	case <-call.Done:
		t.Log("Asynchronous call finished")
		signalListener(data, 1, nil, nil)
	case <-timeout:
		t.Error("DEADLOCKED")
		bailOutCleanup(data)
		t.FailNow()
	}
}

// callWithRecovery is primarily intended to handle panics happening
// during closing a connection because of double close.
func callWithRecovery(t *testing.T, f func()) {
	defer func() {
		err := recover()
		if err != nil {
			t.Fatal("PANIC, possibly channel double close error: ", err)
		}
	}()
	f()
}

// TestRemoveSignal tests if passing an already registered channel
// indeed removes it from the signal channels list.
//
// Failing to do so may end with double close on single channel, which
// results in panic.
func TestRemoveSignal(t *testing.T) {
	data := testSetup(t)
	defer callWithRecovery(t, func() { cleanSetup(data) })

	t.Log("Removing registered signal channel")
	data.conn.Signal(data.ch1)
	t.Log("Removing done")
	timeout := make(chan struct{}, 1)
	got := make(chan struct{}, 1)
	stop := make(chan struct{})
	go sleep(5, timeout)
	go signalListener(data, 1, got, stop)
	t.Log("Calling", emitSignalAfterTwoSeconds, "synchronously")
	data.remote.Call(emitSignalAfterTwoSeconds, 0)
	t.Log("Call done")
	select {
	case <-got:
		t.Log("Got signal as expected, there should be no more of them")
	case <-timeout:
		stop <- struct{}{}
		t.Fatal("Timeout triggered when waiting for " + orderReceived + " signal")
	}

	timeout = make(chan struct{}, 1)
	got = make(chan struct{}, 1)
	stop = make(chan struct{})
	go sleep(5, timeout)
	go signalListener(data, 1, got, stop)
	select {
	case <-got:
		t.Error("Expected only one signal reception, got at least two")
		bailOutCleanup(data)
		t.FailNow()
	case <-timeout:
		t.Log("Timeout triggered, means we received only signal on only one signal channel, as expected")
		stop <- struct{}{}
	}
}

// TestPreserveRemovedChannel tests if passing an already registered
// channel indeed removes it from the signal channels list. It is a
// bit different situation than in TestRemoveSignal, because we are
// not receiving any signals now. Removed signal channel should be
// usable even after closing connection to bus afterwards.
//
// Failing to do so may end with double close on single channel, which
// results in panic.
func TestPreserveRemovedChannel(t *testing.T) {
	conn, err := privateSessionBus()
	if err != nil {
		panic(err)
	}

	ch := make(chan *Signal, 1)
	t.Log("Adding new channel to signal channels list")
	conn.Signal(ch)
	t.Log("Adding done, removing it then")
	conn.Signal(ch)
	t.Log("Removing done, closing connection")
	callWithRecovery(t, func() { conn.Close() })
	t.Log("Closing done, checking channel")

	fail := make(chan struct{}, 1)
	success := make(chan struct{}, 1)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				fail <- struct{}{}
			}
		}()
		ch <- nil
		success <- struct{}{}
	}()
	select {
	case <-success:
		t.Log("Channel is still opened, as expected")
	case <-fail:
		t.Fatal("Removed channel was closed")
	}
}
