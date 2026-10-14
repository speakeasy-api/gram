// Package inputconfig defines sensor message-context configuration.
package inputconfig

import "fmt"

const (
	// SingleMessage classifies only the target's content.
	SingleMessage = "single_message"

	// SlidingWindow supplies timestamp-ordered context ending at the target.
	SlidingWindow = "sliding_window"

	// DefaultWindowSize includes the target and up to four preceding messages.
	DefaultWindowSize = 5

	// MinWindowSize requires at least one contextual message in addition to the target.
	MinWindowSize = 2

	// MaxWindowSize bounds database, asset, and inference work per target.
	MaxWindowSize = 10
)

// Validate checks explicit, effective values at every authoring boundary.
func Validate(strategy string, size int) error {
	if strategy != SingleMessage && strategy != SlidingWindow {
		return fmt.Errorf("unknown sensor input strategy %q", strategy)
	}
	if size < MinWindowSize || size > MaxWindowSize {
		return fmt.Errorf("window size must be between %d and %d messages", MinWindowSize, MaxWindowSize)
	}
	return nil
}
