//go:build windows

package master

import (
	"os/exec"
	"syscall"
)

func isolateChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
