//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// detach makes the daemon survive the parent CLI exiting (new session, no tty).
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	_ = os.Stdout
}
