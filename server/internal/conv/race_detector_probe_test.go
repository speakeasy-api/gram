package conv_test

import (
	"sync"
	"testing"
)

// TestRaceDetectorProbe intentionally races to verify that CI rejects data races.
func TestRaceDetectorProbe(t *testing.T) {
	t.Parallel()

	var value int
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 2 {
		workers.Go(func() {
			<-start
			for range 1000 {
				value++
			}
		})
	}
	close(start)
	workers.Wait()
	t.Logf("raced value: %d", value)
}
