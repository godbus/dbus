// The UnixCredentials system call is currently only implemented on Linux
// http://golang.org/src/pkg/syscall/sockcmsg_linux.go
// https://groups.google.com/forum/#!topic/golang-dev/z7VbyqR1s78
// http://code.google.com/p/go/source/browse/unix/sockcmsg_linux.go?repo=sys

package dbus

import (
	"io"
	"os"
	"syscall"
)

func (t *unixTransport) SendNullByte() error {
	ucred := &syscall.Ucred{Pid: int32(os.Getpid()), Uid: uint32(os.Getuid()), Gid: uint32(os.Getgid())}
	b := syscall.UnixCredentials(ucred)
	_, oobn, err := t.UnixConn.WriteMsgUnix([]byte{0}, b, nil)
	if err != nil {
		return err
	}
	if oobn != len(b) {
		return io.ErrShortWrite
	}
	return nil
}
