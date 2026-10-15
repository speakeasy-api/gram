package evaluation

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// conversationBuffer bounds history to the largest runnable sensor window and
// resolves content lazily, so failures in older messages affect only larger slices.
type conversationBuffer struct {
	input         *conversationInput
	maxSize       int
	targetLoaded  bool
	target        classifier.Entry
	targetErr     error
	historyLoaded bool
	historyRows   []repo.LoadEvaluationHistoryRow
	historyErr    error
	resolved      map[uuid.UUID]classifier.Entry
	resolvedBytes int
}

type conversationState struct {
	// Messages is chronological, with the target last and present exactly once.
	Messages []classifier.Entry `json:"messages"`

	// TargetMessageIndex identifies the sole classification subject.
	TargetMessageIndex int `json:"target_message_index"`
}

func (b *conversationBuffer) Resolve(ctx context.Context, size int) (classifier.Entry, error) {
	var empty classifier.Entry
	if size < 1 || size > b.maxSize || b.maxSize > MaxWindowSize {
		return empty, fmt.Errorf("%w: window size %d exceeds buffer capacity %d", ErrInvalidInput, size, b.maxSize)
	}
	if !b.targetLoaded {
		b.target, b.targetErr = b.resolveMessage(ctx, targetContent(b.input.stored))
		b.targetLoaded = true
	}
	if b.targetErr != nil {
		return empty, b.targetErr
	}
	if size == 1 {
		return b.target, nil
	}
	if !b.historyLoaded {
		if b.input.history == nil {
			return empty, fmt.Errorf("%w: conversation history unavailable", ErrInvalidInput)
		}
		b.historyRows, b.historyErr = b.input.history.LoadHistory(ctx, b.input.message.GetOrganizationId(), uuid.MustParse(b.input.message.GetProjectId()), b.input.stored, b.maxSize-1)
		b.historyLoaded = true
	}
	if b.historyErr != nil {
		return empty, b.historyErr
	}
	count := min(size-1, len(b.historyRows))
	state := conversationState{Messages: make([]classifier.Entry, count+1), TargetMessageIndex: count}
	state.Messages[count] = b.target
	for i := range count {
		entry, err := b.resolveMessage(ctx, b.historyRows[i])
		if err != nil {
			return empty, err
		}
		state.Messages[count-1-i] = entry
	}
	entry, err := classifier.NewEntry(state)
	if err != nil {
		return empty, fmt.Errorf("%w: encode conversation state: %w", ErrInvalidInput, err)
	}
	return entry, nil
}

func (b *conversationBuffer) resolveMessage(ctx context.Context, row repo.LoadEvaluationHistoryRow) (classifier.Entry, error) {
	if entry, ok := b.resolved[row.ID]; ok {
		return entry, nil
	}
	entry, err := inputWithBudget(ctx, b.input.blobs, b.input.message.GetProjectId(), row, maxContentBytes-b.resolvedBytes)
	if err != nil {
		return entry, err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return classifier.Entry{}, permanent("invalid_content")
	}
	if b.resolved == nil {
		b.resolved = make(map[uuid.UUID]classifier.Entry)
	}
	b.resolved[row.ID] = entry
	b.resolvedBytes += len(data)
	return entry, nil
}
