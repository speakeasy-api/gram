package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxSourceBytes bounds the submitted Python program to 64 KiB.
	MaxSourceBytes = 64 << 10
	// MaxWallTime includes admission, discovery, tools and interpreter work.
	MaxWallTime = 30 * time.Second
	// maxCallbacks bounds attempted helpers, including invalid requests.
	maxCallbacks = 64
	// maxConcurrentCallbacks bounds upstream fanout from one program.
	maxConcurrentCallbacks = 8
	// maxCumulativeBytes bounds total callback replies per execution to 8 MiB.
	maxCumulativeBytes = 8 << 20
	// maxValueBytes bounds the final model-visible JSON value to 256 KiB.
	maxValueBytes = 256 << 10
	// maxPrintBytes bounds captured Python output to 16 KiB.
	maxPrintBytes = 16 << 10
	// callbackCleanupGrace allows cancelled dispatchers to report their final known outcomes.
	callbackCleanupGrace = 2 * time.Second
)

var errCallbackBudget = errors.New("callback result budget exceeded")
var errRunnerTransport = errors.New("runner transport disconnected")

// ExecutionResult preserves tool outcomes even if Python or its transport fails.
type ExecutionResult struct {
	// Value is the final JSON-compatible expression, or null on failure.
	Value json.RawMessage `json:"value"`
	// Output is bounded Python print output.
	Output string `json:"output"`
	// OutputTruncated indicates omitted print bytes.
	OutputTruncated bool `json:"output_truncated"`
	// Error describes the execution failure without recommending replay.
	Error *ExecutionError `json:"error,omitempty"`
	// ToolCalls records dispatch outcomes independently of Python's return value.
	ToolCalls []CallOutcome `json:"tool_calls"`
}

// ExecutionError is a bounded public failure, not an upstream internal error.
type ExecutionError struct {
	// Code identifies the failure category.
	Code string `json:"code"`
	// Message is a client-safe explanation.
	Message string `json:"message"`
}

// CallOutcome prevents accidental replay after a lost write response.
type CallOutcome struct {
	// Path is the exact gateway-qualified tool name.
	Path string `json:"path"`
	// Outcome is completed, not_dispatched, or unknown.
	Outcome string `json:"outcome"`
}

type runnerStart struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	ExecutionID string `json:"execution_id"`
	Code        string `json:"code"`
	WallMS      int64  `json:"wall_ms"`
}

type runnerReply struct {
	Type  string          `json:"type"`
	ID    uint32          `json:"id"`
	Value json.RawMessage `json:"value"`
	Error *string         `json:"error"`
}

type runnerMessage struct {
	Type            string          `json:"type"`
	ExecutionID     string          `json:"execution_id,omitempty"`
	ID              uint32          `json:"id,omitempty"`
	Method          string          `json:"method,omitempty"`
	Arguments       json.RawMessage `json:"arguments,omitempty"`
	Value           json.RawMessage `json:"value,omitempty"`
	Output          string          `json:"output,omitempty"`
	OutputTruncated bool            `json:"output_truncated,omitempty"`
	Code            string          `json:"code,omitempty"`
	Message         string          `json:"message,omitempty"`
}

type executionState struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	calls   []CallOutcome
	active  int
	bytes   int
}

