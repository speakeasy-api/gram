//go:build linux

package runner

import "golang.org/x/sys/unix"

func getTotalMemoryGB() float64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	// Total physical memory in bytes (Totalram * Unit)
	totalRAM := info.Totalram * uint64(info.Unit)
	// Convert to GB
	return float64(totalRAM) / (1024 * 1024 * 1024)
}
