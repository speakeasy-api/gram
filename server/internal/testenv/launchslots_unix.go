//go:build unix

package testenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// tryLockFile takes an exclusive, non-blocking lock on path. It reports false
// when another process holds the lock.
func tryLockFile(path string) (func(), bool, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file: %w", err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("flock: %w", err)
	}

	// Closing the file releases the lock.
	return func() { _ = f.Close() }, true, nil
}
