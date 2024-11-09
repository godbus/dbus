//go:build !darwin
// +build !darwin

package dbus

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path"
	"strings"
)

var execCommand = exec.Command

func getSessionBusPlatformAddress() (string, error) {
	rundir, ok := os.LookupEnv("XDG_RUNTIME_DIR")
	if !ok {
		return "", errors.New("dbus: couldn't determine address of session bus")
	}

	return path.Join(rundir, "bus"), nil
}

// tryDiscoverDbusSessionBusAddress tries to discover an existing dbus session
// and return the value of its DBUS_SESSION_BUS_ADDRESS.
// It tries different techniques employed by different operating systems,
// returning the first valid address it finds, or an empty string.
//
//   - /run/user/<uid>/bus           if this exists, it *is* the bus socket. present on
//     Ubuntu 18.04
//   - /run/user/<uid>/dbus-session: if this exists, it can be parsed for the bus
//     address. present on Ubuntu 16.04
//
// See https://dbus.freedesktop.org/doc/dbus-launch.1.html
func tryDiscoverDbusSessionBusAddress() string {
	if runtimeDirectory, err := getRuntimeDirectory(); err == nil {

		if runUserBusFile := path.Join(runtimeDirectory, "bus"); fileExists(runUserBusFile) {
			// if /run/user/<uid>/bus exists, that file itself
			// *is* the unix socket, so return its path
			return fmt.Sprintf("unix:path=%s", EscapeBusAddressValue(runUserBusFile))
		}
		if runUserSessionDbusFile := path.Join(runtimeDirectory, "dbus-session"); fileExists(runUserSessionDbusFile) {
			// if /run/user/<uid>/dbus-session exists, it's a
			// text file // containing the address of the socket, e.g.:
			// DBUS_SESSION_BUS_ADDRESS=unix:abstract=/tmp/dbus-E1c73yNqrG

			if f, err := os.ReadFile(runUserSessionDbusFile); err == nil {
				fileContent := string(f)

				prefix := "DBUS_SESSION_BUS_ADDRESS="

				if strings.HasPrefix(fileContent, prefix) {
					address := strings.TrimRight(strings.TrimPrefix(fileContent, prefix), "\n\r")
					return address
				}
			}
		}
	}
	return ""
}

func getRuntimeDirectory() (string, error) {
	if currentUser, err := user.Current(); err != nil {
		return "", err
	} else {
		return fmt.Sprintf("/run/user/%s", currentUser.Uid), nil
	}
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}
