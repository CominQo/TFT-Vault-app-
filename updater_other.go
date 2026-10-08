//go:build !windows

package main

import "os/exec"

func restartApp(exe string) error { return exec.Command(exe).Start() }
