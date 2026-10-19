package riskfindings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

func TestClickHouseStateWins(t *testing.T) {
	t.Parallel()

	markedAt := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		eventKind  string
		insertedAt time.Time
		want       bool
	}{
		{
			name:       "explicit reversal always wins",
			eventKind:  chrepo.EventKindUnsuppression,
			insertedAt: markedAt.Add(-time.Hour),
			want:       true,
		},
		{
			name:       "newer suppression wins",
			eventKind:  chrepo.EventKindSuppression,
			insertedAt: markedAt.Add(time.Nanosecond),
			want:       true,
		},
		{
			name:       "older suppression does not win",
			eventKind:  chrepo.EventKindSuppression,
			insertedAt: markedAt,
			want:       false,
		},
		{
			name:       "finding copy is not state",
			eventKind:  chrepo.EventKindFinding,
			insertedAt: markedAt.Add(time.Hour),
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			current := &latestFinding{insertedAt: tt.insertedAt, eventKind: tt.eventKind}
			require.Equal(t, tt.want, clickHouseStateWins(current, markedAt))
		})
	}
}
