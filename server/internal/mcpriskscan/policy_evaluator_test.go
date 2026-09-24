package mcpriskscan_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type policyLookupFunc func(context.Context, string, uuid.UUID, uuid.UUID, string) ([]policycore.Policy, error)

func (f policyLookupFunc) ListEnabledForMCPServer(ctx context.Context, organizationID string, projectID, serverID uuid.UUID, toolName string) ([]policycore.Policy, error) {
	return f(ctx, organizationID, projectID, serverID, toolName)
}

type policyDetectorFunc func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error)

func (f policyDetectorFunc) ScanMCPPolicy(ctx context.Context, policy policycore.Policy, request risk.MCPScanRequest) ([]scanners.Finding, error) {
	return f(ctx, policy, request)
}

type findingPublisher struct {
	mu       sync.Mutex
	messages []*riskv1.Finding
}

func (p *findingPublisher) Publish(_ context.Context, finding *riskv1.Finding, _ ...gcp.PublishOption) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.messages = append(p.messages, finding)
	return gcp.NewSuccessPublishResult()
}

func (p *findingPublisher) Stop(context.Context) error { return nil }

func (p *findingPublisher) snapshot() []*riskv1.Finding {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*riskv1.Finding(nil), p.messages...)
}

func TestPolicyEvaluator_BlockDecisionPublishesAttributedFinding(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	policyID := uuid.New()
	message := "Use an approved credential."
	publisher := &findingPublisher{}
	evaluator := newPolicyEvaluator(t, policyLookupFunc(func(_ context.Context, organizationID string, gotProjectID, gotServerID uuid.UUID, toolName string) ([]policycore.Policy, error) {
		require.Equal(t, "org-test", organizationID)
		require.Equal(t, projectID, gotProjectID)
		require.Equal(t, serverID, gotServerID)
		require.Equal(t, "lookup", toolName)
		return []policycore.Policy{{ID: policyID, ProjectID: projectID, OrganizationID: organizationID, Name: "Credential policy", Action: "block", UserMessage: &message, Version: 3}}, nil
	}), policyDetectorFunc(func(_ context.Context, policy policycore.Policy, request risk.MCPScanRequest) ([]scanners.Finding, error) {
		require.Equal(t, policyID, policy.ID)
		require.JSONEq(t, `{"token":"secret"}`, request.Text)
		return []scanners.Finding{{RuleID: "secret.token", Description: "Credential detected", Match: "secret", StartPos: 10, EndPos: 16, Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
	}), publisher, mcpriskscan.DefaultPolicyConfig)

	decision := evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{"token":"secret"}`))
	require.True(t, decision.Denied())
	require.Equal(t, policyID.String(), decision.PolicyID)
	require.Equal(t, "Credential policy", decision.PolicyName)
	require.Equal(t, "secret.token", decision.RuleID)
	require.Equal(t, "Credential detected", decision.Description)
	require.Equal(t, message, decision.UserMessage)

	published := publisher.snapshot()
	require.Len(t, published, 1)
	require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_DENIED, published[0].GetEnforcementOutcome())
	require.Equal(t, "chat-test", published[0].GetAttribution().GetChatId())
	require.Equal(t, mcpriskscan.PhaseRequest, published[0].GetExecution().GetPhase())
	require.Equal(t, mcpriskscan.SurfaceHostedMCP, published[0].GetExecution().GetMediationSurface())
	require.Equal(t, mcpriskscan.MethodToolsCall, published[0].GetExecution().GetMethod())
	require.Equal(t, serverID.String(), published[0].GetExecution().GetMcpServerId())
	require.Equal(t, "lookup", published[0].GetExecution().GetToolName())
}

func TestPolicyEvaluator_FlagLaneIsDetachedAndAtMostOnce(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	policyID := uuid.New()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	publisher := &findingPublisher{}
	var mu sync.Mutex
	calls := 0
	evaluator := newPolicyEvaluator(t, staticPolicies(policycore.Policy{ID: policyID, ProjectID: projectID, OrganizationID: "org-test", Name: "Flag policy", Action: "warn", Version: 2}), policyDetectorFunc(func(ctx context.Context, _ policycore.Policy, _ risk.MCPScanRequest) ([]scanners.Finding, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		started <- struct{}{}
		select {
		case <-release:
			return []scanners.Finding{{RuleID: "flag.rule", Description: "Flagged", Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), publisher, mcpriskscan.PolicyConfig{Deadline: time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1})

	subject := requestSubject(t.Context(), projectID, serverID, `{"query":"flag"}`)
	decision := evaluator.Scan(t.Context(), subject)
	require.False(t, decision.Denied())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("flag evaluation did not start")
	}
	require.False(t, evaluator.Scan(t.Context(), subject).Denied())
	close(release)
	require.Eventually(t, func() bool { return len(publisher.snapshot()) == 1 }, time.Second, time.Millisecond)
	mu.Lock()
	require.Equal(t, 1, calls)
	mu.Unlock()
	require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_LOGGED, publisher.snapshot()[0].GetEnforcementOutcome())
}

func TestPolicyEvaluator_DrainPublishesFlagFindingsReturnedWithError(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	publisher := &findingPublisher{}
	scanErr := errors.New("detector partially failed")
	evaluator := newPolicyEvaluator(t, staticPolicies(policycore.Policy{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Name:           "Flag policy",
		Action:         "flag",
	}), policyDetectorFunc(func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
		started <- struct{}{}
		<-release
		return []scanners.Finding{{
			RuleID:      "flag.partial",
			Description: "Finding returned before detector failure",
			Tags:        []string{},
			Source:      "gitleaks",
			Confidence:  1,
		}}, scanErr
	}), publisher, mcpriskscan.DefaultPolicyConfig)

	require.False(t, evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`)).Denied())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("flag evaluation did not start")
	}

	drained := make(chan error, 1)
	go func() {
		drained <- evaluator.Drain(t.Context())
	}()
	select {
	case <-drained:
		t.Fatal("drain returned before the flag scan completed")
	default:
	}
	close(release)
	select {
	case err := <-drained:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("drain did not return after the flag scan completed")
	}

	published := publisher.snapshot()
	require.Len(t, published, 1)
	require.Equal(t, "flag.partial", published[0].GetRuleId())
	require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_LOGGED, published[0].GetEnforcementOutcome())
}