// Run executes once, with no transport retry after submitting the start frame.
func (c *RunnerClient) Run(ctx context.Context, executionID uuid.UUID, code string, factory HostFactory) *ExecutionResult {
	result := &ExecutionResult{Value: json.RawMessage("null"), Output: "", OutputTruncated: false, Error: nil, ToolCalls: []CallOutcome{}}
	fail := func(code, message string) { result.Error = &ExecutionError{Code: code, Message: message} }
	if len(code) == 0 || len(code) > MaxSourceBytes || factory == nil {
		fail("invalid_program", "code must contain 1 to 65536 UTF-8 bytes")
		return result
	}
	ctx, wallCancel := context.WithTimeout(ctx, MaxWallTime)
	defer wallCancel()
	ctx, cancelCause := context.WithCancelCause(ctx)
	cancel := func() { cancelCause(context.Canceled) }
	defer cancel()
	if !c.slots.TryAcquire(1) {
		fail("admission_refused", "code runtime capacity is full; program was not submitted")
		return result
	}
	defer c.slots.Release(1)
	host, err := factory(ctx)
	if err != nil || host == nil || ctx.Err() != nil {
		fail("admission_refused", "tool scope was unavailable before submission")
		return result
	}
	failCancelled := func() {
		switch {
		case errors.Is(context.Cause(ctx), ErrExecutionRevoked):
			fail("authorization_revoked", "execution authorization changed; dispatched tool outcomes are recorded separately")
		case errors.Is(context.Cause(ctx), context.DeadlineExceeded):
			fail("deadline_exceeded", "execution deadline exceeded; dispatched tool outcomes may be unknown")
		case errors.Is(context.Cause(ctx), errRunnerTransport):
			fail("outcome_unknown", "runtime disconnected; dispatched tool outcomes may be unknown; do not replay automatically")
		case errors.Is(context.Cause(ctx), errCallbackBudget):
			fail("callback_limit", "callback result budget exceeded; dispatched tool outcomes are recorded separately")
		default:
			fail("cancelled", "execution cancelled; dispatched tool outcomes may be unknown")
		}
	}
	stream, err := c.open(ctx)
	if err != nil {
		fail("admission_refused", "code runtime unavailable; program was not submitted")
		return result
	}
	defer stream.finish()
	deadline, _ := ctx.Deadline()
	if err := stream.SetDeadline(deadline); err != nil {
		fail("admission_refused", "code stream could not be bounded")
		return result
	}
	interruptDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = stream.SetDeadline(time.Now()); close(interruptDone) })
	defer func() {
		if !stop() {
			<-interruptDone
		}
	}()
	var callbacks sync.WaitGroup
	state := &executionState{mu: sync.Mutex{}, writeMu: sync.Mutex{}, calls: []CallOutcome{}, active: 0, bytes: 0}
	defer func() {
		cancel()
		joined := make(chan struct{})
		go func() { callbacks.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(callbackCleanupGrace):
		}
		state.mu.Lock()
		result.ToolCalls = append(result.ToolCalls, state.calls...)
		state.mu.Unlock()
	}()
	remaining := time.Until(deadline).Milliseconds()
	if remaining < 1 {
		fail("admission_refused", "execution deadline elapsed before submission")
		return result
	}
	err = writeRunnerFrame(stream, runnerStart{Type: "start", Version: ProtocolVersion, ExecutionID: executionID.String(), Code: code, WallMS: remaining})
	if err != nil {
		fail("outcome_unknown", "submission response lost; do not replay this program automatically")
		return result
	}
	seen := make(map[uint32]bool)
	started := false
	for {
		frame, err := readRunnerFrame(stream)
		if err != nil {
			fail("outcome_unknown", "runtime disconnected; dispatched tool outcomes may be unknown; do not replay automatically")
			if ctx.Err() != nil {
				failCancelled()
			}
			return result
		}
		if ctx.Err() != nil {
			failCancelled()
			return result
		}
		var message runnerMessage
		if err := decodeStrict(frame, &message); err != nil {
			fail("protocol_error", "invalid runtime response; do not replay automatically")
			return result
		}
		switch message.Type {
		case "started":
			if started || message.ExecutionID != executionID.String() {
				fail("protocol_error", "invalid runtime admission")
				return result
			}
			started = true
		case "callback":
			if !started || seen[message.ID] || len(seen) >= maxCallbacks {
				fail("callback_limit", "execution stopped at callback boundary")
				return result
			}
			seen[message.ID] = true
			state.mu.Lock()
			if state.active >= maxConcurrentCallbacks {
				state.mu.Unlock()
				fail("callback_limit", "too many concurrent helper calls")
				return result
			}
			state.active++
			state.mu.Unlock()
			callbacks.Go(func() {
				value, callbackErr := state.callback(ctx, host, message)
				if errors.Is(callbackErr, ErrExecutionRevoked) {
					cancelCause(ErrExecutionRevoked)
					return
				}
				reply := runnerReply{Type: "callback_result", ID: message.ID, Value: value, Error: nil}
				if callbackErr != nil {
					reply.Value = json.RawMessage("null")
					reply.Error = new("tool operation unavailable or arguments invalid")
				}
				encoded, err := json.Marshal(reply)
				state.mu.Lock()
				state.bytes += len(encoded)
				overBudget := err != nil || len(value) > MaxResultBytes || len(encoded) > maxFrameBytes || state.bytes > maxCumulativeBytes
				state.mu.Unlock()
				if overBudget {
					cancelCause(errCallbackBudget)
					return
				}
				state.writeMu.Lock()
				defer state.writeMu.Unlock()
				if ctx.Err() != nil {
					return
				}
				// Decrement before publishing a reply: a fast runner may immediately
				// emit its next callback or completion when these bytes arrive.
				state.mu.Lock()
				state.active--
				state.mu.Unlock()
				if err := writeRunnerFrame(stream, reply); err != nil {
					cancelCause(errRunnerTransport)
				}
			})
		case "complete":
			state.mu.Lock()
			pending := state.active
			state.mu.Unlock()
			if !started || pending != 0 || len(message.Value) == 0 || len(message.Value) > maxValueBytes || len(message.Output) > maxPrintBytes {
				fail("protocol_error", "invalid runtime completion; do not replay automatically")
				return result
			}
			result.Value, result.Output, result.OutputTruncated = message.Value, message.Output, message.OutputTruncated
			return result
		case "error":
			if started && message.Code == "admission_refused" {
				fail("protocol_error", "runtime contradicted its admission; do not replay automatically")
				return result
			}
			if len(message.Code) > 128 || len(message.Message) > 4096 || message.Code == "" {
				fail("protocol_error", "invalid runtime error")
				return result
			}
			fail(message.Code, message.Message)
			return result
		default:
			fail("protocol_error", "unknown runtime message")
			return result
		}
	}
}

