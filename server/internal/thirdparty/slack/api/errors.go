package api

import (
	"fmt"
	"net/http"
)

// ErrCodeAlreadyReacted is the envelope error reactions.add answers with when
// the emoji the caller asked for is already on the message. The requested end
// state already holds, so callers generally treat it as success rather than as
// a failed call.
const ErrCodeAlreadyReacted = "already_reacted"

// Error is returned when a Slack Web API call completed but Slack refused it:
// either the HTTP response was not 200, or the response envelope reported
// ok=false. It carries the machine-readable envelope code so callers can branch
// on specific outcomes and so error boundaries can tell an expected caller
// mistake apart from a Slack-side failure.
type Error struct {
	// Method is the Slack Web API method that was called, e.g. "reactions.add".
	Method string

	// Code is the envelope's `error` value, e.g. "thread_not_found". It is empty
	// when Slack answered with a non-200 status instead of an envelope, or when
	// the envelope reported ok=false without naming an error.
	Code string

	// StatusCode is the HTTP status Slack answered with. Envelope failures carry
	// 200, since Slack reports most refusals in the body of a 200 response.
	StatusCode int

	// message is the rendered error text, fixed at construction so the two
	// refusal shapes (non-200 status, ok=false envelope) each keep their own
	// wording.
	message string
}

// slackFaultCodes are the envelope error codes that report a failure on Slack's
// side rather than in the request: its own internal errors and throttling.
// Everything else an envelope can report — an unknown channel, a thread ts that
// is not a parent, blocks that fail validation, a scope the token never had —
// is something the caller or the app's Slack configuration has to change.
var slackFaultCodes = map[string]struct{}{
	"internal_error":      {},
	"fatal_error":         {},
	"service_unavailable": {},
	"request_timeout":     {},
	"team_added_to_org":   {},
	"org_login_required":  {},
	// Slack spells the throttling code both ways depending on the method.
	"ratelimited":  {},
	"rate_limited": {},
}

func newStatusError(method string, statusCode int, body []byte) *Error {
	return &Error{
		Method:     method,
		Code:       "",
		StatusCode: statusCode,
		message:    fmt.Sprintf("slack %s returned %d: %s", method, statusCode, string(body)),
	}
}

func newEnvelopeError(method string, statusCode int, envelope ResponseEnvelope) *Error {
	return &Error{
		Method:     method,
		Code:       envelope.Error,
		StatusCode: statusCode,
		message:    fmt.Sprintf("slack %s: %s", method, errorDetails(envelope)),
	}
}

func (e *Error) Error() string {
	return e.message
}

// ClientFault implements oops.ClientFaulter, reporting whether this refusal is
// the caller's to fix.
//
// The Slack Web API answers HTTP 200 with `ok: false` for essentially every
// argument, permission, and state problem, reserving a small set of codes for
// its own failures, so an envelope error is a caller fault unless its code names
// a Slack-side failure. A non-200 status follows the usual HTTP split, except
// that 429 stays a Slack-side fault: a throttled call is worth retrying, not
// worth reporting back as bad input.
func (e *Error) ClientFault() bool {
	if e.StatusCode != http.StatusOK {
		return e.StatusCode >= 400 && e.StatusCode < 500 && e.StatusCode != http.StatusTooManyRequests
	}

	if e.Code == "" {
		return false
	}

	_, slackFault := slackFaultCodes[e.Code]
	return !slackFault
}
