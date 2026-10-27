//go:build !windows

package sidecar

import "os/exec"

func configureProcess(_ *exec.Cmd) {}
