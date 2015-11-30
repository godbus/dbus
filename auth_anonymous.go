package dbus

// AuthAnonymous returns an Auth that authenticates as an anonymous user
func AuthAnonymous() Auth {
	return authAnonymous{}
}

// authAnonymous implements the ANONYMOUS authentication mechanism.
type authAnonymous struct {
}

func (a authAnonymous) FirstData() ([]byte, []byte, AuthStatus) {
	return []byte("ANONYMOUS"), []byte(""), AuthOk
}

func (a authAnonymous) HandleData(b []byte) ([]byte, AuthStatus) {
	return nil, AuthError
}
