// +build !darwin

package dbus

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

var (
	dbusLaunchLock sync.Mutex
)

// startDbusDaemon starts a new dbus-daemon and returns its address or an error.
func startDbusDaemon() (string, error) {
	cmd := exec.Command("dbus-daemon", "--session", "--print-address",
		"--nofork", "--nopidfile")
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}

	// If possible kill the process when the parent exits. This
	// only works on Linux. On other systems dbus-daemon will
	// become orphan.
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM

	err = cmd.Start()
	if err != nil {
		return "", err
	}

	type Output struct {
		address string
		err     error
	}

	ch := make(chan Output)
	go func() {
		reader := bufio.NewReader(outPipe)
		line, err := reader.ReadString('\n')
		ch <- Output{line, err}
		close(ch)
	}()

	select {
	case out := <-ch:
		if out.err != nil {
			return "", out.err
		}
		return out.address, nil
	case <-time.After(time.Second):
		cmd.Process.Kill()
		return "", errors.New("cannot start dbus-daemon: timeout")
	}
}

func sessionBusPlatform() (*Conn, error) {
	dbusLaunchLock.Lock()
	defer dbusLaunchLock.Unlock()

	address := os.Getenv(sessionBusAddressEnv)
	if address != "" && address != "autolaunch:" {
		return Dial(address)
	}

	address, err := startDbusDaemon()
	if err != nil {
		return nil, err
	}
	os.Setenv(sessionBusAddressEnv, address)

	return Dial(address)
}
