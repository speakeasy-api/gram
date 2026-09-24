package promptinjection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

const Source = "prompt_injection"

const Rule = "prompt_injection"

// LabelInjection is the positive class an engine returns for a flagged message.
const LabelInjection = "INJECTION"

// LabelSafe is a judgement: the engine looked at the content and cleared it.
const LabelSafe = "SAFE"

// LabelUnavailable is the fail-open verdict when an engine cannot reach a
// decision at all — outage, timeout, rate limit, cancellation, no judge wired.
// It yields no finding just like LabelSafe, so gating still fails open, but it
// stays distinguishable so ScanStrict can refuse to let a caller record it as
// a clean judgement. (cubic)
const LabelUnavailable = "UNAVAILABLE"

// ErrNoVerdict reports that the judge never reached a decision. Distinct from
// a clean verdict: nothing was judged, so nothing may be recorded as clean.
var ErrNoVerdict = errors.New("pi judge reached no verdict")

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
	Label         string
	Score         float64
	Rationale     string
	DirectiveKind string
	Target        string
	Operational   bool
	// STokens counts the exact prepared content-bearing values sent to the
	// classifier.
	STokens int64
	// Completed reports that a real classifier returned a valid verdict and its
	// prepared content was counted successfully.
	Completed bool
	// Model identifies the classifier model when applicable.
	Model string
	// Provider identifies the classifier provider when applicable.
	Provider string
}

type Classifier func(ctx context.Context, req Request) ([]Result, error)

