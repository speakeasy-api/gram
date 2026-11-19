package repo

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatLogTimeBoundsNarrow(t *testing.T) {
	t.Parallel()

	complete := chatLogTimeBoundsCompleteFromUnixNano
	tests := []struct {
		name      string
		bounds    chatLogTimeBounds
		start     int64
		end       int64
		wantStart int64
		wantEnd   int64
	}{
		{
			name:      "it keeps the window when the chat has no bounds",
			bounds:    chatLogTimeBounds{first: 0, last: 0, found: false},
			start:     0,
			end:       complete + 1000,
			wantStart: 0,
			wantEnd:   complete + 1000,
		},
		{
			name:      "it narrows both ends for a chat that started after the cutoff",
			bounds:    chatLogTimeBounds{first: complete + 100, last: complete + 200, found: true},
			start:     0,
			end:       complete + 1000,
			wantStart: complete + 100,
			wantEnd:   complete + 200,
		},
		{
			name:      "it keeps the lower bound for a chat that started before the cutoff",
			bounds:    chatLogTimeBoundsBefore(complete),
			start:     0,
			end:       complete + 1000,
			wantStart: 0,
			wantEnd:   complete + 200,
		},
		{
			name:      "it never widens the requested window",
			bounds:    chatLogTimeBounds{first: complete + 100, last: complete + 900, found: true},
			start:     complete + 300,
			end:       complete + 400,
			wantStart: complete + 300,
			wantEnd:   complete + 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotStart, gotEnd := tt.bounds.narrow(tt.start, tt.end)
			require.Equal(t, tt.wantStart, gotStart)
			require.Equal(t, tt.wantEnd, gotEnd)
		})
	}
}

func chatLogTimeBoundsBefore(complete int64) chatLogTimeBounds {
	return chatLogTimeBounds{first: complete - 100, last: complete + 200, found: true}
}
