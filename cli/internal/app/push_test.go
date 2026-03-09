package app

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