func TestPolicyEvaluator_NonBlockActionsUseFlagLane(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"flag", "warn", "quarantine"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			projectID := uuid.New()
			serverID := uuid.New()
			publisher := &findingPublisher{}
			evaluator := newPolicyEvaluator(t, staticPolicies(policycore.Policy{
				ID:             uuid.New(),
				ProjectID:      projectID,
				OrganizationID: "org-test",
				Name:           action,
				Action:         action,
			}), policyDetectorFunc(func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
				return []scanners.Finding{{RuleID: action + ".rule", Description: "Flagged", Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
			}), publisher, mcpriskscan.DefaultPolicyConfig)

			require.False(t, evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`)).Denied())
			require.Eventually(t, func() bool { return len(publisher.snapshot()) == 1 }, time.Second, time.Millisecond)
			require.Equal(t, riskv1.Finding_ENFORCEMENT_OUTCOME_LOGGED, publisher.snapshot()[0].GetEnforcementOutcome())
		})
	}
}

func TestPolicyEvaluator_FailModeResolvesIndeterminateEvaluation(t *testing.T) {
	t.Parallel()
	lookupErr := errors.New("database unavailable")
	for _, test := range []struct {
		name     string
		failMode mcpriskscan.FailMode
		denied   bool
	}{
		{name: "open", failMode: mcpriskscan.FailOpen, denied: false},
		{name: "closed", failMode: mcpriskscan.FailClosed, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			evaluator := newPolicyEvaluator(t, policyLookupFunc(func(context.Context, string, uuid.UUID, uuid.UUID, string) ([]policycore.Policy, error) {
				return nil, lookupErr
			}), policyDetectorFunc(nil), &findingPublisher{}, mcpriskscan.PolicyConfig{Deadline: time.Second, FailMode: test.failMode, FlagConcurrency: 1})
			decision := evaluator.Scan(t.Context(), requestSubject(t.Context(), uuid.New(), uuid.New(), `{}`))
			require.Equal(t, test.denied, decision.Denied())
			require.True(t, decision.Indeterminate)
		})
	}
}

func TestPolicyEvaluator_PayloadAvailabilityOnlyMattersWhenPoliciesApply(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	noPolicies := newPolicyEvaluator(t, staticPolicies(), policyDetectorFunc(nil), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig)
	unavailable := mcpriskscan.NewRequest(t.Context(), mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall,
		OrganizationID: "org-test", ProjectID: projectID.String(), ServerID: serverID.String(), ToolName: "lookup",
	}, mcpriskscan.BorrowPayload(nil))
	decision := noPolicies.Scan(t.Context(), unavailable)
	require.False(t, decision.Denied())
	require.False(t, decision.Indeterminate)

	blockPolicy := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Block", Action: "block"}
	evaluator := newPolicyEvaluator(t, staticPolicies(blockPolicy), policyDetectorFunc(nil), &findingPublisher{}, mcpriskscan.PolicyConfig{
		Deadline: time.Second, FailMode: mcpriskscan.FailClosed, FlagConcurrency: 1,
	})
	oversized := mcpriskscan.NewRequest(t.Context(), mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall,
		OrganizationID: "org-test", ProjectID: projectID.String(), ServerID: serverID.String(), ToolName: "lookup",
	}, mcpriskscan.BorrowPayload(make([]byte, mcpriskscan.MaxPayloadBytes+1)))
	decision = evaluator.Scan(t.Context(), oversized)
	require.True(t, decision.Denied())
	require.True(t, decision.Indeterminate)
}

func TestPolicyEvaluator_DeadlineBoundsBlockDetector(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	evaluator := newPolicyEvaluator(t, staticPolicies(policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Block", Action: "block"}), policyDetectorFunc(func(ctx context.Context, _ policycore.Policy, _ risk.MCPScanRequest) ([]scanners.Finding, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), &findingPublisher{}, mcpriskscan.PolicyConfig{Deadline: 20 * time.Millisecond, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1})

	started := time.Now()
	decision := evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`))
	require.False(t, decision.Denied())
	require.True(t, decision.Indeterminate)
	require.Less(t, time.Since(started), 250*time.Millisecond)
}

