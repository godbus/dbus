package dbus

import (
	"io/ioutil"
	"os"
	"testing"
)

const testString = `This is a test!
This text should be read from the file that is created by this test.`

type unixFDTest struct{}

func (t unixFDTest) Test(fd UnixFD) (string, *Error) {
	var b [4096]byte
	file := os.NewFile(uintptr(fd), "testfile")
	defer file.Close()
	_, err := file.Seek(0, 0)
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	n, err := file.Read(b[:])
	if err != nil {
		return "", &Error{"com.github.guelfey.test.Error", nil}
	}
	return string(b[:n]), nil
}

func TestUnixFDs(t *testing.T) {
	conn, err := ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	file, err := ioutil.TempFile("", "go.dbus-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write([]byte(testString)); err != nil {
		t.Fatal(err)
	}
	name := conn.Names()[0]
	test := unixFDTest{}
	conn.Export(test, "/com/github/guelfey/test", "com.github.guelfey.test")
	var s string
	obj := conn.Object(name, "/com/github/guelfey/test")
	err = obj.Call("com.github.guelfey.test.Test", 0, UnixFD(file.Fd())).Store(&s)
	if err != nil {
		t.Fatal(err)
	}
	if s != testString {
		t.Fatal("got", s, "wanted", testString)
	}
}
