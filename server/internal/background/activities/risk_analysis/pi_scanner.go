package risk_analysis

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/feature"
)

// SourcePromptInjection is the policy source value that enables prompt
// injection scanning. Used by both the batch analyzer (writes findings to
// risk_results) and the realtime risk scanner (hook deny path for
// action='block' policies).
const SourcePromptInjection = "prompt_injection"

// PromptInjectionScanner combines two detection engines that emit the
// same canonical rule_id (`prompt-injection`):
//
//   - L0 heuristic regex/keyword rules — always run, cheap, in-process.
//   - L1 ML classifier (deberta-v3) — opt-in per org via the
//     feature.FlagPromptInjectionUseClassifier flag.
//
// L1 findings are appended to L0 findings, not substituted. The engine
// choice is an implementation detail not surfaced on the public contract.
// The classifier opt-in is ignored when the classifier is wired as a stub
// (no `--pi-classifier-url`) so local-dev still runs heuristics only.
type PromptInjectionScanner struct {
	classifier PromptInjectionClassifier
	flags      feature.Provider
	logger     *slog.Logger
}

// NewPromptInjectionScanner constructs a scanner that always runs L0
// heuristics. Orgs opt in to additionally run the L1 classifier via the
// FlagPromptInjectionUseClassifier feature flag. `flags` may be nil; when
// nil, no org gets L1.
func NewPromptInjectionScanner(logger *slog.Logger, classifier PromptInjectionClassifier, flags feature.Provider) *PromptInjectionScanner {
	return &PromptInjectionScanner{classifier: classifier, flags: flags, logger: logger}
}

// classifierEnabled returns true when this org has opted in to the L1
// classifier engine via feature flag and the classifier is a real (non-
// stub) implementation. Errors fall back to false.
func (s *PromptInjectionScanner) classifierEnabled(ctx context.Context, orgID string) bool {
	if _, isStub := s.classifier.(StubClassifier); isStub {
		return false
	}
	if s.flags == nil {
		return false
	}
	on, err := s.flags.IsFlagEnabled(ctx, feature.FlagPromptInjectionUseClassifier, orgID)
	if err != nil {
		s.logger.WarnContext(ctx, "prompt-injection classifier flag check failed; skipping L1",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	return on
}

// Scan runs L0 heuristics on a single text and, when the org has opted in
// to the L1 classifier via feature flag, appends an L1 finding if the
// classifier flags the text. Used by the realtime risk scanner on the
// hook path.
func (s *PromptInjectionScanner) Scan(ctx context.Context, text, orgID string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}

	findings := runHeuristics(text)

	if !s.classifierEnabled(ctx, orgID) {
		return findings, nil
	}

	results, err := s.classifier.Classify(ctx, []string{text})
	if err != nil {
		s.logger.WarnContext(ctx, "pi_classifier scan failed; returning L0 findings only",
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

// ScanBatch is the batched counterpart used by AnalyzeBatch. L0 runs over
// every text; when the org has opted in to the L1 classifier, a single
// batched Classify call is folded in on top.
func (s *PromptInjectionScanner) ScanBatch(ctx context.Context, texts []string, orgID string) ([][]Finding, error) {
	out := make([][]Finding, len(texts))
	for i, t := range texts {
		if t == "" {
			continue
		}
		out[i] = runHeuristics(t)
	}

	if !s.classifierEnabled(ctx, orgID) {
		return out, nil
	}

	results, err := s.classifier.Classify(ctx, texts)
	if err != nil {
		s.logger.WarnContext(ctx, "pi_classifier batch scan failed; returning L0 findings only",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
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
		Tags:             []string{"ml", "layer-1"},
		Source:           SourcePromptInjection,
		Confidence:       r.Score,
		DeadLetterReason: "",
	}
}

// DetectPromptInjection runs the L0 heuristic rules only. Kept for tests
// and for code paths that don't have a scanner instance. Returns one
// Finding per heuristic match.
func DetectPromptInjection(_ context.Context, text string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}
	return runHeuristics(text), nil
}
