package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

func TestTranscriptLineage(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		previous  []string
		incoming  []string
		compacted bool
		kinds     []string
	}{
		{name: "edited prefix with retained tail and new turn", previous: []string{"old", "reply", "continue", "done"}, incoming: []string{"edited", "reply", "continue", "done", "continue"}, kinds: []string{CaptureUnknown, CaptureReobserved, CaptureReobserved, CaptureReobserved, CaptureOriginal}},
		{name: "known compaction summary and retained tail", previous: []string{"old", "reply", "continue", "done"}, incoming: []string{"summary", "continue", "done"}, compacted: true, kinds: []string{CaptureSynthetic, CaptureReobserved, CaptureReobserved}},
		{name: "isolated repetition is not a copy", previous: []string{"old", "continue"}, incoming: []string{"edited", "continue"}, kinds: []string{CaptureUnknown, CaptureUnknown}},
		{name: "unaligned rewrite stays unknown", previous: []string{"old", "reply"}, incoming: []string{"summary", "new"}, kinds: []string{CaptureUnknown, CaptureUnknown}},
		{name: "repeated tail is ambiguous", previous: []string{"old", "a", "b"}, incoming: []string{"summary", "a", "b", "a", "b"}, kinds: []string{CaptureUnknown, CaptureUnknown, CaptureUnknown, CaptureUnknown, CaptureUnknown}},
		{name: "positionally identical prefix", previous: []string{"a", "b", "old"}, incoming: []string{"a", "b", "edited"}, kinds: []string{CaptureReobserved, CaptureReobserved, CaptureUnknown}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			previous := make([]repo.ListChatMessagesForMatchRow, len(tc.previous))
			for i, text := range tc.previous {
				previous[i] = repo.ListChatMessagesForMatchRow{ID: uuid.New(), Role: "user", Content: text, CaptureKind: CaptureOriginal}
			}
			incoming := make([]messageSlot, len(tc.incoming))
			for i, text := range tc.incoming {
				incoming[i] = slotFromStored("user", text, "", nil)
			}
			got := transcriptLineage(previous, incoming, tc.compacted)
			for i, lineage := range got {
				kind := CaptureOriginal
				if lineage != nil {
					kind = lineage.Kind
				}
				require.Equal(t, tc.kinds[i], kind, "position %d", i)
				if kind == CaptureReobserved {
					require.NotEqual(t, uuid.Nil, lineage.OriginalMessageID)
				}
			}
		})
	}
}
