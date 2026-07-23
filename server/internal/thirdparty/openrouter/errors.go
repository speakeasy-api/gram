package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
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

// ErrBadRequest signals OpenRouter rejected the request with a 400/422 that is
// not transcript-shaped: an invalid parameter, an unknown model, malformed
// JSON. Re-sending the same payload produces the same answer, so callers treat
// it as deterministic rather than retrying it.
var ErrBadRequest = errors.New("openrouter: bad request")

// IsBadRequest reports whether err originated from an OpenRouter 400/422
// response anywhere in the error chain. History-corruption candidates are
// classified separately and do not satisfy this predicate.
func IsBadRequest(err error) bool {
	return errors.Is(err, ErrBadRequest)
}

// ErrContentPolicy signals the provider refused the request on content grounds:
// a moderation, safety or content-filter rejection rather than a credential or
// entitlement problem. The verdict is a property of the payload, so re-sending
// it produces the same refusal and callers treat it as deterministic.
var ErrContentPolicy = errors.New("openrouter: content policy rejection")

// IsContentPolicy reports whether err originated from a provider content-policy
// refusal anywhere in the error chain. Auth and configuration 403s carry no
// such marker and do not satisfy this predicate.
func IsContentPolicy(err error) bool {
	return errors.Is(err, ErrContentPolicy)
}

// maxDiagnosticBodyBytes bounds a response body recorded as a span attribute.
const maxDiagnosticBodyBytes = 2048

// diagnosticBody bounds a response body for span diagnostics. The cut is
// trimmed back to whole runes so an exporter never receives invalid UTF-8.
func diagnosticBody(body []byte) string {
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > maxDiagnosticBodyBytes {
		snippet = strings.ToValidUTF8(snippet[:maxDiagnosticBodyBytes], "")
	}
	return snippet
}

// classifyHTTPError turns a non-2xx OpenRouter response into an error carrying
// the status and a classification sentinel. The body is inspected for
// classification and then dropped from the error: an error response echoes back
// the payload that provoked it, so it can carry transcript fragments, tool
// arguments, system prompts and anything else the caller sent, and the error
// reaches user-facing surfaces, logs and persisted columns. The status and the
// classification are all a caller needs there.
//
// A bounded body still goes on the active span, which is where a misclassified
// status is diagnosed: spans are the one sink already trusted with request
// payloads (the empty-choices path records the same attribute), and without it
// widening the marker lists means guessing at what upstream actually sent.
func classifyHTTPError(ctx context.Context, status int, body []byte) error {
	trace.SpanFromContext(ctx).SetAttributes(
		attr.HTTPResponseStatusCode(status),
		attr.OpenRouterResponseBody(diagnosticBody(body)),
	)

	switch status {
	case http.StatusPaymentRequired:
		return fmt.Errorf("OpenRouter API error (status %d), response body omitted: %w", status, ErrInsufficientCredits)
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		// 400/422 is the strongest "request body is the problem" signal, but
		// it also covers non-history bad params (invalid model, malformed
		// JSON, etc). Self-heal trims the live transcript and persists it,
		// so misclassifying a one-off param error as corruption would lose
		// real history. Require BOTH the status code and a corruption-shaped
		// body before opting into self-heal.
		if looksLikeHistoryCorruption(strings.TrimSpace(string(body))) {
			return fmt.Errorf("OpenRouter API error (status %d), response body omitted: %w", status, ErrHistoryCorruptionCandidate)
		}
		return fmt.Errorf("OpenRouter API error (status %d), response body omitted: %w", status, ErrBadRequest)
	case http.StatusForbidden:
		// 403 is overloaded: a missing or unentitled key, an org policy block,
		// and a provider moderation refusal all land here. Only the last one is
		// deterministic; the others clear once the key or entitlement is fixed,
		// so require a content-shaped body before spending the caller's attempt
		// budget on it.
		if looksLikeContentPolicy(strings.TrimSpace(string(body))) {
			return fmt.Errorf("OpenRouter API error (status %d), response body omitted: %w", status, ErrContentPolicy)
		}
		return fmt.Errorf("OpenRouter API error (status %d), response body omitted", status)
	default:
		return fmt.Errorf("OpenRouter API error (status %d), response body omitted", status)
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

// contentPolicyBodyMarkers are fragments providers emit when they refuse a
// payload on content grounds. Every fragment is a phrase or a field name a
// provider emits, never a bare word: a 403 body can echo the request back, so a
// bare "safety", "moderation" or "flagged" matches an auth denial whose echoed
// transcript merely discusses those topics — and misclassifying a credential
// failure as a content refusal makes it deterministic, spending the caller's
// whole attempt budget on a request that a fixed key would have served.
var contentPolicyBodyMarkers = []string{
	// OpenRouter's own moderation envelope: a 403 carries
	// error.metadata.reasons and error.metadata.flagged_input.
	"flagged_input",
	"requires moderation",
	// OpenAI-shaped
	"content_policy",
	"content policy",
	"content_filter",
	"content filter",
	// Google/Vertex-shaped
	"safety settings",
	"safety_settings",
}

func looksLikeContentPolicy(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range contentPolicyBodyMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
