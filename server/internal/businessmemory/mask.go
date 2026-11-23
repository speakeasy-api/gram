package businessmemory

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
	"github.com/speakeasy-api/gram/server/internal/skills/efficacy"
)

// maskTranscript runs every free-text field of a judge transcript through the
// shared team memory masker, so extracted facts are built from the same masked
// view that chat summaries, memos and search use. The input is left untouched:
// the publisher caches transcripts across the judges of one chat.
func maskTranscript(ctx context.Context, redactor *chatredact.Redactor, transcript efficacy.Transcript) (efficacy.Transcript, error) {
	mask := func(text string) (string, error) {
		if text == "" {
			return text, nil
		}
		masked, _, err := redactor.Text(ctx, text)
		if err != nil {
			return "", fmt.Errorf("mask transcript text: %w", err)
		}
		return masked, nil
	}

	out := efficacy.Transcript{Omitted: transcript.Omitted, Messages: make([]efficacy.TranscriptMessage, len(transcript.Messages))}
	for i, message := range transcript.Messages {
		var err error
		if message.Content, err = mask(message.Content); err != nil {
			return efficacy.Transcript{}, err
		}
		if message.ToolOutcomeNotes, err = mask(message.ToolOutcomeNotes); err != nil {
			return efficacy.Transcript{}, err
		}
		if len(message.ToolCalls) > 0 {
			calls := make([]efficacy.TranscriptToolCall, len(message.ToolCalls))
			for k, call := range message.ToolCalls {
				// Tool names can carry a human-written title, not just an
				// identifier, so they go through the masker too.
				if call.Name, err = mask(call.Name); err != nil {
					return efficacy.Transcript{}, err
				}
				if call.Arguments, err = mask(call.Arguments); err != nil {
					return efficacy.Transcript{}, err
				}
				calls[k] = call
			}
			message.ToolCalls = calls
		}
		out.Messages[i] = message
	}
	return out, nil
}
