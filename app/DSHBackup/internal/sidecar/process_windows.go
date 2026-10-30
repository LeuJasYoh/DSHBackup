//go:build windows

package sidecar

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureProcess(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

func attachProcessJob(pid int) (uintptr, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	ret, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil || ret == 0 {
		windows.CloseHandle(job)
		if err == nil {
			err = fmt.Errorf("SetInformationJobObject 未成功")
		}
		return 0, err
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(pid),
	)
	if err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("无法将子进程加入作业：%w", err)
	}
	return uintptr(job), nil
}

func terminateProcessJob(job uintptr) {
	if job == 0 {
		return
	}
	_ = windows.TerminateJobObject(windows.Handle(job), 1)
}

func closeProcessJob(job uintptr) {
	if job == 0 {
		return
	}
	_ = windows.CloseHandle(windows.Handle(job))
}
