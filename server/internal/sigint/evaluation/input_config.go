package evaluation

import "fmt"

const (
	// InputStrategySingleMessage classifies only the target's content.
	InputStrategySingleMessage = "single_message"

	// InputStrategySlidingWindow supplies timestamp-ordered context ending at the target.
	InputStrategySlidingWindow = "sliding_window"

	// DefaultWindowSize includes the target and up to four preceding messages.
	DefaultWindowSize = 5

	// MinWindowSize requires at least one contextual message in addition to the target.
	MinWindowSize = 2

	// MaxWindowSize bounds database, asset, and inference work per target.
	MaxWindowSize = 10
)

// ValidateInputConfiguration checks effective sensor message-context settings.
func ValidateInputConfiguration(strategy string, size int) error {
	if strategy != InputStrategySingleMessage && strategy != InputStrategySlidingWindow {
		return fmt.Errorf("unknown sensor input strategy %q", strategy)
	}
	if size < MinWindowSize || size > MaxWindowSize {
		return fmt.Errorf("window size must be between %d and %d messages", MinWindowSize, MaxWindowSize)
	}
	return nil
}
