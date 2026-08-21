//go:build !windows
// +build !windows

package rpeat

import (
	"os"
	"syscall"
)

func syscallSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func syscallGetpgid(pid int) (pgid int, err error) {
	pgid, err = syscall.Getpgid(pid)
	return
}

func syscallKill(pid int, signal os.Signal) error {
	return syscall.Kill(pid, signal.(syscall.Signal))
	//return syscall.Kill(pid, signal)
}
