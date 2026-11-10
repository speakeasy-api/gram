package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

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
// submitted to its admission refusal, or returns nil for any other code. The
// client reports its own pre-submission failures with other codes, so
// admission_refused here is always the runner's.
func runnerRefusal(code string) error {
	switch code {
	case "capacity_exceeded":
		return codemode.ErrCapacity
	case "runtime_unavailable":
		return codemode.ErrRuntimeUnavailable
	case "admission_refused":
		return errors.New("runner refused admission")
	default:
		return nil
	}
}

// marshalCodeAdmissionRefusal returns the refusal as both text and
// structured content, so clients can branch on the code and retry hint.
func marshalCodeAdmissionRefusal(ctx context.Context, logger *slog.Logger, id mcpjsonrpc.ID, refusal codeAdmissionRefusal, timing codeExecutionTiming) (json.RawMessage, error) {
	structured, err := json.Marshal(refusal)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal").LogError(ctx, logger)
	}
	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{Type: "text", MimeType: nil, Text: string(structured), Data: nil, Meta: nil})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal content").LogError(ctx, logger)
	}
	body, err := json.Marshal(&result[codeToolCallResult]{ID: id, Result: codeToolCallResult{toolCallResult: toolCallResult{Content: []json.RawMessage{chunk}, StructuredContent: structured, IsError: true}, Meta: map[string]codeExecutionTiming{codeTimingMetaKey: timing}}, serverIdentity: serverInfoMetaServer, cacheHints: nil})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize code refusal response").LogError(ctx, logger)
	}
	return body, nil
}

// codeTimingMetaKey namespaces the gateway's timing in an execute result's
// _meta, beside the server identity.
const codeTimingMetaKey = "com.speakeasy.ai/timing"

// codeExecutionTiming reports where the gateway spent an execute request, so
// a client can tell its own network time from server time.
type codeExecutionTiming struct {
	// ServerMS is the gateway's total time for the request, in milliseconds.
	ServerMS int64 `json:"server_ms"`

	// AdmissionMS is time spent waiting for capacity before submission; absent
	// when the request was refused before reaching the runtime.
	AdmissionMS *int64 `json:"admission_ms,omitempty"`

	// RunMS is time from submission to the result, including tool callbacks;
	// absent when the program was never submitted.
	RunMS *int64 `json:"run_ms,omitempty"`
}

// newCodeExecutionTiming measures the request from started; execution is nil
// when the coordinator refused it.
func newCodeExecutionTiming(started time.Time, execution *codemode.ExecutionResult) codeExecutionTiming {
	timing := codeExecutionTiming{ServerMS: time.Since(started).Milliseconds(), AdmissionMS: nil, RunMS: nil}
	if execution != nil {
		timing.AdmissionMS = new(execution.Timing.Admission.Milliseconds())
		timing.RunMS = new(execution.Timing.Run.Milliseconds())
	}
	return timing
}

// codeToolCallResult is an execute result with the gateway's timing in _meta.
type codeToolCallResult struct {
	toolCallResult

	// Meta carries the timing under codeTimingMetaKey.
	Meta map[string]codeExecutionTiming `json:"_meta"`
}
