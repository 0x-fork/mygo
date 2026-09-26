//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the command in its own process group so the whole
// tree (e.g. a dev server and its workers) can be stopped.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminate asks a command started with setProcessGroup, and the processes
// it started, to exit.
func terminate(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGTERM) }

// kill stops them right away.
func kill(cmd *exec.Cmd) { signalGroup(cmd, syscall.SIGKILL) }

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, sig)
}
