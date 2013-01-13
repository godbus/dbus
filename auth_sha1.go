package dbus

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"os"
	"os/user"
)

// AuthCookieSha1 implements the DBUS_COOKIE_SHA1 authentication mechanism.
type AuthCookieSha1 struct{}

func (a AuthCookieSha1) FirstData() ([]byte, AuthStatus) {
	u, err := user.Current()
	if err != nil {
		panic(err)
	}
	b := make([]byte, 2*len(u.Username))
	hex.Encode(b, []byte(u.Username))
	return b, AuthContinue
}

func (a AuthCookieSha1) HandleData(data []byte) ([]byte, AuthStatus) {
	challenge := make([]byte, len(data)/2)
	_, err := hex.Decode(challenge, data)
	if err != nil {
		return nil, AuthError
	}
	b := bytes.Split(challenge, []byte{' '})
	if len(b) != 3 {
		return nil, AuthError
	}
	context := b[0]
	id := b[1]
	svchallenge := b[2]
	cookie := a.getCookie(context, id)
	if cookie == nil {
		return nil, AuthError
	}
	clchallenge := a.generateChallenge()
	hash := sha1.New()
	hash.Write(bytes.Join([][]byte{svchallenge, clchallenge, cookie}, []byte{':'}))
	hexhash := make([]byte, 2*hash.Size())
	hex.Encode(hexhash, hash.Sum(nil))
	data = append(clchallenge, ' ')
	data = append(data, hexhash...)
	resp := make([]byte, 2*len(data))
	hex.Encode(resp, data)
	return resp, AuthOk
}

func (a AuthCookieSha1) getCookie(context, id []byte) []byte {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	file, err := os.Open(home + "/.dbus-keyrings/" + string(context))
	if err != nil {
		return nil
	}
	defer file.Close()
	rd := bufio.NewReader(file)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return nil
		}
		line = line[:len(line)-1]
		b := bytes.Split(line, []byte{' '})
		if len(b) != 3 {
			return nil
		}
		if bytes.Equal(b[0], id) {
			return b[2]
		}
	}
	panic("not reached")
}

func (a AuthCookieSha1) generateChallenge() []byte {
	b := make([]byte, 16)
	n, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	if n != 16 {
		panic(io.ErrUnexpectedEOF)
	}
	enc := make([]byte, 32)
	hex.Encode(enc, b)
	return enc
}
