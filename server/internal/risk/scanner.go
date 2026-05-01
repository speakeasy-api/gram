package risk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zricethezav/gitleaks/v8/detect"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskScanner checks text against blocking risk policies.
type RiskScanner interface {
	// ScanForEnforcement scans text against all enabled blocking policies
	// for the given project. Returns nil if no blocking policy matches.
	ScanForEnforcement(ctx context.Context, projectID uuid.UUID, text string) (*ScanResult, error)
	// LookupShadowMCPBlockingPolicy returns the first enabled shadow-MCP
	// policy for the project whose action is "block". Returns nil when no
	// such policy exists. Used by hooks to gate the realtime deny path.
	LookupShadowMCPBlockingPolicy(ctx context.Context, projectID uuid.UUID) (*ShadowMCPPolicy, error)
	// HasEnabledShadowMCPPolicy reports whether the project has at least one
	// enabled shadow-MCP policy (any action). Used by the MCP server to
	// decide whether to inject the x-gram-toolset-id constant into tool
	// schemas.
	HasEnabledShadowMCPPolicy(ctx context.Context, projectID uuid.UUID) (bool, error)
}

// ShadowMCPPolicy is the minimal policy view the hooks layer needs to render
// a deny message that follows the same `matched policy %q (...)` format as
// gitleaks/presidio enforcement.
type ShadowMCPPolicy struct {
	ID          string
	Name        string
	UserMessage *string // nil/empty means "render the default message"
}

