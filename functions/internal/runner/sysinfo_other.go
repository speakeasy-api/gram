//go:build !linux

package runner

func getTotalMemoryGB() float64 {
	return 0
}
