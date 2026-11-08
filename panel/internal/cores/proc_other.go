//go:build !linux

package cores

import "syscall"

func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }
