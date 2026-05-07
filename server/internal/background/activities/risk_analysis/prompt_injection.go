package risk_analysis

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// RulePIClassifierDeberta is the rule id stored in
// risk_policies.prompt_injection_rules to opt a policy in to L1
// classifier-backed detection on top of the always-on L0 heuristics.
const RulePIClassifierDeberta = "deberta-v3-classifier"

// piClassifierFindingDescription is the human-readable description carried
// on the Finding emitted when the L1 model flags a text. Kept short — the
// dashboard renders this verbatim under the policy result row.
const piClassifierFindingDescription = "ML classifier flagged prompt injection"

// PromptInjectionScanner runs the always-on L0 heuristic rules and, when a
// policy opts in via prompt_injection_rules, the L1 ML classifier.
//
// Construction always wires a non-nil classifier (StubClassifier when
// --pi-classifier-url is empty), so callers don't branch on availability.
type PromptInjectionScanner struct {
	classifier PromptInjectionClassifier
	threshold  float64
	logger     *slog.Logger
}

// NewPromptInjectionScanner returns a scanner that calls the given classifier
// for L1 detection. threshold is the minimum INJECTION-class probability
// required to emit an L1 finding (typical: 0.9 — high precision; the model
// argmaxes at 0.5 so anything below that is a SAFE prediction).
//
// logger must be non-nil; pass an explicit *slog.Logger so log lines carry the
// caller's component attrs (forbidigo blocks slog.Default in this codebase).
func NewPromptInjectionScanner(logger *slog.Logger, classifier PromptInjectionClassifier, threshold float64) *PromptInjectionScanner {
	return &PromptInjectionScanner{classifier: classifier, threshold: threshold, logger: logger}
}

// Scan runs the heuristic rules unconditionally; runs the L1 classifier when
// rules contains RulePIClassifierDeberta. Used by the realtime risk scanner.
func (s *PromptInjectionScanner) Scan(ctx context.Context, text string, rules []string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}

	findings := runHeuristics(text)

	if !slices.Contains(rules, RulePIClassifierDeberta) {
		return findings, nil
	}

	results, err := s.classifier.Classify(ctx, []string{text})
	if err != nil {
		// Don't fail the scan on classifier errors — surface L0 findings and
		// let the per-batch error counter pick up the failure.
		s.logger.WarnContext(ctx, "pi_classifier scan failed, continuing with heuristics only", attr.SlogError(err))
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

// ScanBatch is the batched counterpart used by AnalyzeBatch. When the L1
// classifier is enabled, all texts go through a single Classify call so the
// HTTP cost is paid once per activity, not once per message.
func (s *PromptInjectionScanner) ScanBatch(ctx context.Context, texts []string, rules []string) ([][]Finding, error) {
	out := make([][]Finding, len(texts))

	// L0 — always.
	for i, t := range texts {
		if t == "" {
			continue
		}
		out[i] = runHeuristics(t)
	}

	if !slices.Contains(rules, RulePIClassifierDeberta) {
		return out, nil
	}

	// L1 — single batched HTTP call.
	results, err := s.classifier.Classify(ctx, texts)
	if err != nil {
		s.logger.WarnContext(ctx, "pi_classifier batch scan failed, continuing with heuristics only", attr.SlogError(err))
		return out, nil
	}
	if len(results) != len(texts) {
		s.logger.WarnContext(ctx, "pi_classifier returned mismatched batch size, dropping L1 findings",
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

func (s *PromptInjectionScanner) findingFromResult(text string, r ClassifierResult) *Finding {
	if r.Label != LabelInjection || r.Score < s.threshold {
		return nil
	}
	return &Finding{
		RuleID:           "pi." + RulePIClassifierDeberta,
		Description:      piClassifierFindingDescription,
		Match:            text,
		StartPos:         0,
		EndPos:           len(text),
		Tags:             []string{"ml", "layer-1"},
		Source:           SourcePromptInjection,
		Confidence:       r.Score,
		DeadLetterReason: "",
	}
}

// DetectPromptInjection runs the L0 heuristic rules only. Kept for tests and
// for code paths that don't have a scanner instance (none in production —
// production callers must use PromptInjectionScanner so policy.prompt_injection_rules
// is honored). Returns one Finding per heuristic match.
func DetectPromptInjection(_ context.Context, text string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}
	return runHeuristics(text), nil
}
