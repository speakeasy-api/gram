package llmanalyzer

import (
	"context"
	"encoding/json"
	"sync"
)

// CompleteCall is one Complete invocation recorded by a StubCompleter.
type CompleteCall struct {
	// Info is the call attribution the analyzer passed.
	Info CallInfo

	// Messages are the chat turns the analyzer built.
	Messages []Message
}

// StubCompleter is a Completer for tests and local development. It answers
// every call with the configured response or error and records what it was
// asked. The zero value is usable and safe for concurrent use.
type StubCompleter struct {
	// Response is the completion content returned when Err is nil.
	Response string

	// Err is returned by Complete when non-nil.
	Err error

	// PromptTokens is reported as the completion's prompt token count.
	PromptTokens int

	// CompletionTokens is reported as the completion's completion token
	// count.
	CompletionTokens int

	// Model is reported as the completion's model name.
	Model string

	// Calls records every Complete invocation in order. Read it under
	// CallsSnapshot when other goroutines may still be calling.
	Calls []CompleteCall

	// ParseFailures counts RecordParseFailure invocations.
	ParseFailures int

	mu sync.Mutex
}

var _ Completer = (*StubCompleter)(nil)

// Complete records the call and returns the configured response or error.
func (s *StubCompleter) Complete(_ context.Context, info CallInfo, messages []Message) (Completion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Calls = append(s.Calls, CompleteCall{Info: info, Messages: messages})
	if s.Err != nil {
		return Completion{
			Content:          "",
			PromptTokens:     0,
			CompletionTokens: 0,
			Model:            s.Model,
			Attempts:         1,
		}, s.Err
	}
	return Completion{
		Content:          s.Response,
		PromptTokens:     s.PromptTokens,
		CompletionTokens: s.CompletionTokens,
		Model:            s.Model,
		Attempts:         1,
	}, nil
}

// RecordParseFailure counts the call.
func (s *StubCompleter) RecordParseFailure(context.Context, CallInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ParseFailures++
}

// CallsSnapshot returns a copy of the recorded calls.
func (s *StubCompleter) CallsSnapshot() []CompleteCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CompleteCall, len(s.Calls))
	copy(out, s.Calls)
	return out
}

// VerdictJSON renders a model reply carrying all four risk keys. Keys present
// in scores take that score; the others score 0. Every key carries the given
// reasoning.
func VerdictJSON(scores map[string]int, reasoning string) string {
	type risk struct {
		Score     int    `json:"score"`
		Reasoning string `json:"reasoning"`
	}
	object := make(map[string]risk, len(riskKeys))
	for _, key := range riskKeys {
		object[key] = risk{Score: scores[key], Reasoning: reasoning}
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		// Marshalling a map of plain structs cannot fail; keep the reply a
		// parsable object anyway.
		return "{}"
	}
	return string(encoded)
}