// NoopClassifier stands in when no judge is configured. It reaches no verdict
// rather than clearing content nothing looked at.
func NoopClassifier(_ context.Context, req Request) ([]Result, error) {
	results := make([]Result, len(req.Messages))
	for i := range results {
		results[i] = Result{Label: LabelUnavailable, Score: 0, Rationale: "", DirectiveKind: "", Target: "", Operational: false, STokens: 0, Completed: false, Model: "", Provider: ""}
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

// Scan fails open: a judge that cannot reach a verdict yields no findings, so
// an outage never turns into a blocked message on the gating path.
func (s *Scanner) Scan(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message, trajectories ...judgemessage.Trajectory) (scanners.Result, error) {
	result, _, err := s.ScanWithVerdict(ctx, text, orgID, projectID, userID, msg, trajectories...)
	return result, err
}

// ScanWithVerdict preserves provider attribution for the executor that emits
// usage while keeping enforcement findings in the shared scanner result.
func (s *Scanner) ScanWithVerdict(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message, trajectories ...judgemessage.Trajectory) (scanners.Result, Result, error) {
	result, verdict, err := s.ScanStrictWithVerdict(ctx, text, orgID, projectID, userID, msg, trajectories...)
	if err != nil {
		if errors.Is(err, ErrNoVerdict) {
			return scanners.Result{Findings: nil, STokens: verdict.STokens, Completed: false}, verdict, nil
		}
		s.logger.WarnContext(ctx, "pi judge scan failed; dropping prompt injection findings",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return scanners.Result{Findings: nil, STokens: 0, Completed: false}, verdict, nil
	}
	return result, verdict, nil
}

// ScanStrict reports a judge failure instead of failing open. Callers that
// record whether a scan happened need to tell "judged clean" apart from "never
// judged" - collapsing the two writes down a durable claim that content is
// clean on the strength of an outage.
func (s *Scanner) ScanStrict(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message, trajectories ...judgemessage.Trajectory) (scanners.Result, error) {
	result, _, err := s.ScanStrictWithVerdict(ctx, text, orgID, projectID, userID, msg, trajectories...)
	return result, err
}

// ScanStrictWithVerdict returns provider failure instead of failing open and
// preserves provider attribution for successful usage emission.
func (s *Scanner) ScanStrictWithVerdict(ctx context.Context, text, orgID, projectID, userID string, msg judgemessage.Message, trajectories ...judgemessage.Trajectory) (scanners.Result, Result, error) {
	if text == "" && !msg.HasContent() {
		return scanners.Result{Findings: nil, STokens: 0, Completed: false}, Result{Label: "", Score: 0, Rationale: "", DirectiveKind: "", Target: "", Operational: false, STokens: 0, Completed: false, Model: "", Provider: ""}, nil
	}

	trajectory := judgemessage.Trajectory{PriorUserRequest: "", RecentUntrustedContent: ""}
	if len(trajectories) > 0 {
		trajectory = trajectories[0]
	}
	results, err := s.classifier(ctx, Request{Messages: []judgemessage.Message{msg}, Trajectories: []judgemessage.Trajectory{trajectory}, OrgID: orgID, ProjectID: projectID, UserIDs: []string{userID}})
	if err != nil {
		return scanners.Result{Findings: nil, STokens: 0, Completed: false}, Result{Label: "", Score: 0, Rationale: "", DirectiveKind: "", Target: "", Operational: false, STokens: 0, Completed: false, Model: "", Provider: ""}, fmt.Errorf("pi judge classify: %w", err)
	}
	if len(results) != 1 {
		return scanners.Result{Findings: nil, STokens: 0, Completed: false}, Result{Label: "", Score: 0, Rationale: "", DirectiveKind: "", Target: "", Operational: false, STokens: 0, Completed: false, Model: "", Provider: ""}, fmt.Errorf("pi judge returned %d results for 1 message", len(results))
	}
	verdict := results[0]
	if verdict.Label == LabelUnavailable {
		return scanners.Result{Findings: nil, STokens: verdict.STokens, Completed: false}, verdict, ErrNoVerdict
	}

	findings := []scanners.Finding{}
	if f := s.findingFromResult(text, verdict); f != nil {
		findings = append(findings, *f)
	}
	return scanners.Result{Findings: findings, STokens: verdict.STokens, Completed: verdict.Completed}, verdict, nil
}

func (s *Scanner) ScanBatch(ctx context.Context, texts []string, orgID, projectID string, userIDs []string, msgs []judgemessage.Message, trajectorySets ...[]judgemessage.Trajectory) ([]scanners.Result, error) {
	out, _, err := s.ScanBatchWithVerdicts(ctx, texts, orgID, projectID, userIDs, msgs, trajectorySets...)
	return out, err
}

// ScanBatchWithVerdicts preserves provider attribution alongside the scanner
// results for callers that emit per-item usage.
func (s *Scanner) ScanBatchWithVerdicts(ctx context.Context, texts []string, orgID, projectID string, userIDs []string, msgs []judgemessage.Message, trajectorySets ...[]judgemessage.Trajectory) ([]scanners.Result, []Result, error) {
	out := make([]scanners.Result, len(texts))
	if len(msgs) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge batch scan has mismatched message count",
			attr.SlogError(errors.New("len(msgs) != len(texts)")),
		)
		return out, nil, nil
	}

	var trajectories []judgemessage.Trajectory
	if len(trajectorySets) > 0 {
		trajectories = trajectorySets[0]
	}
	if len(trajectories) != 0 && len(trajectories) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge batch scan has nonparallel trajectories; unmatched messages scan without trajectory context",
			attr.SlogError(errors.New("len(trajectories) != len(texts)")),
			attr.SlogOrganizationID(orgID),
		)
	}
	results, err := s.classifier(ctx, Request{Messages: msgs, Trajectories: trajectories, OrgID: orgID, ProjectID: projectID, UserIDs: userIDs})
	if err != nil {
		s.logger.WarnContext(ctx, "pi judge batch scan failed; dropping prompt injection findings",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
		return out, nil, nil
	}
	if len(results) != len(texts) {
		s.logger.WarnContext(ctx, "pi judge returned mismatched batch size, dropping prompt injection findings",
			attr.SlogError(errors.New("len(results) != len(texts)")),
		)
		return out, results, nil
	}

	for i, r := range results {
		if texts[i] == "" && !msgs[i].HasContent() {
			continue
		}
		if r.Label == LabelUnavailable {
			continue
		}
		findings := []scanners.Finding{}
		if f := s.findingFromResult(texts[i], r); f != nil {
			findings = append(findings, *f)
		}
		out[i] = scanners.Result{Findings: findings, STokens: r.STokens, Completed: r.Completed}
	}
	return out, results, nil
}

func (s *Scanner) findingFromResult(text string, r Result) *scanners.Finding {
	if r.Label != LabelInjection {
		return nil
	}
	// Emit the finding into the existing risk-policy path. That static policy,
	// not the judge metadata, decides whether the finding blocks or surfaces.
	ruleID, description := Describe()
	if r.Rationale != "" {
		description = r.Rationale
	}
	tags := []string{"llm-judge", "layer-1"}
	if r.DirectiveKind != "" {
		tags = append(tags,
			"semantic-typed",
			"directive_kind:"+r.DirectiveKind,
			"target:"+r.Target,
			"operational:"+strconv.FormatBool(r.Operational),
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
