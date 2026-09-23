package mcpriskscan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// FailMode determines the request-path result of an indeterminate block scan.
type FailMode string

const (
	// FailOpen allows requests when block policy evaluation is indeterminate.
	FailOpen FailMode = "open"

	// FailClosed denies requests when block policy evaluation is indeterminate.
	FailClosed FailMode = "closed"
)

// PolicyConfig controls bounded MCP policy evaluation.
type PolicyConfig struct {
	// Deadline bounds lookup, block scanning, and each detached flag lane.
	Deadline time.Duration

	// FailMode resolves incomplete block policy evaluation.
	FailMode FailMode

	// FlagConcurrency bounds detached flag evaluation goroutines.
	FlagConcurrency int
}

// DefaultPolicyConfig is the process-level MCP enforcement configuration.
var DefaultPolicyConfig = PolicyConfig{
	Deadline:        2 * time.Second,
	FailMode:        FailOpen,
	FlagConcurrency: 8,
}

// PolicyLookup resolves enabled policies for one concrete MCP subject.
type PolicyLookup interface {
	ListEnabledForMCPServer(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, toolName string) ([]policycore.Policy, error)
}

// PolicyDetector runs one policy through the shared synchronous detector set.
type PolicyDetector interface {
	ScanMCPPolicy(ctx context.Context, policy policycore.Policy, request risk.MCPScanRequest) ([]scanners.Finding, error)
}

type policyEvaluator struct {
	logger     *slog.Logger
	lookup     PolicyLookup
	detector   PolicyDetector
	publisher  gcp.Publisher[*riskv1.Finding]
	config     PolicyConfig
	flagSlots  chan struct{}
	onFlagDrop func(context.Context, Event)
}

// NewPolicyEvaluator creates an evaluator that enforces block policies inline
// and evaluates every other policy in the bounded flag lane.
func NewPolicyEvaluator(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	lookup PolicyLookup,
	detector PolicyDetector,
	publisher gcp.Publisher[*riskv1.Finding],
	config PolicyConfig,
) *Evaluator {
	if config.Deadline <= 0 {
		config.Deadline = DefaultPolicyConfig.Deadline
	}
	if config.FailMode == "" {
		config.FailMode = DefaultPolicyConfig.FailMode
	}
	if config.FlagConcurrency <= 0 {
		config.FlagConcurrency = DefaultPolicyConfig.FlagConcurrency
	}
	evaluator := newInstrumentedEvaluator(nil, tracerProvider, meterProvider, logger)
	policy := &policyEvaluator{
		logger:     logger,
		lookup:     lookup,
		detector:   detector,
		publisher:  publisher,
		config:     config,
		flagSlots:  make(chan struct{}, config.FlagConcurrency),
		onFlagDrop: nil,
	}
	policy.onFlagDrop = evaluator.metrics.recordFlagDrop
	evaluator.policy = policy
	return evaluator
}

func (p *policyEvaluator) evaluate(ctx context.Context, subject Subject) Decision {
	event := subject.Event
	projectID, err := uuid.Parse(event.ProjectID)
	if err != nil {
		return p.resolveIndeterminate(ctx, fmt.Errorf("parse project id: %w", err))
	}
	serverID := uuid.Nil
	if event.ServerID != "" {
		serverID, err = uuid.Parse(event.ServerID)
		if err != nil {
			return p.resolveIndeterminate(ctx, fmt.Errorf("parse MCP server id: %w", err))
		}
	}

	scanCtx, cancel := context.WithTimeout(ctx, p.config.Deadline)
	defer cancel()
	policies, err := p.lookup.ListEnabledForMCPServer(scanCtx, event.OrganizationID, projectID, serverID, event.ToolName)
	if err != nil {
		return p.resolveIndeterminate(ctx, fmt.Errorf("list MCP policies: %w", err))
	}
	if len(policies) == 0 {
		return Allow()
	}

	blockPolicies, flagPolicies := partitionPolicies(policies)
	defer p.scheduleFlagLane(ctx, subject, flagPolicies)
	if subject.Payload.Availability() != PayloadAvailable {
		return p.resolveIndeterminate(ctx, fmt.Errorf("MCP payload is %s", subject.Payload.Availability()))
	}

	request := policyScanRequest(subject, subject.Payload.Bytes())
	var scanErr error
	for _, policy := range blockPolicies {
		findings, err := p.detector.ScanMCPPolicy(scanCtx, policy, request)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("scan policy %s: %w", policy.ID, err))
		}
		if len(findings) == 0 {
			continue
		}
		finding := findings[0]
		p.publish(scanCtx, subject.Event, policy, findings, riskv1.Finding_ENFORCEMENT_OUTCOME_DENIED)
		return deniedDecision(policy, finding)
	}
	if scanErr != nil || scanCtx.Err() != nil {
		return p.resolveIndeterminate(ctx, errors.Join(scanErr, scanCtx.Err()))
	}
	return Allow()
}

