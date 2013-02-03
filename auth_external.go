package dbus

import (
	"encoding/hex"
	"os/user"
)

// AuthExternal implements the EXTERNAL authentication mechanism.
type AuthExternal struct{}

func (a AuthExternal) FirstData() ([]byte, AuthStatus) {
	u, err := user.Current()
	if err != nil {
		return nil, AuthError
	}
	b := make([]byte, 2*len(u.Username))
	hex.Encode(b, []byte(u.Username))
	return b, AuthOk
}

func (a AuthExternal) HandleData(b []byte) ([]byte, AuthStatus) {
	return nil, AuthError
}
