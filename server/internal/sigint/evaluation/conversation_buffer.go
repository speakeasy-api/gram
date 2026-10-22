package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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

	// contentLimitSize caches the fitting suffix length after a budget overflow.
	contentLimitSize int
}

type conversationState struct {
	// Messages is chronological, with the target last and present exactly once.
	Messages []classifier.Entry `json:"messages"`

	// TargetMessageIndex identifies the sole classification subject.
	TargetMessageIndex int `json:"target_message_index"`
}

func (b *conversationBuffer) Resolve(ctx context.Context, size int) (ResolvedInput, error) {
	var empty ResolvedInput
	if size < 1 || size > b.maxSize || b.maxSize > MaxWindowSize {
		return empty, fmt.Errorf("%w: window size %d exceeds buffer capacity %d", ErrInvalidInput, size, b.maxSize)
	}
	if !b.targetLoaded {
		b.target, b.targetErr = b.resolveMessage(ctx, targetContent(b.input.stored), maxContentBytes)
		b.targetLoaded = true
	}
	if b.targetErr != nil {
		return empty, b.targetErr
	}
	if size == 1 {
		return ResolvedInput{State: b.target, MessageCount: 1, Truncated: false}, nil
	}
	if !b.historyLoaded {
		if b.input.history == nil {
			return empty, fmt.Errorf("%w: conversation history unavailable", ErrInvalidInput)
		}
		b.historyRows, b.historyErr = b.input.history.LoadHistory(ctx, b.input.message.GetOrganizationId(), uuid.MustParse(b.input.message.GetProjectId()), b.input.stored, b.maxSize-1, maxContentBytes-b.resolvedBytes)
		b.historyLoaded = true
	}
	if b.historyErr != nil {
		return empty, b.historyErr
	}
	count := min(size-1, len(b.historyRows))
	truncated := b.contentLimitSize > 0 && count+1 > b.contentLimitSize
	if truncated {
		count = b.contentLimitSize - 1
	}
	state := conversationState{Messages: []classifier.Entry{b.target}, TargetMessageIndex: 0}
	encoded, err := json.Marshal(state)
	if err != nil {
		return empty, fmt.Errorf("%w: encode target state: %w", ErrInvalidInput, err)
	}
	stateBytes := len(encoded)
	if stateBytes > maxContentBytes {
		return empty, permanent("content_too_large")
	}
	for i := range count {
		// Reserve the comma between messages. Target indexes remain one digit
		// because windows contain at most ten messages.
		entry, err := b.resolveMessage(ctx, b.historyRows[i], maxContentBytes-stateBytes-1)
		if err != nil {
			if terminal, ok := errors.AsType[*permanentError](err); ok && terminal.reason == "content_too_large" {
				truncated = true
				b.contentLimitSize = len(state.Messages)
				break
			}
			return empty, err
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return empty, permanent("invalid_content")
		}
		stateBytes += len(data) + 1
		state.Messages = append(state.Messages, entry)
	}
	slices.Reverse(state.Messages)
	state.TargetMessageIndex = len(state.Messages) - 1
	entry, err := classifier.NewEntry(state)
	if err != nil {
		return empty, fmt.Errorf("%w: encode conversation state: %w", ErrInvalidInput, err)
	}
	return ResolvedInput{State: entry, MessageCount: len(state.Messages), Truncated: truncated}, nil
}

func (b *conversationBuffer) resolveMessage(ctx context.Context, row repo.LoadEvaluationHistoryRow, budget int) (classifier.Entry, error) {
	if entry, ok := b.resolved[row.ID]; ok {
		return entry, nil
	}
	entry, err := inputWithBudget(ctx, b.input.blobs, b.input.message.GetProjectId(), row, budget)
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
