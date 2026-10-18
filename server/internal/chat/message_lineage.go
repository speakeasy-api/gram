package chat

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat/repo"
)

const (
	// CaptureOriginal is a newly observed utterance, including edited content.
	CaptureOriginal = "original"
	// CaptureReobserved is an exact copy from the preceding transcript generation.
	CaptureReobserved = "reobserved"
	// CaptureSynthetic is generated history, such as a compaction summary.
	CaptureSynthetic = "synthetic"
	// CaptureUnknown has insufficient lineage to count as an original utterance.
	CaptureUnknown = "unknown"
)

// MessageLineage preserves why a transcript row was persisted.
type MessageLineage struct {
	// Kind distinguishes new observations from copied or generated history.
	Kind string

	// OriginalMessageID points to the original row when the copy is identifiable.
	OriginalMessageID uuid.UUID
}

// MarkCompactedLineage identifies retained history in a producer-declared
// compaction. Unmatched text is synthetic, never newly observed human speech.
func MarkCompactedLineage(ctx context.Context, db repo.DBTX, projectID, chatID uuid.UUID, generation int32, writes []MessageWrite) error {
	previous, err := repo.New(db).ListChatMessagesForMatch(ctx, repo.ListChatMessagesForMatchParams{ProjectID: projectID, ChatID: chatID, Generation: generation})
	if err != nil {
		return fmt.Errorf("load compaction lineage: %w", err)
	}
	slots := make([]messageSlot, len(writes))
	for i, write := range writes {
		p := write.Params
		slots[i] = slotFromStored(p.Role, p.Content, p.ToolCallID.String, p.ToolCalls)
	}
	lineages := transcriptLineage(previous, slots, true)
	for i := range writes {
		writes[i].Lineage = lineages[i]
	}
	return nil
}

// transcriptLineage matches positional prefixes and a retained contiguous tail
// anchored at the previous transcript's end. An isolated repeated phrase is not
// sufficient to deduplicate a new user turn. Matches never cross conversations.
func transcriptLineage(previous []repo.ListChatMessagesForMatchRow, incoming []messageSlot, compacted bool) []*MessageLineage {
	stored := make([]repo.ListChatMessagesForMatchRow, 0, len(previous))
	for _, row := range previous {
		if !slotFromStored(row.Role, row.Content, row.ToolCallID.String, row.ToolCalls).isBlankAssistant() {
			stored = append(stored, row)
		}
	}
	out := make([]*MessageLineage, len(incoming))
	for i := range out {
		out[i] = &MessageLineage{Kind: CaptureUnknown, OriginalMessageID: uuid.Nil}
		if compacted {
			out[i] = &MessageLineage{Kind: CaptureSynthetic, OriginalMessageID: uuid.Nil}
		}
	}
	copyLineage := func(i, j int) {
		row := stored[j]
		kind := CaptureReobserved
		if row.CaptureKind == CaptureSynthetic || row.CaptureKind == CaptureUnknown {
			kind = row.CaptureKind
		}
		original := row.ID
		if row.OriginalMessageID.Valid {
			original = row.OriginalMessageID.UUID
		}
		out[i] = &MessageLineage{Kind: kind, OriginalMessageID: original}
	}
	same := func(i, j int) bool {
		row := stored[j]
		return incoming[i] == slotFromStored(row.Role, row.Content, row.ToolCallID.String, row.ToolCalls)
	}
	i, j := 0, 0
	for i < len(incoming) && j < len(stored) {
		if incoming[i].isBlankAssistant() {
			i++
			continue
		}
		if !same(i, j) {
			break
		}
		copyLineage(i, j)
		i++
		j++
	}
	bestEnd, bestLength, tied := 0, 0, false
	for end := i; end < len(incoming); end++ {
		length := 0
		for end-length >= i && len(stored)-1-length >= j && same(end-length, len(stored)-1-length) {
			length++
		}
		if length > bestLength {
			bestEnd, bestLength, tied = end, length, false
		} else if length > 0 && length == bestLength {
			tied = true
		}
	}
	if !tied && (bestLength >= 2 || compacted && bestLength == 1) {
		for offset := range bestLength {
			copyLineage(bestEnd-offset, len(stored)-1-offset)
		}
		if !compacted {
			for i := bestEnd + 1; i < len(out); i++ {
				out[i] = nil
			}
		}
	}
	return out
}
