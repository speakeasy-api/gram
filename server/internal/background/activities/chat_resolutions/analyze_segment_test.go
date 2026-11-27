package resolution_activities

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// AnalyzeSegment loads the transcript key only when it will write an outcome,
// so a segment whose tool calls map to no message never touches the key.
func TestWritesAnOutcome(t *testing.T) {
	t.Parallel()

	call := func(index int) toolCallAnalysis {
		return toolCallAnalysis{MessageIndex: index, Outcome: "success", Notes: "note"}
	}
	cases := []struct {
		name     string
		calls    []toolCallAnalysis
		start    int
		messages int
		want     bool
	}{
		{name: "no tool calls", calls: nil, start: 0, messages: 3, want: false},
		{name: "every call out of range", calls: []toolCallAnalysis{call(5), call(-4)}, start: 1, messages: 3, want: false},
		{name: "one call in range", calls: []toolCallAnalysis{call(5), call(1)}, start: 1, messages: 3, want: true},
		{name: "last message", calls: []toolCallAnalysis{call(2)}, start: 0, messages: 3, want: true},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, writesAnOutcome(tc.calls, tc.start, tc.messages), tc.name)
	}
}
