package openrouter

import (
	"context"
	"errors"
	"net"
	"net/http"
)

// FailureReason is a bounded classification of why an OpenRouter call did not
// return a usable answer. It exists so telemetry can tell the failures apart
// that an operator responds to differently: a drained credit balance needs a
// top-up, a 429 needs backoff or a quota increase, and a 5xx needs patience.
// Collapsing them into a single "error" bucket is what let prompt-injection
// scanning sit dark on an empty balance without anything paging.
//
// Values are stable metric dimensions. Add to them rather than renaming.
type FailureReason string

const (
	// ReasonNone is the classification of a nil error.
	ReasonNone FailureReason = "none"
	// ReasonInsufficientCredits is an OpenRouter 402: the key's balance
	// cannot fund the request. Every call on that key fails until somebody
	// tops it up, so it never self-heals.
	ReasonInsufficientCredits FailureReason = "insufficient_credits"
	// ReasonRateLimited is an OpenRouter 429, whether the key's own ceiling
	// or upstream provider capacity.
	ReasonRateLimited FailureReason = "rate_limited"
	// ReasonUnauthorized is a 401 or a non-content 403: a missing, revoked or
	// unentitled key. Like insufficient credits it does not self-heal.
	ReasonUnauthorized FailureReason = "unauthorized"
	// ReasonKeyDisabled is a locked-down Gram-provisioned platform key. The
	// call never reaches OpenRouter.
	ReasonKeyDisabled FailureReason = "key_disabled"
	// ReasonUpstreamUnavailable is a 5xx, an edge timeout (524) or a provider
	// overload (529): the model is reachable in principle but not right now.
	ReasonUpstreamUnavailable FailureReason = "upstream_unavailable"
	// ReasonContentPolicy is a provider moderation refusal. It is a property
	// of the payload, not of Gram's ability to run analysis.
	ReasonContentPolicy FailureReason = "content_policy"
	// ReasonBadRequest is a deterministic 400/422: bad parameters, an unknown
	// model, a malformed or oversize transcript. It usually means Gram sent
	// something wrong, so it is an engineering signal rather than an
	// operational one.
	ReasonBadRequest FailureReason = "bad_request"
	// ReasonTimeout is a deadline or network timeout on Gram's side.
	ReasonTimeout FailureReason = "timeout"
	// ReasonCanceled is the caller abandoning the call. It is not a fault.
	ReasonCanceled FailureReason = "canceled"
	// ReasonError is everything unclassified: transport failures, decode
	// errors, and statuses no branch above claims.
	ReasonError FailureReason = "error"
)

// IsRateLimited reports whether err originated from an OpenRouter 429 anywhere
// in the error chain.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

// IsUpstreamUnavailable reports whether err carries an OpenRouter response
// status that says the provider could not serve the request right now: any
// 5xx, including the 524 edge timeout and the 529 provider-overloaded status
// the SDK surfaces.
func IsUpstreamUnavailable(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode >= http.StatusInternalServerError
}

// Classify reduces err to a bounded FailureReason suitable for use as a metric
// dimension.
//
// Cancellation and timeout are checked before the HTTP classification because
// a caller that walks away mid-flight is not an OpenRouter fault, and reading
// it as one would make an ordinary deploy look like an outage.
func Classify(err error) FailureReason {
	if err == nil {
		return ReasonNone
	}
	if errors.Is(err, context.Canceled) {
		return ReasonCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return ReasonTimeout
	}
	switch {
	case IsInsufficientCredits(err):
		return ReasonInsufficientCredits
	case IsRateLimited(err):
		return ReasonRateLimited
	case IsPlatformKeyDisabled(err):
		return ReasonKeyDisabled
	case IsContentPolicy(err):
		return ReasonContentPolicy
	case IsBadRequest(err), IsHistoryCorruptionCandidate(err):
		return ReasonBadRequest
	case IsUpstreamUnavailable(err):
		return ReasonUpstreamUnavailable
	}
	if httpErr, ok := errors.AsType[*HTTPError](err); ok {
		switch httpErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return ReasonUnauthorized
		case http.StatusRequestTimeout:
			return ReasonTimeout
		}
	}
	return ReasonError
}

// Degraded reports whether a reason means Gram could not get an answer out of
// the model. Content-policy refusals are answers — the provider looked and
// declined — so they are excluded, as are the non-faults.
func (r FailureReason) Degraded() bool {
	switch r {
	case ReasonNone, ReasonCanceled, ReasonContentPolicy:
		return false
	default:
		return true
	}
}
