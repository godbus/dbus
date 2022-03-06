// The UnixCredentials system call is currently only implemented on Linux
// http://golang.org/src/pkg/syscall/sockcmsg_linux.go
// https://golang.org/s/go1.4-syscall
// http://code.google.com/p/go/source/browse/unix/sockcmsg_linux.go?repo=sys

// Local implementation of the UnixCredentials system call for FreeBSD

package dbus

import (
	"io"
	"syscall"
	"unsafe"
)

// http://golang.org/src/pkg/syscall/ztypes_linux_amd64.go
// https://golang.org/src/syscall/ztypes_freebsd_amd64.go
type Ucred struct {
	Pid int32
	Uid uint32
	Gid uint32
}

// https://github.com/freebsd/freebsd-src/blob/822d379b1f474b3d9e3a82a7ce7dad96990b55b0/sys/sys/socket.h#L490-L511
// https://github.com/freebsd/freebsd-src/blob/822d379b1f474b3d9e3a82a7ce7dad96990b55b0/sys/sys/_types.h#L118-L150
const (
	cmGroupMax     = 16
	SizeofCmsgcred = 4 /*pid_t	cmcred_pid */ +
		4 /*uid_t	cmcred_uid */ +
		4 /*uid_t	cmcred_euid */ +
		4 /*gid_t	cmcred_gid */ +
		4 /*short	cmcred_ngroups */ +
		4*cmGroupMax /*gid_t	cmcred_groups[CMGROUP_MAX] */
)

// UnixCredentials returns a socket control message
// for sending to another process. This can be used for
// authentication.
func UnixCredentials() []byte {
	// https://www.freebsd.org/cgi/man.cgi?query=unix&sektion=4#CONTROL%09MESSAGES
	// Credentials of the sending process can be transmitted explicitly using a
	// control message of type SCM_CREDS with a data portion of type struct
	// cmsgcred, defined in <sys/socket.h> as follows:
	//
	// struct cmsgcred {
	//   pid_t cmcred_pid;      /* PID of sending process */
	//   uid_t cmcred_uid;      /* real UID of sending process */
	//   uid_t cmcred_euid;      /* effective UID of sending process */
	//   gid_t cmcred_gid;      /* real GID of sending process */
	//   short cmcred_ngroups;      /* number of groups */
	//   gid_t cmcred_groups[CMGROUP_MAX];     /* groups */
	// };
	//
	// The sender should pass a zeroed buffer which will be filled in by the
	// system.

	b := make([]byte, syscall.CmsgSpace(SizeofCmsgcred))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&b[0]))
	h.Level = syscall.SOL_SOCKET
	h.Type = syscall.SCM_CREDS
	h.SetLen(syscall.CmsgLen(SizeofCmsgcred))
	return b
}

// http://golang.org/src/pkg/syscall/sockcmsg_linux.go
// ParseUnixCredentials decodes a socket control message that contains
// credentials in a Ucred structure. To receive such a message, the
// SO_PASSCRED option must be enabled on the socket.
func ParseUnixCredentials(m *syscall.SocketControlMessage) (*Ucred, error) {
	if m.Header.Level != syscall.SOL_SOCKET {
		return nil, syscall.EINVAL
	}
	if m.Header.Type != syscall.SCM_CREDS {
		return nil, syscall.EINVAL
	}
	ucred := *(*Ucred)(unsafe.Pointer(&m.Data[0]))
	return &ucred, nil
}

func (t *unixTransport) SendNullByte() error {
	b := UnixCredentials()
	_, oobn, err := t.UnixConn.WriteMsgUnix([]byte{0}, b, nil)
	if err != nil {
		return err
	}
	if oobn != len(b) {
		return io.ErrShortWrite
	}
	return nil
}
