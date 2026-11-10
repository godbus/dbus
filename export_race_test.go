package dbus

import (
	"fmt"
	"sync"
	"testing"
)

type raceObject struct{}

func (raceObject) Ping() *Error { return nil }

var raceInterfaces = []string{"org.example.A", "org.example.B"}

// Exports, unexports, lookups and introspection of the object table run at once, as they do when
// one goroutine exports objects while the connection dispatches incoming calls.
func TestExportConcurrentWithDispatch(t *testing.T) {
	h := NewDefaultHandler()
	conn := &Conn{handler: h}
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(2)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				path := ObjectPath(fmt.Sprintf("/org/example/w%d/o%d", w, i%4))
				for _, iface := range raceInterfaces {
					if err := conn.Export(raceObject{}, path, iface); err != nil {
						t.Error(err)
					}
				}
				for _, iface := range raceInterfaces {
					if err := conn.Export(nil, path, iface); err != nil {
						t.Error(err)
					}
				}
			}
		}(w)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				h.introspectPath("/org/example")
				h.LookupObject(ObjectPath(fmt.Sprintf("/org/example/w%d/o%d/child", w, i%4)))
			}
		}(w)
	}
	wg.Wait()
}

// Two interfaces exported at once on a new path both stay exported.
func TestExportSamePathConcurrently(t *testing.T) {
	for i := 0; i < 200; i++ {
		h := NewDefaultHandler()
		conn := &Conn{handler: h}
		var wg sync.WaitGroup
		for _, iface := range raceInterfaces {
			wg.Add(1)
			go func(iface string) {
				defer wg.Done()
				if err := conn.Export(raceObject{}, "/org/example", iface); err != nil {
					t.Error(err)
				}
			}(iface)
		}
		wg.Wait()
		obj, _ := h.LookupObject("/org/example")
		for _, iface := range raceInterfaces {
			if _, ok := obj.LookupInterface(iface); !ok {
				t.Fatalf("run %d: %s was lost", i, iface)
			}
		}
	}
}
