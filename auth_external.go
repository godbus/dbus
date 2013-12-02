package dbus

import (
	"encoding/hex"
	"os/user"
	"strconv"
)

// AuthExternal returns an Auth that authenticates as the given user with the
// EXTERNAL mechanism.
func AuthExternal(user string) Auth {
	return authExternal{user}
}

// AuthExternal implements the EXTERNAL authentication mechanism.
type authExternal struct {
	user string
}

func (a authExternal) FirstData() ([]byte, []byte, AuthStatus) {
	b := make([]byte, 2*len(a.user))
	hex.Encode(b, []byte(a.user))
	return []byte("EXTERNAL"), b, AuthOk
}

func (a authExternal) HandleData(b []byte) ([]byte, AuthStatus) {
	return nil, AuthError
}

// ServerAuthExternal implements the EXTERNAL authentication mechanism on the server side.
// If callback is specified it decides whether authenticating as a particular uid is
// allowed, otherwise we allow root and the same user as the server process.
func ServerAuthExternal(callback func(uid uint32) bool) ServerAuth {
	return serverAuthExternal{callback}
}

type serverAuthExternal struct {
	allowUserCallback func(uid uint32) bool
}

func (a serverAuthExternal) Name() string {
	return "EXTERNAL"
}

func (a serverAuthExternal) Supported(tr transport) bool {
	trUnix, isOk := tr.(*unixTransport)
	return isOk && trUnix.hasPeerUid
}

func (a serverAuthExternal) HandleAuth(b []byte, tr transport) ([]byte, ServerAuthStatus) {
	trUnix, isOk := tr.(*unixTransport)
	if !isOk {
		return nil, ServerAuthRejected
	}

	userStr, err := hex.DecodeString(string(b))
	if err != nil {
		return nil, ServerAuthRejected
	}

	uid, err := strconv.ParseUint(string(userStr), 10, 32)
	if err != nil {
		userData, err := user.Lookup(string(userStr))
		if err != nil {
			return nil, ServerAuthRejected
		}
		uid, err = strconv.ParseUint(userData.Uid, 10, 32)
		if err != nil {
			return nil, ServerAuthRejected
		}
	}

	// Verify that the user is who he claims to be
	if !trUnix.hasPeerUid || trUnix.peerUid != uint32(uid) {
		return nil, ServerAuthRejected
	}

	if a.allowUserCallback != nil {
		if a.allowUserCallback(uint32(uid)) {
			return nil, ServerAuthOk
		}
	} else {
		/* Default: Allow same user or root */
		if uid == 0 {
			return nil, ServerAuthOk
		}

		u, err := user.Current()
		if err == nil {
			currentUid, err := strconv.ParseUint(u.Uid, 10, 32)
			if err == nil && currentUid == uid {
				return nil, ServerAuthOk
			}
		}
	}

	return nil, ServerAuthRejected
}

func (a serverAuthExternal) HandleData(b []byte) ([]byte, ServerAuthStatus) {
	return nil, ServerAuthRejected
}
