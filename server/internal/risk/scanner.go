package risk

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskScanner checks text against blocking risk policies.
type RiskScanner interface {
	// ScanForEnforcement scans text against all enabled blocking policies
	// for the given project. Returns nil if no blocking policy matches.
	ScanForEnforcement(ctx context.Context, projectID uuid.UUID, text string) (*ScanResult, error)
}

// ScanResult describes a match from a blocking risk policy.
type ScanResult struct {
	Action      string // "block"
	PolicyID    string
	PolicyName  string
	Source      string // "gitleaks" or "presidio"
	RuleID      string
	Description string
	Match       string
}

// Scanner implements RiskScanner using gitleaks and optionally Presidio.
type Scanner struct {
	logger     *slog.Logger
	repo       *repo.Queries
	gitleaks   *ra.Scanner
	piiScanner ra.PIIScanner // nil if Presidio is unavailable
}

// NewScanner creates a RiskScanner. piiScanner may be nil if Presidio
// is not available in the server process.
func NewScanner(logger *slog.Logger, db *pgxpool.Pool, piiScanner ra.PIIScanner) *Scanner {
	return &Scanner{
		logger:     logger.With(attr.SlogComponent("risk-scanner")),
		repo:       repo.New(db),
		gitleaks:   ra.NewScanner(),
		piiScanner: piiScanner,
	}
}

func (s *Scanner) ScanForEnforcement(ctx context.Context, projectID uuid.UUID, text string) (*ScanResult, error) {
	if text == "" {
		return nil, nil
	}

	policies, err := s.repo.ListEnabledEnforcingPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list enforcing policies: %w", err)
	}
	if len(policies) == 0 {
		return nil, nil
	}

	for _, p := range policies {
		result, err := s.scanPolicy(ctx, p, text)
		if err != nil {
			s.logger.WarnContext(ctx, "scan failed for block policy",
				attr.SlogError(err),
				attr.SlogRiskPolicyID(p.ID.String()),
			)
			continue
		}
		if result != nil {
			return result, nil
		}
	}

	return nil, nil
}

func (s *Scanner) scanPolicy(ctx context.Context, policy repo.RiskPolicy, text string) (*ScanResult, error) {
	for _, source := range policy.Sources {
		switch source {
		case "gitleaks":
			findings, err := s.gitleaks.Scan(text)
			if err != nil {
				return nil, fmt.Errorf("gitleaks scan: %w", err)
			}
			if len(findings) > 0 {
				return &ScanResult{
					Action:      policy.Action,
					PolicyID:    policy.ID.String(),
					PolicyName:  policy.Name,
					Source:      "gitleaks",
					RuleID:      findings[0].RuleID,
					Description: findings[0].Description,
					Match:       findings[0].Match,
				}, nil
			}

		case "presidio":
			if s.piiScanner == nil {
				continue
			}
			batchResults, err := s.piiScanner.AnalyzeBatch(ctx, []string{text}, policy.PresidioEntities, func() {})
			if err != nil {
				return nil, fmt.Errorf("presidio scan: %w", err)
			}
			if len(batchResults) > 0 && len(batchResults[0]) > 0 {
				f := batchResults[0][0]
				return &ScanResult{
					Action:      policy.Action,
					PolicyID:    policy.ID.String(),
					PolicyName:  policy.Name,
					Source:      "presidio",
					RuleID:      f.RuleID,
					Description: f.Description,
					Match:       f.Match,
				}, nil
			}
		}
	}
	return nil, nil
}
