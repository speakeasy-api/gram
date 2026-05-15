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

// promptInjectionClassifierFindingDescription is the human-readable
// description carried on the Finding emitted when the L1 model flags a
// text. Kept short — the dashboard renders this verbatim.
const promptInjectionClassifierFindingDescription = "Detected a prompt injection attempt."

// PromptInjectionScanner emits prompt-injection findings using one of two
// engines:
//   - L1 ML classifier (deberta-v3) — the default
//   - L0 heuristic regex/keyword rules — opt-in per org via the
//     feature.FlagPromptInjectionUseRegex flag
//
// Both engines emit the same canonical rule_id (`prompt-injection`); the
// engine choice is an implementation detail not surfaced on the public
// contract. If the classifier is wired as a stub (no `--pi-classifier-url`),
// the scanner falls back to L0 regardless of the flag so local-dev still
// produces findings.
type PromptInjectionScanner struct {
	classifier PromptInjectionClassifier
	flags      feature.Provider
	logger     *slog.Logger
}

// NewPromptInjectionScanner constructs a scanner that defaults to the L1
// classifier. `flags` may be nil; when nil (and the classifier is real),
// every org gets the default L1 engine.
func NewPromptInjectionScanner(logger *slog.Logger, classifier PromptInjectionClassifier, flags feature.Provider) *PromptInjectionScanner {
	return &PromptInjectionScanner{classifier: classifier, flags: flags, logger: logger}
}

// useRegex returns true when this scan should use the L0 regex engine
// instead of the default L1 classifier. The decision is per-org: a feature
// flag flips the engine. When the classifier is a stub (no sidecar), L0 is
// the only thing that can produce findings.
func (s *PromptInjectionScanner) useRegex(ctx context.Context, orgID string) bool {
	if _, isStub := s.classifier.(StubClassifier); isStub {
		return true
	}
	if s.flags == nil {
		return false
	}
	on, err := s.flags.IsFlagEnabled(ctx, feature.FlagPromptInjectionUseRegex, orgID)
	if err != nil {
		s.logger.WarnContext(ctx, "prompt-injection engine flag check failed; defaulting to classifier",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return false
	}
	return on
}

// Scan runs prompt-injection detection on a single text. Used by the
// realtime risk scanner on the hook path.
func (s *PromptInjectionScanner) Scan(ctx context.Context, text, orgID string) ([]Finding, error) {
	if text == "" {
		return nil, nil
	}

	if s.useRegex(ctx, orgID) {
		return runHeuristics(text), nil
	}

	results, err := s.classifier.Classify(ctx, []string{text})
	if err != nil {
		s.logger.WarnContext(ctx, "pi_classifier scan failed, falling back to heuristics",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return runHeuristics(text), nil
	}
	if len(results) != 1 {
		return runHeuristics(text), nil
	}

	if f := s.findingFromResult(text, results[0]); f != nil {
		return []Finding{*f}, nil
	}
	return nil, nil
}

// ScanBatch is the batched counterpart used by AnalyzeBatch. The whole
// batch runs through one engine — there is no L0 + L1 mixing.
func (s *PromptInjectionScanner) ScanBatch(ctx context.Context, texts []string, orgID string) ([][]Finding, error) {
	out := make([][]Finding, len(texts))

	if s.useRegex(ctx, orgID) {
		for i, t := range texts {
			if t == "" {
				continue
			}
			out[i] = runHeuristics(t)
		}
		return out, nil
	}

	// L1 — single batched HTTP call.
	results, err := s.classifier.Classify(ctx, texts)
	if err != nil {
		s.logger.WarnContext(ctx, "pi_classifier batch scan failed, falling back to heuristics",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		for i, t := range texts {
			if t == "" {
				continue
			}
			out[i] = runHeuristics(t)
		}
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
	ruleID, description := Normalize(SourcePromptInjection, RulePromptInjection, promptInjectionClassifierFindingDescription, RuleContext{ToolName: "", MatchedPattern: ""})
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
