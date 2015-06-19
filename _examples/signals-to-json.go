package main

/* Example usage, showing systemd unit property changes:
 *
 *   signals-to-json -systemBus=true -filter="type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',arg0='org.freedesktop.systemd1.Unit',path_namespace='/org/freedesktop/systemd1/unit'"
 *
 */

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/charles-dyfis-net/go-dbus"
	"os"
)

var cfg struct {
	Filter string
	SystemBus bool
}

func main() {
	var conn *dbus.Conn
	var err error

	flag.BoolVar(&cfg.SystemBus, "systemBus", false, "Use system rather than session bus")
	flag.StringVar(&cfg.Filter, "filter", "", "Filter to apply to select signals (default looks for systemd service property changes)")
	flag.Parse()

	if(flag.NArg() != 0) {
		fmt.Fprintf(os.Stderr, "Unrecognized argument seen\n")
		flag.Usage()
		os.Exit(1)
	}

	if(cfg.SystemBus) {
		conn, err = dbus.SystemBus()
	} else {
		conn, err = dbus.SessionBus()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to system bus:", err)
		os.Exit(1)
	}

	call := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, cfg.Filter)
	if(call.Err != nil) {
		panic(call.Err)
	}

	c := make(chan *dbus.Signal, 10)
	conn.Signal(c)
	for v := range c {
		data, err := json.Marshal(v)
		if(err != nil) {
			fmt.Fprintf(os.Stderr, "Unable to marshal message %v: %s\n", err)
		} else {
			fmt.Println(string(data))
		}
	}
}
