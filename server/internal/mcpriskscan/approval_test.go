package mcpriskscan_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/approval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/scanners"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type approvalGateFunc func(context.Context, approval.HoldInput) (approval.HoldOutcome, error)

func (f approvalGateFunc) HoldCall(ctx context.Context, call approval.HoldInput) (approval.HoldOutcome, error) {
	return f(ctx, call)
}

func approvalPolicy(projectID uuid.UUID) policycore.Policy {
	return policycore.Policy{
		ID:             uuid.New(),
		ProjectID:      projectID,
		OrganizationID: "org-test",
		Name:           "Approve writes",
		Action:         policycore.ActionRequireApproval,
		ApprovalConfig: &policycore.ApprovalConfig{Approvers: []string{"role:organization:" + uuid.NewString()}},
	}
}

func noDetector(t *testing.T) policyDetectorFunc {
	t.Helper()
	return func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
		t.Error("approval policies must not run detectors")
		return nil, nil
	}
}

func TestPolicyEvaluator_ApprovalHoldsAgentCall(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	serverID := uuid.New()
	agentID := uuid.New()
	policy := approvalPolicy(projectID)

	var mu sync.Mutex
	var calls []approval.HoldInput
	gate := approvalGateFunc(func(_ context.Context, call approval.HoldInput) (approval.HoldOutcome, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call)
		return approval.HoldOutcome{Allowed: false, Token: "apr_test", UserMessage: "ask for approval", Policy: &call.Policies[0]}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(policy), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), agentID)
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, serverID, `{"id":1}`))

	require.True(t, decision.Denied())
	require.True(t, decision.HeldForApproval())
	require.Equal(t, "apr_test", decision.ApprovalToken)
	require.Equal(t, "ask for approval", decision.UserMessage)
	require.Equal(t, policy.ID.String(), decision.PolicyID)
	require.Len(t, calls, 1)
	require.Len(t, calls[0].Policies, 1)
	require.Equal(t, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()).String(), calls[0].RequesterURN)
	require.Empty(t, calls[0].RequesterUserID)
	require.Equal(t, projectID, calls[0].ProjectID)
	require.Equal(t, serverID.String(), calls[0].ServerID)
	require.Equal(t, "lookup", calls[0].ToolName)
	require.JSONEq(t, `{"id":1}`, string(calls[0].Arguments))
}

func TestPolicyEvaluator_ApprovalAllowsCoveredCall(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	var consulted atomic.Int32
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		consulted.Add(1)
		return approval.HoldOutcome{Allowed: true, Token: "", UserMessage: "", Policy: nil}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(approvalPolicy(projectID)), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

	require.False(t, decision.Denied())
	require.False(t, decision.HeldForApproval())
	require.Equal(t, int32(1), consulted.Load(), "the gate decides covered calls")
}

func TestPolicyEvaluator_ApprovalFailsClosed(t *testing.T) {
	t.Parallel()
	failing := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		return approval.HoldOutcome{}, errors.New("database unavailable")
	})
	cases := map[string]struct {
		options []mcpriskscan.PolicyEvaluatorOption
		stamp   func(context.Context) context.Context
		message string
	}{
		"no gate": {
			options: nil,
			stamp: func(ctx context.Context) context.Context {
				return mcpidentity.NewValidatorBoundary().StampAgent(ctx, uuid.New())
			},
			message: "approvals are not available",
		},
		"gate error": {
			options: []mcpriskscan.PolicyEvaluatorOption{mcpriskscan.WithApprovalGate(failing)},
			stamp: func(ctx context.Context) context.Context {
				return mcpidentity.NewValidatorBoundary().StampAgent(ctx, uuid.New())
			},
			message: "could not be recorded",
		},
		"anonymous caller": {
			options: []mcpriskscan.PolicyEvaluatorOption{mcpriskscan.WithApprovalGate(failing)},
			stamp:   func(ctx context.Context) context.Context { return ctx },
			message: "cannot request approval",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			projectID := uuid.New()
			evaluator := newPolicyEvaluator(t, staticPolicies(approvalPolicy(projectID)), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, tc.options...)

			ctx := tc.stamp(t.Context())
			decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

			require.True(t, decision.Denied())
			require.False(t, decision.HeldForApproval())
			require.Contains(t, decision.UserMessage, tc.message)
		})
	}
}

func TestPolicyEvaluator_ApprovalSkipsResponses(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		t.Error("responses must not be held for approval")
		return approval.HoldOutcome{}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(approvalPolicy(projectID)), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	request := requestSubject(ctx, projectID, uuid.New(), `{}`)
	decision := evaluator.Scan(ctx, mcpriskscan.NewResponse(request, mcpriskscan.TextResponsePayload([]byte("ok"))))

	require.False(t, decision.Denied())
}

