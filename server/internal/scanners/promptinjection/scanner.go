package promptinjection

import (
	"context"
	"errors"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const Source = "prompt_injection"

const Rule = "prompt_injection"

// LabelInjection is the positive class an engine returns for a flagged message.
const LabelInjection = "INJECTION"

// LabelSafe is the fail-open verdict when an engine cannot reach a decision.
const LabelSafe = "SAFE"

type Request struct {
	Messages  []judgemessage.Message
	OrgID     string
	ProjectID string
}

type Result struct {
	Label     string
	Score     float64
	Rationale string
}

type Classifier interface {
	Classify(ctx context.Context, req Request) ([]Result, error)
}

type ClassifierFunc func(ctx context.Context, req Request) ([]Result, error)

func (f ClassifierFunc) Classify(ctx context.Context, req Request) ([]Result, error) {
	return f(ctx, req)
}

type noopClassifier struct{}

func (noopClassifier) Classify(_ context.Context, req Request) ([]Result, error) {
	results := make([]Result, len(req.Messages))
	for i := range results {
		results[i] = Result{Label: LabelSafe, Score: 0, Rationale: ""}
	}
	return results, nil
}

var NoopClassifier Classifier = noopClassifier{}

func Describe() (string, string) {
	return scanners.GuardRuleID(Rule), "Detected a prompt injection attempt."
}

type Scanner struct {
	classifier Classifier
	logger     *slog.Logger
}

func NewScanner(logger *slog.Logger, classifier Classifier) *Scanner {
	if classifier == nil {
		classifier = NoopClassifier
	}
	return &Scanner{classifier: classifier, logger: logger}
}

func (s *Scanner) l1Active(l1Enabled bool) bool {
	return l1Enabled
}

func (s *Scanner) Scan(ctx context.Context, text, orgID, projectID string, msg judgemessage.Message, l1Enabled bool) ([]scanners.Finding, error) {
	if text == "" && !msg.HasContent() {
		return nil, nil
	}

	findings := runHeuristics(text)

	if !s.l1Active(l1Enabled) {
		return findings, nil
	}

	results, err := s.classifier.Classify(ctx, Request{Messages: []judgemessage.Message{msg}, OrgID: orgID, ProjectID: projectID})
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

func (s *Scanner) ScanBatch(ctx context.Context, texts []string, orgID, projectID string, msgs []judgemessage.Message, l1Enabled bool) ([][]scanners.Finding, error) {
	out := make([][]scanners.Finding, len(texts))
	for i, t := range texts {
		if t == "" {
			continue
		}
		out[i] = runHeuristics(t)
	}

	if !s.l1Active(l1Enabled) {
		return out, nil
	}

	results, err := s.classifier.Classify(ctx, Request{Messages: msgs, OrgID: orgID, ProjectID: projectID})
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
		if texts[i] == "" && !msgs[i].HasContent() {
			continue
		}
		if f := s.findingFromResult(texts[i], r); f != nil {
			out[i] = append(out[i], *f)
		}
	}
	return out, nil
}

func (s *Scanner) findingFromResult(text string, r Result) *scanners.Finding {
	if r.Label != LabelInjection {
		return nil
	}
	ruleID, description := Describe()
	if r.Rationale != "" {
		description = r.Rationale
	}
	return &scanners.Finding{
		RuleID:              ruleID,
		Description:         description,
		Match:               text,
		StartPos:            0,
		EndPos:              len(text),
		Tags:                []string{"llm-judge", "layer-1"},
		Source:              Source,
		Confidence:          r.Score,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}

func Detect(_ context.Context, text string) ([]scanners.Finding, error) {
	if text == "" {
		return nil, nil
	}
	return runHeuristics(text), nil
}
