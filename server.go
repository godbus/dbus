package dbus

import (
	"errors"
	"net"
	"strings"
)

// Server represents a server listening for and accepting new dbus
// clients at a particular interface
type Server interface {
	Accept() (*Conn, error)
	Uuid() string
}

type Handler interface {
	GotConnection(Server, *Conn)
}

type unixServer struct {
	listener *net.UnixListener
	uuid     string
}

func (s *unixServer) Uuid() string {
	return s.uuid
}

func (s *unixServer) Accept() (*Conn, error) {
	var err error

	t := new(unixTransport)
	t.EnableUnixFDs()
	t.UnixConn, err = s.listener.AcceptUnix()
	if err != nil {
		return nil, err
	}
	return newConn(t)
}

func newUnixServer(keys string, uuid string) (Server, error) {
	var err error

	abstract := getKey(keys, "abstract")
	path := getKey(keys, "path")

	s := new(unixServer)
	s.uuid = uuid
	switch {
	case abstract == "" && path == "":
		return nil, errors.New("dbus: invalid address (neither path nor abstract set)")
	case abstract != "" && path == "":
		s.listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: "@" + abstract, Net: "unix"})
		if err != nil {
			return nil, err
		}
		return s, nil
	case abstract == "" && path != "":
		s.listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, errors.New("dbus: invalid address (both path and abstract set)")
	}
}

// Runs a server loop accepting and authenticating new connections,
// calling GotConnection on the supplied Handler object. The
// authentication and the handler callback are run in a separate
// goroutine for each client connection.
func Serve(s Server, h Handler) {
	for {
		conn, err := s.Accept()
		if err == nil {
			go func() {
				h.GotConnection(s, conn)
			}()
		}
	}
}

// NewServer returns a new server object listening on the specified address.
func NewServer(address string, uuid string) (Server, error) {

	s := map[string]func(string, string) (Server, error){
		"unix": newUnixServer,
	}

	i := strings.IndexRune(address, ':')
	if i == -1 {
		return nil, errors.New("dbus: invalid bus address (no transport)")
	}

	f := s[address[:i]]
	if f == nil {
		return nil, errors.New("dbus: invalid bus address (invalid or unsupported transport)")
	}

	return f(address[i+1:], uuid)
}