// ScanResult describes a match from a blocking risk policy.
//
// We deliberately do not include the raw matched substring (the secret/PII
// itself) so that ScanResult is safe to log, store, or serialize. Block
// messages render PolicyName + Description, never the matched value.
type ScanResult struct {
	Action      string // "block"
	PolicyID    string
	PolicyName  string
	Source      string // "gitleaks" or "presidio"
	RuleID      string
	Description string
	UserMessage *string // optional override for the rendered block message
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

var _ RiskScanner = (*Scanner)(nil)

// Scanner implements RiskScanner using gitleaks and optionally Presidio.
// It pre-creates a gitleaks detector at construction time to avoid the
// per-scan mutex+init overhead on the hot path.
type Scanner struct {
	logger     *slog.Logger
	repo       *repo.Queries
	gitleaksMu sync.Mutex       // DetectString is not concurrent-safe
	detector   *detect.Detector // pre-created, reused across scans
	piiScanner ra.PIIScanner    // nil if Presidio is unavailable
	metrics    *scannerMetrics
}

// NewScanner creates a RiskScanner. piiScanner may be nil if Presidio
// is not available in the server process. Pre-creates a gitleaks detector
// to avoid per-scan rule compilation on the real-time hook path; returns
// an error if the detector cannot be built (init relies on viper global
// state and should never realistically fail, but propagating the error
// keeps startup honest).
func NewScanner(logger *slog.Logger, db *pgxpool.Pool, piiScanner ra.PIIScanner, meterProvider metric.MeterProvider) (*Scanner, error) {
	det, err := ra.SharedDetector()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks detector: %w", err)
	}

	return &Scanner{
		logger:     logger.With(attr.SlogComponent("risk-scanner")),
		repo:       repo.New(db),
		gitleaksMu: sync.Mutex{},
		detector:   det,
		piiScanner: piiScanner,
		metrics:    newScannerMetrics(meterProvider, logger),
	}, nil
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

	// Fan out across policies. The first goroutine that finds a match returns
	// errMatchFound, which causes errgroup to cancel its context — sibling
	// goroutines stop their in-flight Presidio HTTP calls early instead of
	// finishing uselessly. Gitleaks scans serialize on s.gitleaksMu (the v8
	// detector is not concurrent-safe); the real win is Presidio fan-out.
	var (
		winner   atomic.Pointer[ScanResult]
		matchErr = errors.New("risk policy match")
	)
	g, gctx := errgroup.WithContext(ctx)
	for _, p := range policies {
		g.Go(func() error {
			result, scanErr := s.scanPolicy(gctx, p, text)
			if scanErr != nil {
				if errors.Is(scanErr, context.Canceled) {
					return nil
				}
				s.logger.WarnContext(gctx, "scan failed for block policy",
					attr.SlogError(scanErr),
					attr.SlogRiskPolicyID(p.ID.String()),
				)
				return nil
			}
			if result != nil && winner.CompareAndSwap(nil, result) {
				return matchErr
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil && !errors.Is(err, matchErr) {
		s.recordScan(ctx, projectID.String(), o11y.OutcomeFailure, time.Since(start))
		return nil, fmt.Errorf("risk policy fan-out: %w", err)
	}
	if hit := winner.Load(); hit != nil {
		s.recordScan(ctx, projectID.String(), "blocked", time.Since(start))
		return hit, nil
	}

	s.recordScan(ctx, projectID.String(), o11y.OutcomeSuccess, time.Since(start))
	return nil, nil
}

// LookupShadowMCPBlockingPolicy returns the first enabled shadow-MCP policy
// for the project whose action is "block". Flag-action policies surface as
// findings via the batch scanner instead of denying at the hook layer.
func (s *Scanner) LookupShadowMCPBlockingPolicy(ctx context.Context, projectID uuid.UUID) (*ShadowMCPPolicy, error) {
	policies, err := s.repo.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list shadow_mcp policies: %w", err)
	}
	for _, p := range policies {
		if p.Action == "block" {
			return &ShadowMCPPolicy{
				ID:          p.ID.String(),
				Name:        p.Name,
				UserMessage: conv.FromPGText[string](p.UserMessage),
			}, nil
		}
	}
	return nil, nil
}

// HasEnabledShadowMCPPolicy reports whether the project has at least one
// enabled shadow-MCP policy (flag or block). The MCP server uses this to
// decide whether to inject the x-gram-toolset-id constant into tool
// schemas.
func (s *Scanner) HasEnabledShadowMCPPolicy(ctx context.Context, projectID uuid.UUID) (bool, error) {
	policies, err := s.repo.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return false, fmt.Errorf("list shadow_mcp policies: %w", err)
	}
	return len(policies) > 0, nil
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

// scanPolicy runs a policy's sources sequentially. Gitleaks holds a mutex
// (the v8 detector mutates internal state), so concurrent gitleaks calls
// serialize anyway, and PresidioClient.AnalyzeBatch is invoked with a single
// text per call — its internal worker pool only fans out when n > 1, so
// per-policy parallelism over sources buys roughly nothing. The
// across-policies fan-out in ScanForEnforcement is the real win.
func (s *Scanner) scanPolicy(ctx context.Context, policy repo.RiskPolicy, text string) (*ScanResult, error) {
	for _, source := range policy.Sources {
		switch source {
		case "gitleaks":
			findings := s.scanGitleaks(text)
			if len(findings) > 0 {
				return &ScanResult{
					Action:      policy.Action,
					PolicyID:    policy.ID.String(),
					PolicyName:  policy.Name,
					Source:      "gitleaks",
					RuleID:      findings[0].RuleID,
					Description: findings[0].Description,
					UserMessage: conv.FromPGText[string](policy.UserMessage),
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
					UserMessage: conv.FromPGText[string](policy.UserMessage),
				}, nil
			}
		}
	}
	return nil, nil
}

// scanGitleaks runs DetectString on the pre-created detector under
// gitleaksMu. The detector is reused (avoiding per-scan rule compilation)
// but DetectString mutates internal state (rules, line counters, last-finding
// bookkeeping) without synchronization, so calls must serialize.
func (s *Scanner) scanGitleaks(text string) []ra.Finding {
	s.gitleaksMu.Lock()
	raw := s.detector.DetectString(text)
	s.gitleaksMu.Unlock()
	return ra.ConvertFindings(text, raw)
}
