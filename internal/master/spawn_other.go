//go:build !unix && !windows

package master

import "os/exec"

func isolateChild(*exec.Cmd) {}
