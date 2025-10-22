//go:build !linux && !darwin

package runner

func getTotalMemoryGB() float64 {
	return 0
}