func (s *executionState) callback(ctx context.Context, host Host, message runnerMessage) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("helper cancelled: %w", err)
	}
	var value any
	var err error
	switch message.Method {
	case "servers":
		var args PageArgs
		if err := decodeStrict(message.Arguments, &args); err != nil {
			return nil, err
		}
		value, err = host.Servers(ctx, args)
	case "search":
		var args SearchArgs
		if err := decodeStrict(message.Arguments, &args); err != nil {
			return nil, err
		}
		value, err = host.Search(ctx, args)
	case "describe":
		var args struct {
			Path string `json:"path"`
		}
		if err := decodeStrict(message.Arguments, &args); err != nil {
			return nil, err
		}
		value, err = host.Describe(ctx, args.Path)
	case "call":
		var args struct {
			Path      string          `json:"path"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := decodeStrict(message.Arguments, &args); err != nil || len(args.Path) > 1024 {
			return nil, fmt.Errorf("invalid tool call arguments")
		}
		s.mu.Lock()
		index := len(s.calls)
		s.calls = append(s.calls, CallOutcome{Path: args.Path, Outcome: "unknown"})
		s.mu.Unlock()
		var result *ToolResult
		result, err = host.Call(ctx, args.Path, args.Arguments)
		s.mu.Lock()
		if err != nil {
			s.calls[index].Outcome = "not_dispatched"
		} else if result != nil {
			s.calls[index].Outcome = result.Outcome
		}
		s.mu.Unlock()
		value = result
	default:
		return nil, fmt.Errorf("unknown helper")
	}
	if err != nil {
		return nil, fmt.Errorf("code helper: %w", err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode helper result: %w", err)
	}
	return encoded, nil
}
