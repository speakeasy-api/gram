package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcpjsonrpc"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// codeRefusalCode classifies why an execution never reached a runner.
type codeRefusalCode string

const (
	codeRefusalCapacity    codeRefusalCode = "capacity_exceeded"
	codeRefusalUnavailable codeRefusalCode = "runtime_unavailable"
	codeRefusalActive      codeRefusalCode = "request_active"
	codeRefusalCancelled   codeRefusalCode = "cancelled"
	codeRefusalUnexpected  codeRefusalCode = "not_admitted"
)

const (
	// codeCapacityRetryAfter is the retry hint for a full runtime: a typical
	// execution finishes well within a second.
	codeCapacityRetryAfter = 1000
	// codeUnavailableRetryAfter is the retry hint while runners cannot start,
	// for example while a deploy installs the sandbox template.
	codeUnavailableRetryAfter = 15000
)

// codeAdmissionRefusal is the structured result for an execution that was
// refused before its program reached a runner.
type codeAdmissionRefusal struct {
	// Error describes the refusal.
	Error codeAdmissionRefusalError `json:"error"`
}

type codeAdmissionRefusalError struct {
	// Code classifies the refusal.
	Code codeRefusalCode `json:"code"`

	// Message explains the refusal; it always states no program was submitted.
	Message string `json:"message"`

	// Retryable is true when the same request can succeed unchanged later.
	Retryable bool `json:"retryable"`

	// RetryAfterMS suggests how long to wait before retrying, in milliseconds.
	RetryAfterMS int `json:"retry_after_ms,omitempty"`
}

func newCodeAdmissionRefusal(err error) codeAdmissionRefusal {
	refusal := func(code codeRefusalCode, message string, retryable bool, retryAfter int) codeAdmissionRefusal {
		return codeAdmissionRefusal{Error: codeAdmissionRefusalError{Code: code, Message: message + " No program was submitted.", Retryable: retryable, RetryAfterMS: retryAfter}}
	}
	switch {
	case errors.Is(err, codemode.ErrCapacity):
		return refusal(codeRefusalCapacity, "Code Mode is busy.", true, codeCapacityRetryAfter)
	case errors.Is(err, codemode.ErrRuntimeUnavailable):
		return refusal(codeRefusalUnavailable, "The code runtime is temporarily unavailable.", true, codeUnavailableRetryAfter)
	case errors.Is(err, codemode.ErrAlreadyActive):
		return refusal(codeRefusalActive, "A request with this ID is already running; use a new request ID.", false, 0)
	case errors.Is(err, codemode.ErrCancelledBeforeSubmit), errors.Is(err, context.Canceled):
		return refusal(codeRefusalCancelled, "The execution was cancelled.", false, 0)
	default:
		return refusal(codeRefusalUnexpected, "Code execution was not admitted.", true, codeCapacityRetryAfter)
	}
}

// runnerRefusal maps a runner result code that proves the program was never
// submitted to its admission refusal.
func runnerRefusal(code string) (error, bool) {
	switch code {
	case "capacity_exceeded":
		return codemode.ErrCapacity, true
	case "runtime_unavailable":
		return codemode.ErrRuntimeUnavailable, true
	case "admission_refused":
		return errors.New("runner refused admission"), true
	default:
		return nil, false
	}
}

// marshalCodeAdmissionRefusal returns the refusal as both text and
// structured content, so clients can branch on the code and retry hint.
func marshalCodeAdmissionRefusal(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, refusal codeAdmissionRefusal) (json.RawMessage, error) {
	structured, err := json.Marshal(refusal)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal").LogError(ctx, logger)
	}
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{Type: "text", MimeType: nil, Text: string(structured), Data: nil, Meta: nil})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal content").LogError(ctx, logger)
	}
	body, err := json.Marshal(&result[toolCallResult]{ID: id, Result: toolCallResult{Content: []json.RawMessage{chunk}, StructuredContent: structured, IsError: true}, serverIdentity: serverInfoMetaServer, cacheHints: nil})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal response").LogError(ctx, logger)
	}
	return body, nil
}
