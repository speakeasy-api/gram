package promptpolicy

import (
	"context"
	"log/slog"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/scanners"
)

type Scanner struct {
	logger   *slog.Logger
	evaluate Evaluator
}

func NewScanner(logger *slog.Logger, evaluate Evaluator) *Scanner {
	return &Scanner{
		logger:   logger,
		evaluate: evaluate,
	}
}

// Scan evaluates one message; userID is the scanned chat's owner (empty when
// unattributed), threaded onto the judge's completion telemetry.
func (s *Scanner) Scan(ctx context.Context, orgID, projectID, userID, prompt string, cfg Config, msg judgemessage.Message) scanners.Result {
	result, _ := s.ScanWithVerdict(ctx, orgID, projectID, userID, prompt, cfg, msg)
	return result
}

// ScanWithVerdict preserves model/provider attribution for the executor that
// emits usage while keeping fail-mode findings independent from completion.
func (s *Scanner) ScanWithVerdict(ctx context.Context, orgID, projectID, userID, prompt string, cfg Config, msg judgemessage.Message) (scanners.Result, *Verdict) {
	if s == nil || s.evaluate == nil || strings.TrimSpace(prompt) == "" {
		return scanners.Result{Findings: FindingsFromEvaluation(cfg, nil, nil, true), STokens: 0, Completed: false}, nil
	}

	verdict, err := s.evaluate(ctx, Input{
		OrgID:     orgID,
		ProjectID: projectID,
		UserID:    userID,
		Prompt:    prompt,
		Message:   msg,
		Config:    cfg,
	})
	if err != nil && cfg.FailOpen && s.logger != nil {
		s.logger.WarnContext(ctx, "prompt policy judge failed; returning no findings",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
	}
	result := scanners.Result{Findings: FindingsFromEvaluation(cfg, verdict, err, false), STokens: 0, Completed: false}
	if err == nil && verdict != nil && verdict.Completed {
		result.STokens = verdict.STokens
		result.Completed = true
	}
	return result, verdict
}

func FindingsFromEvaluation(cfg Config, verdict *Verdict, err error, judgeUnavailable bool) []scanners.Finding {
	if err != nil || judgeUnavailable {
		if cfg.FailOpen {
			return []scanners.Finding{}
		}
		return []scanners.Finding{NewFinding(FailClosedVerdict(err))}
	}
	if verdict == nil || !verdict.Matched {
		return []scanners.Finding{}
	}
	return []scanners.Finding{NewFinding(*verdict)}
}
