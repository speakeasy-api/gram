package promptinjection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const Source = "prompt_injection"

const Rule = "prompt_injection"

// LabelInjection is the positive class an engine returns for a flagged message.
const LabelInjection = "INJECTION"

// LabelSafe is the verdict an engine returns when it judged the message and
// found no attack.
const LabelSafe = "SAFE"

// LabelError is the outcome an engine returns when it could not reach a verdict
// (timeout, rate limit, transient failure). Realtime callers treat it exactly
// like SAFE (fail open, no finding), but strict callers such as the background
// skill scanner use it to retry instead of persisting a false SAFE.
const LabelError = "ERROR"

type Request struct {
	Messages  []judgemessage.Message
	OrgID     string
	ProjectID string
	// UserIDs is parallel to Messages: the scanned chat's owner per message
	// (empty string = unattributed). Rides on the judge's completion
	// telemetry so scanning volume attributes to whose traffic was analyzed.
	UserIDs []string
}

type Result struct {
	Label     string
	Score     float64
	Rationale string
}

type Classifier func(ctx context.Context, req Request) ([]Result, error)

func NoopClassifier(_ context.Context, req Request) ([]Result, error) {
	results := make([]Result, len(req.Messages))
	for i := range results {
		results[i] = Result{Label: LabelSafe, Score: 0, Rationale: ""}
	}
	return results, nil
}

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

func (s *Scanner) Scan(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message) ([]scanners.Finding, error) {
	if text == "" && !msg.HasContent() {
		return nil, nil
	}

	results, err := s.classifier(ctx, Request{Messages: []judgemessage.Message{msg}, OrgID: orgID, ProjectID: projectID, UserIDs: []string{userID}})
	if err != nil {
		s.logger.WarnContext(ctx, "pi judge scan failed; dropping prompt injection findings",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return nil, nil
	}
	if len(results) != 1 {
		return nil, nil
	}

	if f := s.findingFromResult(text, results[0]); f != nil {
		return []scanners.Finding{*f}, nil
	}
	return nil, nil
}

// ScanStrict is like Scan but surfaces a non-verdict (classifier error, wrong
// result count, or a LabelError fail-open outcome) as an error instead of
// silently returning no findings. Background scanners use it so a version is
// only recorded once the judge actually reached a SAFE/INJECTION decision;
// otherwise a transient judge outage would be persisted as a permanent SAFE.
func (s *Scanner) ScanStrict(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message) ([]scanners.Finding, error) {
	if text == "" && !msg.HasContent() {
		return nil, nil
	}

	results, err := s.classifier(ctx, Request{Messages: []judgemessage.Message{msg}, OrgID: orgID, ProjectID: projectID, UserIDs: []string{userID}})
	if err != nil {
		return nil, fmt.Errorf("classify prompt injection: %w", err)
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("prompt injection judge returned %d results, want 1", len(results))
	}

	switch results[0].Label {
	case LabelInjection:
		return []scanners.Finding{*s.findingFromResult(text, results[0])}, nil
	case LabelSafe:
		return nil, nil
	default:
		return nil, fmt.Errorf("prompt injection judge reached no verdict (label %q)", results[0].Label)
	}
}

func (s *Scanner) ScanBatch(ctx context.Context, texts []string, orgID, projectID string, userIDs []string, msgs []judgemessage.Message) ([][]scanners.Finding, error) {
	out := make([][]scanners.Finding, len(texts))
	if len(msgs) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge batch scan has mismatched message count",
			attr.SlogError(errors.New("len(msgs) != len(texts)")),
		)
		return out, nil
	}

	results, err := s.classifier(ctx, Request{Messages: msgs, OrgID: orgID, ProjectID: projectID, UserIDs: userIDs})
	if err != nil {
		s.logger.WarnContext(ctx, "pi judge batch scan failed; dropping prompt injection findings",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return out, nil
	}
	if len(results) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge returned mismatched batch size, dropping prompt injection findings",
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
