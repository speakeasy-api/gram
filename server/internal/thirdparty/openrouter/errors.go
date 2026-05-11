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

func classifyHTTPError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if status == http.StatusPaymentRequired {
		return fmt.Errorf("OpenRouter API error (status %d): %s: %w", status, msg, ErrInsufficientCredits)
	}
	return fmt.Errorf("OpenRouter API error (status %d): %s", status, msg)
}
