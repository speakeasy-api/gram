//go:build windows

package relay

import "syscall"

// detachedProcess is Windows' DETACHED_PROCESS creation flag, absent from
// the syscall package.
const detachedProcess = 0x00000008

// drainSysProcAttr severs the detached drain from the hook's console and
// process group, so a ctrl-event broadcast or group signal aimed at the
// hook can't take the drain down with it. Mirrors agenthooks' --async
// detach.
func drainSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess,
	}
}
