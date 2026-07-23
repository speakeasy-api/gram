package timewindowpoller

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPollCheckpointRoundTripPartial(t *testing.T) {
	t.Parallel()

	watermark := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	windowStart := watermark.Add(time.Millisecond)
	windowEnd := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	checkpoint := PartialCheckpoint(watermark, windowStart, windowEnd, "next-page")

	encoded, err := checkpoint.MarshalText()
	require.NoError(t, err)

	var decoded PollCheckpoint
	require.NoError(t, decoded.UnmarshalText(encoded))
	require.Equal(t, checkpoint, decoded)
}

func TestPollCheckpointDecodesLegacyWatermark(t *testing.T) {
	t.Parallel()

	watermark := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)

	decoded, err := DecodeCheckpoint("", watermark)
	require.NoError(t, err)
	require.Equal(t, CompletedCheckpoint(watermark), decoded)
}
