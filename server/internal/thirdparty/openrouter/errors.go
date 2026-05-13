package openrouter

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrInsufficientCredits is returned when OpenRouter rejects a request with
// status 402: the org has exhausted its credit balance or requested more
// tokens than the remaining balance can fund.
var ErrInsufficientCredits = errors.New("openrouter: insufficient credits")

// IsInsufficientCredits reports whether err originated from an OpenRouter 402
// response anywhere in the error chain.
func IsInsufficientCredits(err error) bool {
	return errors.Is(err, ErrInsufficientCredits)
}

// ErrHistoryCorruptionCandidate signals OpenRouter (or the upstream provider
// it proxies for) rejected the request with a 4xx consistent with a
// malformed or oversize transcript. Callers self-heal by trimming history
// and retrying.
var ErrHistoryCorruptionCandidate = errors.New("openrouter: history corruption candidate")

// IsHistoryCorruptionCandidate reports whether err originated from an
// OpenRouter 400/422 response anywhere in the error chain.
func IsHistoryCorruptionCandidate(err error) bool {
	return errors.Is(err, ErrHistoryCorruptionCandidate)
}

func classifyHTTPError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	switch status {
	case http.StatusPaymentRequired:
		return fmt.Errorf("OpenRouter API error (status %d): %s: %w", status, msg, ErrInsufficientCredits)
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		// 400/422 is the strongest "request body is the problem" signal, but
		// it also covers non-history bad params (invalid model, malformed
		// JSON, etc). Self-heal trims the live transcript and persists it,
		// so misclassifying a one-off param error as corruption would lose
		// real history. Require BOTH the status code and a corruption-shaped
		// body before opting into self-heal.
		if looksLikeHistoryCorruption(msg) {
			return fmt.Errorf("OpenRouter API error (status %d): %s: %w", status, msg, ErrHistoryCorruptionCandidate)
		}
		return fmt.Errorf("OpenRouter API error (status %d): %s", status, msg)
	default:
		return fmt.Errorf("OpenRouter API error (status %d): %s", status, msg)
	}
}

// historyCorruptionBodyMarkers are fragments inference providers emit when
// they refuse a transcript for shape reasons (tool pairing, role
// alternation, oversize context). Kept narrow; widen only as new patterns
// are observed in production. Body comes from the upstream verbatim so
// matching here — at the OpenRouter boundary — is the only place the
// fragments aren't already mangled by intermediate wrappers.
var historyCorruptionBodyMarkers = []string{
	// Anthropic
	"tool_use",
	"tool_result",
	// Anthropic + others
	"must alternate",
	// OpenAI
	"tool_calls",
	// Context overflow
	"maximum context length",
	"context_length_exceeded",
	"prompt is too long",
}

func looksLikeHistoryCorruption(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range historyCorruptionBodyMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
