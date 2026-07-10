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
func (s *Scanner) Scan(ctx context.Context, orgID, projectID, userID, prompt string, cfg Config, msg judgemessage.Message) []scanners.Finding {
	if s == nil || s.evaluate == nil || strings.TrimSpace(prompt) == "" {
		return FindingsFromEvaluation(cfg, nil, nil, true)
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
	return FindingsFromEvaluation(cfg, verdict, err, false)
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
