//go:build windows
// +build windows

package rpeat

import (
	"errors"
	"math"
	"os"
	"syscall"
)

func syscallSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func syscallGetpgid(pid int) (pgid int, err error) {
	pgid = int(math.Abs(float64(pid)))
	err = errors.New("unsupported group pid in windows")
	return
}

func syscallKill(pid int, signal os.Signal) error {
	p, err := os.FindProcess(int(math.Abs(float64(pid))))
	if err != nil {
		p.Kill()
	}
	return err
}
