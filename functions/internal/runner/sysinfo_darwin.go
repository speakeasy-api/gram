//go:build darwin

package runner

import "golang.org/x/sys/unix"

func getTotalMemoryGB() float64 {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil {
		return 0
	}
	// Convert bytes to GB
	return float64(total) / (1024 * 1024 * 1024)
}
