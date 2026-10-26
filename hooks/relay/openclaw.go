package relay

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/speakeasy-api/agenthooks"
)

// openclawLLMInputHook is OpenClaw's native name for the hook that fires
// before the model call of a run. Its event carries the run's user prompt.
const openclawLLMInputHook = "llm_input"

// openclawPromptRunsCap bounds how many OpenClaw runs the relay remembers as
// having reported their user prompt. The serve daemon lives as long as the
// Gateway, so the record must not grow with every run. 1024 is far more runs
// than one Gateway has in flight at once. A run evicted while it is still
// running can report its prompt a second time at most, and the server keeps
// one user message per OpenClaw run id.
const openclawPromptRunsCap = 1024

// openclawPromptRuns records the OpenClaw runs whose user prompt the relay
// already reported, so that each run reports it once. The serve loop handles
// gate frames (before_agent_run) inline and observe frames (llm_input) on a
// worker goroutine, so every access holds mu.
type openclawPromptRuns struct {
	mu sync.Mutex

	// seen holds the recorded run ids.
	seen map[string]struct{}

	// ring holds the recorded run ids in insertion order. A new run id
	// replaces the oldest one when the ring is full.
	ring [openclawPromptRunsCap]string

	// next is the ring slot that the next recorded run id takes.
	next int
}

func newOpenclawPromptRuns() *openclawPromptRuns {
	return &openclawPromptRuns{
		mu:   sync.Mutex{},
		seen: make(map[string]struct{}, openclawPromptRunsCap),
		ring: [openclawPromptRunsCap]string{},
		next: 0,
	}
}

// claim records runID and reports whether this call recorded it first.
func (p *openclawPromptRuns) claim(runID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, ok := p.seen[runID]; ok {
		return false
	}
	if evicted := p.ring[p.next]; evicted != "" {
		delete(p.seen, evicted)
	}
	p.ring[p.next] = runID
	p.next = (p.next + 1) % openclawPromptRunsCap
	p.seen[runID] = struct{}{}
	return true
}

// openclawRunID returns the OpenClaw run that an event belongs to: ctx.runId,
// which the decoder maps to the turn id, else the event's own runId.
func openclawRunID(e *agenthooks.Event) string {
	if runID := strings.TrimSpace(e.Session.TurnID); runID != "" {
		return runID
	}
	var runID string
	if raw := e.RawField("event.runId"); len(raw) > 0 && json.Unmarshal(raw, &runID) == nil {
		return strings.TrimSpace(runID)
	}
	return ""
}

// noteOpenclawPrompt records that a before_agent_run gate reported the user
// prompt of its run, so that the llm_input of the same run does not report it
// again.
func (r *Relay) noteOpenclawPrompt(e *agenthooks.PromptEvent) {
	if e.Provider != agenthooks.ProviderOpenClaw || strings.TrimSpace(e.Prompt) == "" {
		return
	}
	if runID := openclawRunID(&e.Event); runID != "" {
		r.openclawPrompts.claim(runID)
	}
}

// onModelRequest reports the user prompt of an OpenClaw run from llm_input
// when no before_agent_run gate reported it. OpenClaw's Codex app-server
// harness fires llm_input but never before_agent_run, so without this report
// the turn has no user message. The built-in harnesses fire before_agent_run
// first, which records the run, so the relay ignores their llm_input.
//
// The report is observe-only. OpenClaw does not await llm_input, and it fires
// no gate on the Codex harness, so prompt policies cannot block those turns.
// The relay discards the server's verdict.
func (r *Relay) onModelRequest(ctx context.Context, e *agenthooks.ModelEvent) error {
	if e.Provider != agenthooks.ProviderOpenClaw || e.NativeName != openclawLLMInputHook {
		return nil
	}
	// Without a run id the relay cannot tell whether before_agent_run already
	// reported this turn, and the server correlates OpenClaw prompts by run.
	runID := openclawRunID(&e.Event)
	if runID == "" {
		return nil
	}
	var prompt string
	if raw := e.RawField("event.prompt"); len(raw) == 0 || json.Unmarshal(raw, &prompt) != nil || strings.TrimSpace(prompt) == "" {
		return nil
	}
	if !r.openclawPrompts.claim(runID) {
		return nil
	}

	pe := &agenthooks.PromptEvent{Event: e.Event, Prompt: prompt}
	pe.Kind = agenthooks.KindPromptSubmitted
	pe.Session.TurnID = runID
	r.deliver(ctx, pe)
	return nil
}
