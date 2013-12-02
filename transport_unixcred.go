// +build !darwin

package dbus

import (
	"errors"
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

func readMsg(file *os.File, p []byte, oob []byte) (n, oobn, flags int, sa syscall.Sockaddr, err error) {
	for {
		n, oobn, flags, sa, err = syscall.Recvmsg(int(file.Fd()), p, oob, 0)
		if err != nil {
			if err == syscall.EAGAIN {
				continue
			}
		}
		break
	}
	return
}

func (t *unixTransport) ReadNullByte() error {
	var oobBuf [4096]byte
	res := []byte{0}

	// There is currently no way to get at the underlying fd of a UnixConn, so
	// we can't set SO_PASSCRED on it. We can use File() to get a copy of it though.
	// Unfortunately that will not allow us to ReadMsgUnix, so we have to do that
	// manually
	file, err := t.File()
	if err != nil {
		return err
	}

	err = syscall.SetsockoptInt(int(file.Fd()), syscall.SOL_SOCKET, syscall.SO_PASSCRED, 1)
	if err != nil {
		return err
	}

	n, oobn, flags, _, err := readMsg(file, res, oobBuf[:])
	if err != nil {
		return err
	}

	if n == 0 {
		return io.ErrUnexpectedEOF
	}

	if flags&syscall.MSG_CTRUNC != 0 {
		return errors.New("dbus: control data truncated")
	}

	msgs, err := syscall.ParseSocketControlMessage(oobBuf[:oobn])
	if err != nil {
		return err
	}

	for _, msg := range msgs {
		cred, _ := syscall.ParseUnixCredentials(&msg)
		if cred != nil {
			t.hasPeerUid = true
			t.peerUid = cred.Uid
		}
	}

	return nil
}
