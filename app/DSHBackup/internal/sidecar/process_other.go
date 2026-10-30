//go:build !windows

package sidecar

import "os/exec"

func configureProcess(_ *exec.Cmd) {}

func attachProcessJob(_ int) (uintptr, error) { return 0, nil }

func terminateProcessJob(_ uintptr) {}

func closeProcessJob(_ uintptr) {}