func TestPolicyEvaluator_BlockPoliciesScanConcurrently(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	slow := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Slow", Action: "block"}
	failing := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Failing", Action: "block"}
	matching := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Matching", Action: "block"}
	slowCancelled := make(chan struct{})
	publisher := &findingPublisher{}
	evaluator := newPolicyEvaluator(t, staticPolicies(slow, failing, matching), policyDetectorFunc(func(ctx context.Context, policy policycore.Policy, _ risk.MCPScanRequest) ([]scanners.Finding, error) {
		switch policy.ID {
		case slow.ID:
			<-ctx.Done()
			close(slowCancelled)
			return nil, ctx.Err()
		case failing.ID:
			return nil, errors.New("detector unavailable")
		default:
			return []scanners.Finding{{RuleID: "secret.token", Description: "Credential detected", Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
		}
	}), publisher, mcpriskscan.PolicyConfig{Deadline: 5 * time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1})

	started := time.Now()
	decision := evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`))
	require.True(t, decision.Denied())
	require.Equal(t, matching.ID.String(), decision.PolicyID)
	require.Less(t, time.Since(started), time.Second)
	<-slowCancelled
	require.Len(t, publisher.snapshot(), 1)
}

func TestPolicyEvaluator_DropsFlagLaneWhenCapacityIsFull(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, meter.Shutdown(context.Background())) })
	evaluator := mcpriskscan.NewPolicyEvaluator(testenv.NewLogger(t), testenv.NewTracerProvider(t), meter, staticPolicies(policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Action: "flag"}), policyDetectorFunc(func(ctx context.Context, _ policycore.Policy, _ risk.MCPScanRequest) ([]scanners.Finding, error) {
		started <- struct{}{}
		select {
		case <-release:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), &findingPublisher{}, mcpriskscan.PolicyConfig{Deadline: time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1})

	require.False(t, evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`)).Denied())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("flag evaluation did not start")
	}
	require.False(t, evaluator.Scan(t.Context(), requestSubject(t.Context(), projectID, serverID, `{}`)).Denied())
	close(release)

	var metrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &metrics))
	for _, scope := range metrics.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "mcp.risk.scan.flag_dropped" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 1)
			require.Equal(t, int64(1), sum.DataPoints[0].Value)
			require.Equal(t, attribute.NewSet(
				attribute.String("gram.mcp.risk.scan.surface", mcpriskscan.SurfaceHostedMCP),
				attribute.String("gram.mcp.risk.scan.method", mcpriskscan.MethodToolsCall),
			), sum.DataPoints[0].Attributes)
			return
		}
	}
	t.Fatal("flag drop metric was not recorded")
}

func newPolicyEvaluator(t *testing.T, lookup mcpriskscan.PolicyLookup, detector mcpriskscan.PolicyDetector, publisher gcp.Publisher[*riskv1.Finding], config mcpriskscan.PolicyConfig) *mcpriskscan.Evaluator {
	t.Helper()
	return mcpriskscan.NewPolicyEvaluator(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), lookup, detector, publisher, config)
}

func staticPolicies(policies ...policycore.Policy) policyLookupFunc {
	return func(context.Context, string, uuid.UUID, uuid.UUID, string) ([]policycore.Policy, error) {
		return policies, nil
	}
}

func requestSubject(ctx context.Context, projectID, serverID uuid.UUID, payload string) mcpriskscan.Subject {
	return mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface:        mcpriskscan.SurfaceHostedMCP,
		Method:         mcpriskscan.MethodToolsCall,
		OrganizationID: "org-test",
		ProjectID:      projectID.String(),
		ServerID:       serverID.String(),
		MetaServerID:   "",
		ToolsetID:      "toolset-test",
		ToolName:       "lookup",
		ResourceURI:    "",
		PromptName:     "",
		ChatID:         "chat-test",
	}, mcpriskscan.BorrowPayload([]byte(payload)))
}
