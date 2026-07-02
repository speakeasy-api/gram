package risk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/celenv"
	"github.com/speakeasy-api/gram/server/internal/risk/customrules"
	"github.com/speakeasy-api/gram/server/internal/risk/policyflags"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// RiskScanner checks text against blocking risk policies.
type RiskScanner interface {
	// ScanForEnforcement scans text against enabled blocking policies that
	// apply to the given user. Everyone-audience policies always apply;
	// targeted policies require a matching risk_policy:evaluate grant.
	// toolName is the tool-call name for tool_request/tool_response messages
	// ("" otherwise); it is surfaced to prompt-based policies.
	ScanForEnforcement(ctx context.Context, organizationID string, projectID uuid.UUID, userID string, text string, messageType message.Type, toolName string) (*ScanResult, error)
	// LookupShadowMCPBlockingPolicy returns the first enabled shadow-MCP
	// policy that applies to the given user. Returns nil when no such policy
	// exists. Used by hooks to gate the realtime deny path.
	LookupShadowMCPBlockingPolicy(ctx context.Context, organizationID string, projectID uuid.UUID, userID string) (*ShadowMCPPolicy, error)
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
	Version     int64
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
	Source      string
	MessageType message.Type
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
	tracer     trace.Tracer
	db         *pgxpool.Pool
	repo       *repo.Queries
	gitleaks   *ra.GitleaksScanner        // pre-created, reused & serialized across scans
	piiScanner ra.PIIScanner              // nil if Presidio is unavailable
	piScanner  *ra.PromptInjectionScanner // never nil; stub-classifier when L1 disabled
	judge      ra.PromptJudge             // nil-safe; guarded at the call site
	flags      feature.Provider           // nil disables prompt_based enforcement
	metrics    *scannerMetrics
	celEng     *celenv.Engine
}

// NewScanner creates a RiskScanner. piiScanner may be nil if Presidio
// is not available in the server process. piScanner must be non-nil; pass a
// scanner built with a nil engine to run L0 heuristics only.
// Pre-creates a gitleaks detector to avoid per-scan rule compilation on the
// real-time hook path; returns an error if the detector cannot be built
// (init relies on viper global state and should never realistically fail,
// but propagating the error keeps startup honest).
func NewScanner(logger *slog.Logger, db *pgxpool.Pool, piiScanner ra.PIIScanner, piScanner *ra.PromptInjectionScanner, judge ra.PromptJudge, flags feature.Provider, tracerProvider trace.TracerProvider, meterProvider metric.MeterProvider, celEng *celenv.Engine) (*Scanner, error) {
	gitleaksScanner, err := ra.NewGitleaksScanner()
	if err != nil {
		return nil, fmt.Errorf("create gitleaks scanner: %w", err)
	}

	if piScanner == nil {
		piScanner = ra.NewPromptInjectionScanner(logger, nil)
	}

	return &Scanner{
		logger:     logger.With(attr.SlogComponent("risk-scanner")),
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/risk"),
		db:         db,
		repo:       repo.New(db),
		gitleaks:   gitleaksScanner,
		piiScanner: piiScanner,
		piScanner:  piScanner,
		judge:      judge,
		flags:      flags,
		metrics:    newScannerMetrics(meterProvider, logger),
		celEng:     celEng,
	}, nil
}

