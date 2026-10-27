//go:build windows

package sidecar

import (
	"os/exec"
	"syscall"
)

func configureProcess(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
