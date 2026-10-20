//go:build unix && !linux

package agent

import "syscall"

func setParentDeathSignal(*syscall.SysProcAttr) {}
