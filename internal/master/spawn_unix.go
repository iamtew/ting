//go:build unix

package master

import (
	"os/exec"
	"syscall"
)

func isolateChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
