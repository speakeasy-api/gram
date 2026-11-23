//go:build codemodeintegration && linux

package testenv

import "syscall"

// guardParentDeath kills the runner when the test binary dies, since a killed
// test never runs its cleanup. The signal follows the thread that started the
// runner; Go keeps that thread alive because it is never locked to a goroutine.
func guardParentDeath(attr *syscall.SysProcAttr) {
	attr.Pdeathsig = syscall.SIGKILL
}
