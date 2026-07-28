//go:build !windows

package relay

import "syscall"

// drainSysProcAttr severs the detached drain from the hook's session and
// process group, so a provider that signals the hook's group on timeout
// can't take the drain down with it. Mirrors agenthooks' --async detach.
func drainSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
