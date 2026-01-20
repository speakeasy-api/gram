package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Tests modify shared package-level variables (isTerminalFunc, openURLFunc)
func TestOpenDeploymentURL(t *testing.T) {
	tests := []struct {
		name           string
		isTerminal     bool
		openURLErr     error
		expectOpenCall bool
	}{
		{
			name:           "it opens URL when running in TTY",
			isTerminal:     true,
			openURLErr:     nil,
			expectOpenCall: true,
		},
		{
			name:           "it does not open URL when not running in TTY",
			isTerminal:     false,
			openURLErr:     nil,
			expectOpenCall: false,
		},
		{
			name:           "it logs error when open fails in TTY",
			isTerminal:     true,
			openURLErr:     errors.New("failed to open browser"),
			expectOpenCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original functions
			origIsTerminal := isTerminalFunc
			origOpenURL := openURLFunc
			t.Cleanup(func() {
				isTerminalFunc = origIsTerminal
				openURLFunc = origOpenURL
			})

			// Set up mocks
			isTerminalFunc = func() bool { return tt.isTerminal }

			var openCalled bool
			var openedURL string
			openURLFunc = func(url string) error {
				openCalled = true
				openedURL = url
				return tt.openURLErr
			}

			// Execute
			logger := slog.Default()
			ctx := context.Background()
			testURL := "https://app.getgram.ai/org/project/deployments/123"

			openDeploymentURL(logger, ctx, testURL)

			// Verify
			require.Equal(t, tt.expectOpenCall, openCalled, "openURL call expectation mismatch")
			if tt.expectOpenCall {
				require.Equal(t, testURL, openedURL, "opened URL mismatch")
			}
		})
	}
}

func TestProcessingMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		elapsed  time.Duration
		expected string
	}{
		{
			name:     "it shows message for first 10 seconds",
			elapsed:  5 * time.Second,
			expected: "processing (5s)...",
		},
		{
			name:     "it shows message at 10 seconds",
			elapsed:  10 * time.Second,
			expected: "processing (10s)...",
		},
		{
			name:     "it shows message at 15 seconds (multiple of 5)",
			elapsed:  15 * time.Second,
			expected: "still processing (15s)...",
		},
		{
			name:     "it returns empty at 12 seconds (not multiple of 5)",
			elapsed:  12 * time.Second,
			expected: "",
		},
		{
			name:     "it shows message at 20 seconds (multiple of 5)",
			elapsed:  20 * time.Second,
			expected: "still processing (20s)...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := processingMessage(tt.elapsed)
			require.Equal(t, tt.expected, result)
		})
	}
}
