//go:build windows

package relay

// piNonBlockingOpenFlag has no Windows equivalent: the CreateFile the runtime
// issues does not block on a named pipe the way a POSIX FIFO open does, and the
// descriptor is checked for being a regular file either way.
const piNonBlockingOpenFlag = 0
