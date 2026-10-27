//go:build codemodeintegration && !linux

package testenv

import "syscall"

// guardParentDeath has no portable equivalent outside Linux.
func guardParentDeath(*syscall.SysProcAttr) {}