func (s *Scanner) ScanForEnforcement(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	userID string,
	text string,
	messageType message.Type,
	toolName string,
) (result *ScanResult, retErr error) {
	// An empty body is only a no-op when there is also no tool attribution: a
	// no-arg/no-output tool call still names a tool (+ MCP server/function) that
	// a tool-scoped prompt policy can match, so let those events through.
	if text == "" && toolName == "" {
		return nil, nil
	}

	// Root span for the scan as a unit of work: gitleaks/presidio/judge spans
	// spawned downstream (through gctx) attribute under this span and its
	// per-policy children instead of dangling as siblings of the RPC span.
	ctx, span := s.tracer.Start(ctx, "risk.scanForEnforcement", trace.WithAttributes(
		attr.OrganizationID(organizationID),
		attr.ProjectID(projectID.String()),
		attr.RiskMessageType(string(messageType)),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	start := time.Now()

	policies, err := s.repo.ListEnabledEnforcingPoliciesByProject(ctx, projectID)
	if err != nil {
		s.recordScan(ctx, projectID.String(), o11y.OutcomeFailure, time.Since(start))
		return nil, fmt.Errorf("list enforcing policies: %w", err)
	}
	span.SetAttributes(attr.RiskPolicyCount(len(policies)))
	if len(policies) == 0 {
		// No enforcing policies, fast path. Record as "skipped" to track volume.
		s.recordScan(ctx, projectID.String(), "skipped", time.Since(start))
		return nil, nil
	}

	grants, err := s.riskPolicyGrants(ctx, organizationID, userID)
	if err != nil {
		s.recordScan(ctx, projectID.String(), o11y.OutcomeFailure, time.Since(start))
		return nil, err
	}

	// Resolve the prompt-policy flag once per scan (on the parent ctx, before
	// fan-out) so prompt_based policies don't each repeat the slug lookup and
	// so the lookup is never cancelled by a sibling match. Gated on the exact
	// condition under which the fan-out would run the judge — a prompt_based
	// policy whose message_types apply to this message — so the lookup is
	// skipped entirely for scans that can never enforce one. message_types gates
	// candidacy; scope_include narrows further per-message in scanPolicy.
	inMessageScope := func(p repo.RiskPolicy) bool {
		return len(p.MessageTypes) == 0 ||
			slices.Contains(p.MessageTypes, messageType)
	}

	promptPoliciesOn := false
	if slices.ContainsFunc(policies, func(p repo.RiskPolicy) bool {
		return p.PolicyType == ra.PolicyTypePromptBased && inMessageScope(p)
	}) {
		// All enforcing policies for a project belong to the same org.
		promptPoliciesOn = s.projectFlagEnabled(ctx, policies[0].OrganizationID, projectID, feature.FlagPromptPolicies)
	}

	// Same once-per-scan resolution for the L1 prompt-injection engine: gate on
	// a standard policy whose prompt_injection source applies to this message,
	// so the slug/flag lookup is skipped for scans that can never run L1.
	piEngineOn := false
	if slices.ContainsFunc(policies, func(p repo.RiskPolicy) bool {
		return p.PolicyType != ra.PolicyTypePromptBased &&
			slices.Contains(p.Sources, ra.SourcePromptInjection) &&
			(len(p.MessageTypes) == 0 || slices.Contains(p.MessageTypes, messageType))
	}) {
		piEngineOn = s.projectFlagEnabled(ctx, policies[0].OrganizationID, projectID, feature.FlagPromptInjectionUseClassifier)
	}

	// Fan out across policies. The first goroutine that finds a match returns
	// errMatchFound, which causes errgroup to cancel its context — sibling
	// goroutines stop their in-flight Presidio HTTP calls early instead of
	// finishing uselessly. Gitleaks scans serialize inside s.gitleaks (the v8
	// detector is not concurrent-safe); the real win is Presidio fan-out.
	var (
		winner   atomic.Pointer[ScanResult]
		matchErr = errors.New("risk policy match")
	)
	g, gctx := errgroup.WithContext(ctx)
	for _, p := range policies {
		policyApplication, err := authz.RiskPolicyApplies(p.ID.String(), authz.RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Evaluate(grants)
		if err != nil {
			s.recordScan(ctx, projectID.String(), o11y.OutcomeFailure, time.Since(start))
			return nil, fmt.Errorf("evaluate risk policy application: %w", err)
		}
		if !policyApplication.Satisfied {
			continue
		}
		if !inMessageScope(p) {
			continue
		}

		g.Go(func() error {
			result, scanErr := s.scanPolicy(gctx, p, text, messageType, toolName, promptPoliciesOn, piEngineOn)
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
func (s *Scanner) LookupShadowMCPBlockingPolicy(ctx context.Context, organizationID string, projectID uuid.UUID, userID string) (*ShadowMCPPolicy, error) {
	grants, err := s.riskPolicyGrants(ctx, organizationID, userID)
	if err != nil {
		return nil, err
	}

	policies, err := s.repo.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list shadow_mcp policies: %w", err)
	}
	for _, p := range policies {
		policyApplication, err := authz.RiskPolicyApplies(p.ID.String(), authz.RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Evaluate(grants)
		if err != nil {
			return nil, fmt.Errorf("evaluate risk policy application: %w", err)
		}
		if !policyApplication.Satisfied {
			continue
		}
		if p.Action == "block" {
			return &ShadowMCPPolicy{
				ID:          p.ID.String(),
				Name:        p.Name,
				Version:     p.Version,
				UserMessage: conv.FromPGText[string](p.UserMessage),
			}, nil
		}
	}
	return nil, nil
}

func (s *Scanner) riskPolicyGrants(ctx context.Context, organizationID string, userID string) ([]authz.Grant, error) {
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve risk policy audience principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, s.db, organizationID, principals)
	if err != nil {
		return nil, fmt.Errorf("load risk policy audience grants: %w", err)
	}
	return grants, nil
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
func (s *Scanner) scanPolicy(ctx context.Context, policy repo.RiskPolicy, text string, messageType message.Type, toolName string, promptPoliciesOn bool, piEngineOn bool) (result *ScanResult, retErr error) {
	// Per-policy child span so an individual gitleaks/presidio/judge span
	// attributes to the policy that spawned it (the g.Go fan-out threads gctx
	// here, so this span parents under risk.scanForEnforcement).
	ctx, span := s.tracer.Start(ctx, "risk.scanPolicy", trace.WithAttributes(
		attr.RiskPolicyID(policy.ID.String()),
		attr.RiskPolicyName(policy.Name),
		attr.RiskPolicyType(policy.PolicyType),
	))
	defer func() {
		if retErr != nil {
			span.SetStatus(codes.Error, retErr.Error())
		}
		span.End()
	}()

	// Build the structured view once; the application predicates and custom
	// rules both evaluate against it.
	view := ra.MessageView{Content: text, Type: messageType, Tools: []ra.ToolView{}}
	if messageType == message.ToolRequest && toolName != "" {
		// In realtime a tool-request's text carries the call arguments (the same
		// body the judge sees), so it doubles as the tool_args source.
		view.Tools = []ra.ToolView{ra.NewToolView(toolName, text)}
	}

	// Policy application gates detection: include narrows scope (alongside
	// message_types); exempt takes the message out of the policy.
	eng := s.celEng
	app, err := ra.CompileScope(eng, policy.ScopeInclude.String, policy.ScopeExempt.String)
	if err != nil {
		return nil, fmt.Errorf("compile policy scope: %w", err)
	}
	if !app.Includes(view) || app.Exempts(view) {
		return nil, nil
	}

	if policy.PolicyType == ra.PolicyTypePromptBased {
		return s.scanPromptPolicy(ctx, policy, text, messageType, toolName, promptPoliciesOn), nil
	}

	disabled := ra.NewDisabledRuleSet(policy.DisabledRules)

	// Suppress findings matched by an exclusion (the policy's own plus any
	// global ones) so a false positive never blocks in real time.
	exclusionRows, err := s.repo.ListEnabledExclusionsForPolicy(ctx, repo.ListEnabledExclusionsForPolicyParams{
		ProjectID:    policy.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: policy.ID, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("list exclusions: %w", err)
	}
	exclusions := ra.NewExclusionSet(exclusionRows)
	filter := func(findings []ra.Finding) []ra.Finding {
		return exclusions.FilterFindings(disabled.FilterFindings(findings))
	}

	// Evaluate custom detection rules up front; their findings are held for the
	// block check after the built-in sources. Message exemptions were already
	// applied above via the policy's scope_exempt.
	var customFindings []ra.Finding
	if len(policy.CustomRuleIds) > 0 {
		customFindings, err = s.scanCustomRules(ctx, policy, view)
		if err != nil {
			// A broken custom rule must not disable the built-in detectors (a
			// fail-open bypass); drop its findings and keep scanning.
			s.logger.ErrorContext(ctx, "custom detection rules failed; continuing with built-in sources", attr.SlogError(err))
			customFindings = nil
		}
	}

	for _, source := range policy.Sources {
		switch source {
		case ra.SourceGitleaks:
			findings := filter(s.scanGitleaks(text))
			if len(findings) > 0 {
				return &ScanResult{
					Action:      policy.Action,
					PolicyID:    policy.ID.String(),
					PolicyName:  policy.Name,
					Source:      ra.SourceGitleaks,
					MessageType: messageType,
					RuleID:      findings[0].RuleID,
					Description: findings[0].Description,
					UserMessage: conv.FromPGText[string](policy.UserMessage),
				}, nil
			}
		case ra.SourcePresidio:
			if s.piiScanner == nil {
				continue
			}
			batchResults, err := s.piiScanner.AnalyzeBatch(
				ctx,
				[]string{text},
				policy.PresidioEntities,
				ra.PresidioScoreThresholdFromConfig(policy.AnalyzerConfig),
				func() {},
			)
			if err != nil {
				return nil, fmt.Errorf("presidio scan: %w", err)
			}
			if len(batchResults) > 0 {
				filtered := filter(batchResults[0])
				if len(filtered) > 0 {
					f := filtered[0]
					return &ScanResult{
						Action:      policy.Action,
						PolicyID:    policy.ID.String(),
						PolicyName:  policy.Name,
						Source:      ra.SourcePresidio,
						MessageType: messageType,
						RuleID:      f.RuleID,
						Description: f.Description,
						UserMessage: conv.FromPGText[string](policy.UserMessage),
					}, nil
				}
			}
		case ra.SourcePromptInjection:
			findings, err := s.piScanner.Scan(ctx, text, policy.OrganizationID, policy.ProjectID.String(), ra.NewJudgeMessage(messageType, toolName, text), piEngineOn)
			if err != nil {
				return nil, fmt.Errorf("prompt injection scan: %w", err)
			}
			findings = filter(findings)
			if len(findings) > 0 {
				return &ScanResult{
					Action:      policy.Action,
					PolicyID:    policy.ID.String(),
					PolicyName:  policy.Name,
					Source:      ra.SourcePromptInjection,
					MessageType: messageType,
					RuleID:      findings[0].RuleID,
					Description: findings[0].Description,
					UserMessage: conv.FromPGText[string](policy.UserMessage),
				}, nil
			}
		}
	}
	if denyFindings := filter(customFindings); len(denyFindings) > 0 {
		return &ScanResult{
			Action:      policy.Action,
			PolicyID:    policy.ID.String(),
			PolicyName:  policy.Name,
			Source:      ra.SourceCustom,
			MessageType: messageType,
			RuleID:      denyFindings[0].RuleID,
			Description: denyFindings[0].Description,
			UserMessage: conv.FromPGText[string](policy.UserMessage),
		}, nil
	}
	return nil, nil
}

// scanPromptPolicy evaluates text against a prompt_based policy's guardrail
// prompt via the LLM judge. The caller (ScanForEnforcement) has already
// filtered policies to those whose message_types apply to this message, so the
// judge runs for whatever message types the policy declares. Returns nil when
// the judge does not match (including fail-open on judge error).
func (s *Scanner) scanPromptPolicy(ctx context.Context, policy repo.RiskPolicy, text string, messageType message.Type, toolName string, promptPoliciesOn bool) *ScanResult {
	cfg := ra.ParseJudgeConfig(policy.ModelConfig)
	if !promptPoliciesOn {
		return nil
	}
	if s.judge == nil || !policy.Prompt.Valid || strings.TrimSpace(policy.Prompt.String) == "" {
		return promptPolicyUnavailableResult(policy, messageType, cfg)
	}

	verdict := s.judge.Evaluate(ctx, ra.JudgeInput{
		OrgID:     policy.OrganizationID,
		ProjectID: policy.ProjectID.String(),
		Prompt:    policy.Prompt.String,
		// text is the type-appropriate body the hook layer already flattened:
		// the prompt for user messages, tool-input JSON for tool_request,
		// tool-output JSON for tool_response.
		Message: ra.NewJudgeMessage(messageType, toolName, text),
		Config:  cfg,
	})
	if verdict == nil {
		return nil
	}

	finding := ra.JudgeFinding(*verdict)
	return &ScanResult{
		Action:      policy.Action,
		PolicyID:    policy.ID.String(),
		PolicyName:  policy.Name,
		Source:      ra.SourceLLMJudge,
		MessageType: messageType,
		RuleID:      finding.RuleID,
		Description: finding.Description,
		UserMessage: conv.FromPGText[string](policy.UserMessage),
	}
}

func (s *Scanner) projectFlagEnabled(ctx context.Context, orgID string, projectID uuid.UUID, flag feature.Flag) bool {
	return policyflags.ProjectFlagEnabled(ctx, s.logger, s.repo, s.flags, orgID, projectID, flag)
}

func promptPolicyUnavailableResult(policy repo.RiskPolicy, messageType message.Type, cfg ra.JudgeConfig) *ScanResult {
	if cfg.FailOpen {
		return nil
	}
	return &ScanResult{
		Action:      policy.Action,
		PolicyID:    policy.ID.String(),
		PolicyName:  policy.Name,
		Source:      ra.SourceLLMJudge,
		MessageType: messageType,
		RuleID:      ra.RuleLLMJudge,
		Description: "Policy judge was unavailable; flagged by fail-closed policy.",
		UserMessage: conv.FromPGText[string](policy.UserMessage),
	}
}

func (s *Scanner) scanCustomRules(ctx context.Context, policy repo.RiskPolicy, view ra.MessageView) ([]ra.Finding, error) {
	if len(policy.CustomRuleIds) == 0 {
		return []ra.Finding{}, nil
	}

	rules, err := customrules.LoadSelected(ctx, s.repo, policy.ProjectID, policy.CustomRuleIds)
	if err != nil {
		return nil, fmt.Errorf("load custom detection rules: %w", err)
	}
	compiled, err := ra.CompileCELRules(s.celEng, rules)
	if err != nil {
		return nil, fmt.Errorf("compile custom detection rules: %w", err)
	}
	findings, err := ra.ScanCELRules(s.celEng, view, compiled)
	if err != nil {
		return nil, fmt.Errorf("scan custom detection rules: %w", err)
	}
	return findings, nil
}

// scanGitleaks scans text on the pre-created, reused gitleaks scanner. The
// scanner reuses one detector (avoiding per-scan rule compilation) and
// serializes the underlying DetectString call, which mutates detector state.
func (s *Scanner) scanGitleaks(text string) []ra.Finding {
	return s.gitleaks.Scan(text)
}
