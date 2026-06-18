package risk_analysis

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// LabelInjection is the positive class an engine returns for a flagged message.
const LabelInjection = "INJECTION"

// LabelSafe is the negative class: the message is not a prompt attack. It is
// also the fail-open verdict an engine returns when it cannot reach a decision
// (timeout, error, rate limit) so a judge outage degrades to the L0 heuristics.
const LabelSafe = "SAFE"

// PromptInjectionRequest carries the messages to evaluate plus the org/project context
// the L1 engine needs for per-org billing and rate limiting. Messages are
// structured (actor, tool attribution, body kind) so the engine can reason
// about a tool call vs a user prompt vs a tool result, not just raw text.
type PromptInjectionRequest struct {
	Messages  []JudgeMessage
	OrgID     string
	ProjectID string
}

// PromptInjectionResult is one prediction. Label decides whether to emit an L1 finding
// (LabelInjection => emit); Score is retained as confidence metadata.
type PromptInjectionResult struct {
	Label string
	Score float64
}

// PromptInjectionEngine is the L1 prompt-injection engine: it scores each message in the
// request and returns one result per message, aligned by index. The production
// implementation is the OpenRouter LLM judge (server/internal/pijudge). The
// scanner takes it as a plain function rather than an interface so risk_analysis
// stays free of the openrouter dependency chain (openrouter -> productfeatures
// -> authz), and so "no L1 engine" is simply a nil func — no stub type needed.
type PromptInjectionEngine func(ctx context.Context, req PromptInjectionRequest) ([]PromptInjectionResult, error)

// DescribePromptInjection returns the canonical (rule_id, description) for any
// prompt-injection finding. The same rule id is emitted regardless of whether
// the match came from the L1 engine or an L0 heuristic.
func DescribePromptInjection() (string, string) {
	return guard(RulePromptInjection), "Detected a prompt injection attempt."
}

// PromptInjectionScanner combines two detection layers that emit the same
// canonical rule_id (`prompt_injection`):
//
//   - L0 heuristic regex/keyword rules — always run, cheap, in-process.
//   - L1 engine (the LLM judge) — opt-in per org via the
//     feature.FlagPromptInjectionUseClassifier flag.
//
// L1 findings are appended to L0 findings, not substituted. The L1 opt-in flag
// is resolved by the caller (once per scan, with the org/project PostHog groups)
// and passed in as l1Enabled — mirroring how the realtime scanner and batch
// analyzer resolve the prompt-policies flag once and fan out. The scanner skips
// L1 when no engine is wired (classify is nil) so local-dev and tests run
// heuristics only regardless of the flag.
type PromptInjectionScanner struct {
	classify PromptInjectionEngine // nil => L1 disabled (L0 heuristics only)
	logger   *slog.Logger
}

// NewPromptInjectionScanner constructs a scanner that always runs L0
// heuristics. Pass the L1 engine's Classify function to additionally run L1
// (opt-in per scan via l1Enabled), or nil to run heuristics only.
func NewPromptInjectionScanner(logger *slog.Logger, classify PromptInjectionEngine) *PromptInjectionScanner {
	return &PromptInjectionScanner{classify: classify, logger: logger}
}

// l1Active reports whether the L1 engine should run for this scan: the caller
// opted this org/project in (l1Enabled) AND an engine is wired (classify != nil).
func (s *PromptInjectionScanner) l1Active(l1Enabled bool) bool {
	return s.classify != nil && l1Enabled
}

// Scan runs L0 heuristics on a single text and, when l1Enabled and an engine is
// wired, appends an L1 finding if the engine flags the message. text is the
// flattened body the L0 heuristics scan; msg is the structured event (actor,
// tool attribution, body kind) the L1 engine reasons over. orgID/projectID
// identify the caller for per-org billing and rate limiting. Used by the
// realtime risk scanner on the hook path.
func (s *PromptInjectionScanner) Scan(ctx context.Context, text, orgID, projectID string, msg JudgeMessage, l1Enabled bool) ([]Finding, error) {
	if text == "" && !msg.HasContent() {
		return nil, nil
	}

	findings := runHeuristics(text)

	if !s.l1Active(l1Enabled) {
		return findings, nil
	}

	results, err := s.classify(ctx, PromptInjectionRequest{Messages: []JudgeMessage{msg}, OrgID: orgID, ProjectID: projectID})
	if err != nil {
		s.logger.WarnContext(ctx, "pi L1 scan failed; returning L0 findings only",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return findings, nil
	}
	if len(results) != 1 {
		return findings, nil
	}

	if f := s.findingFromResult(text, results[0]); f != nil {
		findings = append(findings, *f)
	}
	return findings, nil
}

// ScanBatch is the batched counterpart used by AnalyzeBatch. L0 runs over every
// text; when l1Enabled and an engine is wired, a single batched Classify call
// over the structured messages is folded in on top. texts and msgs are aligned
// by index (texts[i] is the flattened body of msgs[i]).
func (s *PromptInjectionScanner) ScanBatch(ctx context.Context, texts []string, orgID, projectID string, msgs []JudgeMessage, l1Enabled bool) ([][]Finding, error) {
	out := make([][]Finding, len(texts))
	for i, t := range texts {
		if t == "" {
			continue
		}
		out[i] = runHeuristics(t)
	}

	if !s.l1Active(l1Enabled) {
		return out, nil
	}

	results, err := s.classify(ctx, PromptInjectionRequest{Messages: msgs, OrgID: orgID, ProjectID: projectID})
	if err != nil {
		s.logger.WarnContext(ctx, "pi L1 batch scan failed; returning L0 findings only",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return out, nil
	}
	if len(results) != len(texts) {
		s.logger.WarnContext(ctx, "pi engine returned mismatched batch size, dropping L1 findings",
			attr.SlogError(errors.New("len(results) != len(texts)")),
		)
		return out, nil
	}

	for i, r := range results {
		if texts[i] == "" {
			continue
		}
		if f := s.findingFromResult(texts[i], r); f != nil {
			out[i] = append(out[i], *f)
		}
	}
	return out, nil
}

func (s *PromptInjectionScanner) findingFromResult(text string, r PromptInjectionResult) *Finding {
	if r.Label != LabelInjection {
		return nil
	}
	ruleID, description := DescribePromptInjection()
	return &Finding{
		RuleID:           ruleID,
		Description:      description,
		Match:            text,
		StartPos:         0,
		EndPos:           len(text),
		Tags:             []string{"llm-judge", "layer-1"},
		Source:           SourcePromptInjection,
		Confidence:       r.Score,
		DeadLetterReason: "",
		toolCallID:       "",
	}
}

// DetectPromptInjection runs the L0 heuristic rules only. Kept for tests and for
// code paths that don't have a scanner instance.
func DetectPromptInjection(_ context.Context, text string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}
	return runHeuristics(text), nil
}
