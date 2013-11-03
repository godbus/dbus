package dbus

import (
	"os/exec"
)

func SessionBusPlatform() (*Conn, error) {
	cmd := exec.Command("launchctl", "getenv", "DBUS_LAUNCHD_SESSION_BUS_SOCKET")
	b, err := cmd.CombinedOutput()

	if err != nil {
		return nil, err
	}

	return Dial("unix:path=" + string(b[:len(b)-1]))
}
