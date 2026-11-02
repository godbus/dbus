package dbus

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// Exports and unexports on one connection run while calls are dispatched
// (method calls arrive each on their own goroutine). Run with -race.
func TestDefaultHandlerConcurrentExport(t *testing.T) {
	h := NewDefaultHandler()
	conn := &Conn{handler: h}
	methods := map[string]reflect.Value{
		"Ping": reflect.ValueOf(func() *Error { return nil }),
	}

	const workers, rounds = 4, 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		iface := fmt.Sprintf("org.example.Iface%d", w)
		wg.Add(1)
		go func(iface string) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := conn.export(methods, "/org/example/obj", iface, true); err != nil {
					t.Error(err)
					return
				}
				if err := conn.export(nil, "/org/example/obj", iface, true); err != nil {
					t.Error(err)
					return
				}
			}
		}(iface)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				h.PathExists("/org/example/obj")
				h.LookupObject("/org/example/obj/child")
				h.introspectPath("/org/example")
			}
		}()
	}
	wg.Wait()

	if h.PathExists("/org/example/obj") {
		t.Fatal("object still exported after every interface was removed")
	}
}

// Two exports of different interfaces on a new path must both be kept.
func TestDefaultHandlerConcurrentExportKeepsInterfaces(t *testing.T) {
	methods := map[string]reflect.Value{
		"Ping": reflect.ValueOf(func() *Error { return nil }),
	}
	for i := 0; i < 200; i++ {
		h := NewDefaultHandler()
		conn := &Conn{handler: h}
		path := ObjectPath("/org/example/obj")
		var wg sync.WaitGroup
		for _, iface := range []string{"org.example.A", "org.example.B"} {
			wg.Add(1)
			go func(iface string) {
				defer wg.Done()
				if err := conn.export(methods, path, iface, false); err != nil {
					t.Error(err)
				}
			}(iface)
		}
		wg.Wait()
		obj, ok := h.LookupObject(path)
		if !ok {
			t.Fatal("object missing")
		}
		for _, iface := range []string{"org.example.A", "org.example.B"} {
			if _, ok := obj.LookupInterface(iface); !ok {
				t.Fatalf("round %d: interface %s lost", i, iface)
			}
		}
	}
}