func TestPolicyEvaluator_BlockRunsBeforeApproval(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	block := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Block", Action: "block"}
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		t.Error("a blocked call must not create an approval request")
		return approval.HoldOutcome{}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(block, approvalPolicy(projectID)), policyDetectorFunc(func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
		return []scanners.Finding{{RuleID: "block.rule", Description: "Blocked", Tags: []string{}, Source: "gitleaks", Confidence: 1}}, nil
	}), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

	require.True(t, decision.Denied())
	require.False(t, decision.HeldForApproval())
	require.Equal(t, block.ID.String(), decision.PolicyID)
}

func TestPolicyEvaluator_ApprovalDeniesUnreadableArguments(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		t.Error("unreadable arguments must not create an approval request")
		return approval.HoldOutcome{}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(approvalPolicy(projectID)), noDetector(t), &findingPublisher{}, mcpriskscan.PolicyConfig{
		Deadline: time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1,
	}, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	oversized := mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodToolsCall,
		OrganizationID: "org-test", ProjectID: projectID.String(), ServerID: uuid.NewString(), ToolName: "lookup",
	}, mcpriskscan.BorrowPayload(make([]byte, mcpriskscan.MaxPayloadBytes+1)))
	decision := evaluator.Scan(ctx, oversized)

	require.True(t, decision.Denied(), "approval must not fail open")
	require.False(t, decision.HeldForApproval())
}

func TestPolicyEvaluator_FailOpenBlockLaneStillHoldsForApproval(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	block := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Block", Action: "block"}
	gate := approvalGateFunc(func(_ context.Context, call approval.HoldInput) (approval.HoldOutcome, error) {
		return approval.HoldOutcome{Allowed: false, Token: "apr_test", UserMessage: "ask for approval", Policy: &call.Policies[0]}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(block, approvalPolicy(projectID)), policyDetectorFunc(func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
		return nil, errors.New("detector unavailable")
	}), &findingPublisher{}, mcpriskscan.PolicyConfig{Deadline: time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1}, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

	require.True(t, decision.HeldForApproval())
	require.Equal(t, "apr_test", decision.ApprovalToken)
}

func TestPolicyEvaluator_ApprovalPassesEveryCoveringPolicy(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	first := approvalPolicy(projectID)
	second := approvalPolicy(projectID)
	gate := approvalGateFunc(func(_ context.Context, call approval.HoldInput) (approval.HoldOutcome, error) {
		require.Len(t, call.Policies, 2)
		// The first policy already approved; the second still holds the call.
		return approval.HoldOutcome{Allowed: false, Token: "apr_second", UserMessage: "ask again", Policy: &call.Policies[1]}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(first, second), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

	require.True(t, decision.HeldForApproval())
	require.Equal(t, second.ID.String(), decision.PolicyID)
	require.Equal(t, "apr_second", decision.ApprovalToken)
}

func TestPolicyEvaluator_ApprovalOnlyHoldsToolCalls(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		t.Error("only tool calls are held for approval")
		return approval.HoldOutcome{}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(approvalPolicy(projectID)), noDetector(t), &findingPublisher{}, mcpriskscan.DefaultPolicyConfig, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	read := mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface: mcpriskscan.SurfaceHostedMCP, Method: mcpriskscan.MethodResourcesRead,
		OrganizationID: "org-test", ProjectID: projectID.String(), ServerID: uuid.NewString(), ResourceURI: "file:///notes",
	}, mcpriskscan.BorrowPayload([]byte(`{}`)))

	require.False(t, evaluator.Scan(ctx, read).Denied())
}

func TestPolicyEvaluator_FailOpenBlockLaneMarksApprovalIndeterminate(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	block := policycore.Policy{ID: uuid.New(), ProjectID: projectID, OrganizationID: "org-test", Name: "Block", Action: "block"}
	gate := approvalGateFunc(func(context.Context, approval.HoldInput) (approval.HoldOutcome, error) {
		return approval.HoldOutcome{Allowed: true, Token: "", UserMessage: "", Policy: nil}, nil
	})
	evaluator := newPolicyEvaluator(t, staticPolicies(block, approvalPolicy(projectID)), policyDetectorFunc(func(context.Context, policycore.Policy, risk.MCPScanRequest) ([]scanners.Finding, error) {
		return nil, errors.New("detector unavailable")
	}), &findingPublisher{}, mcpriskscan.PolicyConfig{Deadline: time.Second, FailMode: mcpriskscan.FailOpen, FlagConcurrency: 1}, mcpriskscan.WithApprovalGate(gate))

	ctx := mcpidentity.NewValidatorBoundary().StampAgent(t.Context(), uuid.New())
	decision := evaluator.Scan(ctx, requestSubject(ctx, projectID, uuid.New(), `{}`))

	require.False(t, decision.Denied())
	require.True(t, decision.Indeterminate)
}