func (p *policyEvaluator) scheduleFlagLane(parent context.Context, subject Subject, policies []policycore.Policy) {
	if len(policies) == 0 || subject.Payload.Availability() != PayloadAvailable {
		return
	}
	select {
	case p.flagSlots <- struct{}{}:
	default:
		if p.onFlagDrop != nil {
			p.onFlagDrop(parent, subject.Event)
		}
		return
	}
	payload := bytes.Clone(subject.Payload.Bytes())
	go func() {
		defer func() { <-p.flagSlots }()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), p.config.Deadline)
		defer cancel()
		request := policyScanRequest(subject, payload)
		for _, policy := range policies {
			findings, err := p.detector.ScanMCPPolicy(ctx, policy, request)
			if err != nil {
				p.logger.WarnContext(ctx, "MCP flag policy scan failed", attr.SlogRiskPolicyID(policy.ID.String()), attr.SlogError(err))
				continue
			}
			if len(findings) > 0 {
				p.publish(ctx, subject.Event, policy, findings, riskv1.Finding_ENFORCEMENT_OUTCOME_LOGGED)
			}
		}
	}()
}

func (p *policyEvaluator) publish(ctx context.Context, event Event, policy policycore.Policy, findings []scanners.Finding, outcome riskv1.Finding_EnforcementOutcome) {
	if p.publisher == nil {
		p.logger.WarnContext(ctx, "MCP policy findings publisher is unavailable", attr.SlogRiskPolicyID(policy.ID.String()))
		return
	}
	principal := event.Principal()
	attribution := riskv1.Finding_Attribution_builder{
		ChatId:           conv.PtrEmpty(event.ChatID),
		UserId:           conv.PtrEmpty(principal.UserID()),
		ExternalUserId:   nil,
		AssistantId:      conv.PtrEmpty(principal.AgentID()),
		MessageCreatedAt: nil,
		ChatSource:       nil,
		Team:             nil,
		UserEmail:        nil,
	}.Build()
	identityStamped := event.IdentityStamped()
	principalKind := string(principal.Kind())
	execution := riskv1.Finding_Execution_builder{
		ExecutionId:      &event.executionID,
		McpServerId:      &event.ServerID,
		MetaMcpServerId:  &event.MetaServerID,
		ToolsetId:        &event.ToolsetID,
		ToolName:         &event.ToolName,
		Phase:            &event.phase,
		MediationSurface: &event.Surface,
		Method:           &event.Method,
		PrincipalKind:    &principalKind,
		IdentityStamped:  &identityStamped,
	}.Build()
	_, _, err := scanners.PublishFindings(ctx, p.logger, p.publisher, scanners.FindingMetadata{
		RequestID:         event.executionID,
		ChatMessageID:     "",
		ContentPartID:     "",
		ProjectID:         event.ProjectID,
		OrganizationID:    event.OrganizationID,
		RiskPolicyID:      policy.ID.String(),
		RiskPolicyVersion: policy.Version,
		Shadow:            false,
	}, findings, "MCP policy", scanners.WithFindingMCPContext(attribution, execution, outcome))
	if err != nil {
		p.logger.WarnContext(ctx, "failed to publish MCP policy findings", attr.SlogRiskPolicyID(policy.ID.String()), attr.SlogError(err))
	}
}

func (p *policyEvaluator) resolveIndeterminate(ctx context.Context, err error) Decision {
	p.logger.WarnContext(ctx, "MCP block policy evaluation was indeterminate", attr.SlogError(err), attr.SlogRiskEnforcementFailMode(string(p.config.FailMode)))
	if p.config.FailMode == FailClosed {
		return Decision{
			Disposition:   DispositionDeny,
			PolicyID:      "",
			PolicyName:    "",
			RuleID:        "",
			Description:   "MCP risk policy evaluation did not complete",
			UserMessage:   "This MCP request was blocked because its risk policy evaluation did not complete.",
			Indeterminate: true,
		}
	}
	decision := Allow()
	decision.Indeterminate = true
	return decision
}

func partitionPolicies(policies []policycore.Policy) (block, flag []policycore.Policy) {
	block = make([]policycore.Policy, 0, len(policies))
	flag = make([]policycore.Policy, 0, len(policies))
	for _, policy := range policies {
		if policy.Action == "block" {
			block = append(block, policy)
			continue
		}
		flag = append(flag, policy)
	}
	return block, flag
}

func deniedDecision(policy policycore.Policy, finding scanners.Finding) Decision {
	userMessage := fmt.Sprintf("This MCP request was blocked by risk policy %q.", policy.Name)
	if policy.UserMessage != nil && *policy.UserMessage != "" {
		userMessage = *policy.UserMessage
	}
	return Decision{
		Disposition:   DispositionDeny,
		PolicyID:      policy.ID.String(),
		PolicyName:    policy.Name,
		RuleID:        finding.RuleID,
		Description:   finding.Description,
		UserMessage:   userMessage,
		Indeterminate: false,
	}
}

func policyScanRequest(subject Subject, payload []byte) risk.MCPScanRequest {
	principal := subject.Event.Principal()
	return risk.MCPScanRequest{
		Text:      string(payload),
		ToolName:  subject.Event.ToolName,
		ToolsetID: subject.Event.ToolsetID,
		ServerID:  subject.Event.ServerID,
		UserID:    principal.UserID(),
	}
}
