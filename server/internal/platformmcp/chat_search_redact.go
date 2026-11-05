package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/chatredact"
)

// messageText joins a message's redacted content, tool calls and stored
// payload. changed reports that something was masked or withheld.
func (s *ChatSearchService) messageText(ctx context.Context, content string, toolCalls, raw []byte) (string, bool, error) {
	text, changed, err := s.redactor.Message(ctx, content, toolCalls, raw)
	return text, changed, redactErr(err)
}

// redactText masks detected secrets in plain text or JSON.
func (s *ChatSearchService) redactText(ctx context.Context, text string) (string, bool, error) {
	safe, changed, err := s.redactor.Text(ctx, text)
	return safe, changed, redactErr(err)
}

// redactErr maps a failed scan to ErrUnavailable so the tool fails closed with
// the same refusal as before redaction moved to its own package.
func redactErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, chatredact.ErrScanFailed):
		return ErrUnavailable
	default:
		return fmt.Errorf("redact chat text: %w", err)
	}
}
