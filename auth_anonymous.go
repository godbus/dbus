package dbus

import (
)

// AuthAnonymous returns an Auth that authenticates with the ANONYMOUS mechanism
func AuthAnonymous() Auth {
	return authAnonymous{}
}

// AuthAnonymous implements the ANONYMOUS authentication mechanism.
type authAnonymous struct {
}

func (a authAnonymous) FirstData() ([]byte, []byte, AuthStatus) {
	return []byte("ANONYMOUS"), []byte(""), AuthOk
}

func (a authAnonymous) HandleData(b []byte) ([]byte, AuthStatus) {
	return nil, AuthError
}
