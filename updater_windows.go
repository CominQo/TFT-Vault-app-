//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// restartApp starts the freshly installed exe, detached from this process.
func restartApp(exe string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	return cmd.Start()
}
