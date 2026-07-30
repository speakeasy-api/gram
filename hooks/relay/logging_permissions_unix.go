//go:build !windows

package relay

import "os"

func secureLogFile(file *os.File) error {
	return file.Chmod(0o600)
}
