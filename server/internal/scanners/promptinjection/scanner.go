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
	Messages     []judgemessage.Message
	Trajectories []judgemessage.Trajectory
	OrgID        string
	ProjectID    string
	// UserIDs is parallel to Messages: the scanned chat's owner per message
	// (empty string = unattributed). Rides on the judge's completion
	// telemetry so scanning volume attributes to whose traffic was analyzed.
	UserIDs []string
}

type Result struct {
	Label     string
	Score     float64
	Rationale string
	Kind      string
	Target    string
	Severity  string
	Action    string
}

type Classifier func(ctx context.Context, req Request) ([]Result, error)

func NoopClassifier(_ context.Context, req Request) ([]Result, error) {
	results := make([]Result, len(req.Messages))
	for i := range results {
		results[i] = Result{Label: LabelSafe, Score: 0, Rationale: "", Kind: "", Target: "", Severity: "", Action: ""}
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

func (s *Scanner) Scan(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message, trajectories ...judgemessage.Trajectory) ([]scanners.Finding, error) {
	if text == "" && !msg.HasContent() {
		return nil, nil
	}

	trajectory := judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}
	if len(trajectories) > 0 {
		trajectory = trajectories[0]
	}
	results, err := s.classifier(ctx, Request{Messages: []judgemessage.Message{msg}, Trajectories: []judgemessage.Trajectory{trajectory}, OrgID: orgID, ProjectID: projectID, UserIDs: []string{userID}})
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

func (s *Scanner) ScanBatch(ctx context.Context, texts []string, orgID, projectID string, userIDs []string, msgs []judgemessage.Message, trajectorySets ...[]judgemessage.Trajectory) ([][]scanners.Finding, error) {
	out := make([][]scanners.Finding, len(texts))
	if len(msgs) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge batch scan has mismatched message count",
			attr.SlogError(errors.New("len(msgs) != len(texts)")),
		)
		return out, nil
	}

	var trajectories []judgemessage.Trajectory
	if len(trajectorySets) > 0 {
		trajectories = trajectorySets[0]
	}
	results, err := s.classifier(ctx, Request{Messages: msgs, Trajectories: trajectories, OrgID: orgID, ProjectID: projectID, UserIDs: userIDs})
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
	tags := []string{"llm-judge", "layer-1"}
	if r.Kind != "" {
		tags = append(tags,
			"semantic-consensus",
			"directive_kind:"+r.Kind,
			"target:"+r.Target,
			"severity:"+r.Severity,
			"action:"+r.Action,
		)
	}
	return &scanners.Finding{
		RuleID:              ruleID,
		Description:         description,
		Match:               text,
		StartPos:            0,
		EndPos:              len(text),
		Tags:                tags,
		Source:              Source,
		Confidence:          r.Score,
		DeadLetterReason:    "",
		McpLookupToolCallID: "",
		SpanGroupKey:        "",
		Field:               "",
		Path:                "",
	}
}
