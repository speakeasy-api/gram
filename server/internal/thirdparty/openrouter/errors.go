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
		// Other 4xx (401 auth, 403 moderation, 404 model, 408 timeout, 429
		// rate limit) aren't fixable by trimming and are excluded here.
		return fmt.Errorf("OpenRouter API error (status %d): %s: %w", status, msg, ErrHistoryCorruptionCandidate)
	default:
		return fmt.Errorf("OpenRouter API error (status %d): %s", status, msg)
	}
}
