package risk

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zricethezav/gitleaks/v8/detect"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/o11y"
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

type scannerMetrics struct {
	scanDuration metric.Float64Histogram
	scanResults  metric.Int64Counter
}

func newScannerMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *scannerMetrics {
	ctx := context.Background()
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/risk/scanner")

	scanDuration, err := meter.Float64Histogram(
		"risk.enforcement.scan_duration",
		metric.WithDescription("Duration of real-time risk enforcement scans in seconds"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName("risk.enforcement.scan_duration"), attr.SlogError(err))
	}

	scanResults, err := meter.Int64Counter(
		"risk.enforcement.scan_results",
		metric.WithDescription("Total real-time enforcement scan results by outcome (allowed, blocked, error, skipped)"),
		metric.WithUnit("{scan}"),
	)
	if err != nil {
		logger.ErrorContext(ctx, "create metric", attr.SlogMetricName("risk.enforcement.scan_results"), attr.SlogError(err))
	}

	return &scannerMetrics{
		scanDuration: scanDuration,
		scanResults:  scanResults,
	}
}

// Scanner implements RiskScanner using gitleaks and optionally Presidio.
// It pre-creates a gitleaks detector at construction time to avoid the
// per-scan mutex+init overhead on the hot path.
type Scanner struct {
	logger     *slog.Logger
	repo       *repo.Queries
	gitleaks   *ra.Scanner
	gitleaksMu sync.Mutex         // DetectString is not concurrent-safe
	detector   *detect.Detector   // pre-created, reused across scans
	piiScanner ra.PIIScanner      // nil if Presidio is unavailable
	metrics    *scannerMetrics
}

// NewScanner creates a RiskScanner. piiScanner may be nil if Presidio
// is not available in the server process.
func NewScanner(logger *slog.Logger, db *pgxpool.Pool, piiScanner ra.PIIScanner, meterProvider metric.MeterProvider) *Scanner {
	scanner := ra.NewScanner()

	// Pre-create a gitleaks detector to avoid per-scan init overhead.
	// Scan() creates a new detector each call (with a global mutex),
	// which is fine for batch workers but adds unnecessary latency on
	// the real-time hook path.
	det, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		logger.Error("failed to pre-create gitleaks detector, will fall back to per-scan creation", attr.SlogError(err))
	}

	return &Scanner{
		logger:     logger.With(attr.SlogComponent("risk-scanner")),
		repo:       repo.New(db),
		gitleaks:   scanner,
		detector:   det,
		piiScanner: piiScanner,
		metrics:    newScannerMetrics(meterProvider, logger),
	}
}

func (s *Scanner) ScanForEnforcement(ctx context.Context, projectID uuid.UUID, text string) (*ScanResult, error) {
	if text == "" {
		return nil, nil
	}

	start := time.Now()

	policies, err := s.repo.ListEnabledEnforcingPoliciesByProject(ctx, projectID)
	if err != nil {
		s.recordScan(ctx, projectID.String(), o11y.OutcomeFailure, time.Since(start))
		return nil, fmt.Errorf("list enforcing policies: %w", err)
	}
	if len(policies) == 0 {
		// No enforcing policies, fast path. Record as "skipped" to track volume.
		s.recordScan(ctx, projectID.String(), "skipped", time.Since(start))
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
			s.recordScan(ctx, projectID.String(), "blocked", time.Since(start))
			return result, nil
		}
	}

	s.recordScan(ctx, projectID.String(), o11y.OutcomeSuccess, time.Since(start))
	return nil, nil
}

// recordScan records scan metrics. Uses non-blocking OTEL atomic operations.
func (s *Scanner) recordScan(ctx context.Context, projectID string, outcome o11y.Outcome, duration time.Duration) {
	attrs := metric.WithAttributes(
		attr.ProjectID(projectID),
		attr.Outcome(outcome),
	)
	if s.metrics.scanDuration != nil {
		s.metrics.scanDuration.Record(ctx, duration.Seconds(), attrs)
	}
	if s.metrics.scanResults != nil {
		s.metrics.scanResults.Add(ctx, 1, attrs)
	}
}

func (s *Scanner) scanPolicy(ctx context.Context, policy repo.RiskPolicy, text string) (*ScanResult, error) {
	for _, source := range policy.Sources {
		switch source {
		case "gitleaks":
			findings, err := s.scanGitleaks(text)
			if err != nil {
				return nil, fmt.Errorf("gitleaks scan: %w", err)
			}
			if len(findings) > 0 {
				return &ScanResult{
					Action:      actionOrDefault(policy.Action),
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
					Action:      actionOrDefault(policy.Action),
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

// scanGitleaks uses the pre-created detector if available, falling back to
// Scanner.Scan() (which creates a new detector per call) if construction failed.
func (s *Scanner) scanGitleaks(text string) ([]ra.Finding, error) {
	if s.detector == nil {
		return s.gitleaks.Scan(text)
	}
	s.gitleaksMu.Lock()
	raw := s.detector.DetectString(text)
	s.gitleaksMu.Unlock()
	return ra.ConvertFindings(text, raw), nil
}
