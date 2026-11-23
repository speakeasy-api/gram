package slackapproval_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// fakeScheduler records what the event handler asked for.
type fakeScheduler struct {
	started  []slackapproval.Target
	signaled []slackapproval.Target
}

func (s *fakeScheduler) StartNotify(_ context.Context, target slackapproval.Target) error {
	s.started = append(s.started, target)
	return nil
}

func (s *fakeScheduler) SignalDecided(_ context.Context, target slackapproval.Target) error {
	s.signaled = append(s.signaled, target)
	return nil
}

func approvalEvent(t *testing.T, eventType, organizationID string, action audit.Action, projectID, approvalID uuid.UUID) *webhooksv1.Event {
	t.Helper()
	snapshot, err := json.Marshal(audit.RiskPolicyApprovalSnapshot{
		ID: approvalID.String(), PolicyID: uuid.NewString(), PolicyVersion: 1, Status: "pending", RequesterURN: "agent:" + uuid.NewString(),
		Surface: "hosted_mcp", ToolName: "slack__post_message", Reason: nil, RequestedScope: nil, GrantedScope: nil,
		DecidedByURN: nil, DecidedVia: nil, DecisionNote: nil, RequestedAt: nil, DecidedAt: nil, ExpiresAt: nil,
	})
	require.NoError(t, err)
	payload, err := json.Marshal(events.AuditLogCreatedPayloadV1{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		ActorID:        "agent",
		ActorType:      "agent",
		Action:         string(action),
		SubjectID:      uuid.NewString(),
		SubjectType:    "risk_policy",
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: true},
		AfterSnapshot:  snapshot,
	})
	require.NoError(t, err)
	return webhooksv1.Event_builder{EventId: new(uuid.NewString()), OrganizationId: new(organizationID), EventType: new(eventType), Payload: payload, CreatedAt: nil}.Build()
}

func TestEventsStartAndWakeTheSlackRun(t *testing.T) {
	t.Parallel()
	scheduler := &fakeScheduler{started: nil, signaled: nil}
	handler := slackapproval.NewEventHandler(testenv.NewLogger(t), scheduler)
	riskEvent := string(events.RiskPolicyV1.EventType())
	projectID, approvalID := uuid.New(), uuid.New()
	want := slackapproval.Target{OrganizationID: "org_example", ProjectID: projectID, ApprovalID: approvalID}
	metadata := gcp.MessageMetadata{}

	require.NoError(t, handler.Handle(t.Context(), approvalEvent(t, riskEvent, "org_example", audit.ActionRiskPolicyApprovalRequest, projectID, approvalID), metadata))
	require.NoError(t, handler.Handle(t.Context(), approvalEvent(t, riskEvent, "org_example", audit.ActionRiskPolicyApprovalApprove, projectID, approvalID), metadata))
	require.NoError(t, handler.Handle(t.Context(), approvalEvent(t, riskEvent, "org_example", audit.ActionRiskPolicyApprovalDeny, projectID, approvalID), metadata))
	require.Equal(t, []slackapproval.Target{want}, scheduler.started)
	require.Equal(t, []slackapproval.Target{want, want}, scheduler.signaled)
}

func TestEventsIgnoreEverythingElse(t *testing.T) {
	t.Parallel()
	scheduler := &fakeScheduler{started: nil, signaled: nil}
	handler := slackapproval.NewEventHandler(testenv.NewLogger(t), scheduler)
	riskEvent := string(events.RiskPolicyV1.EventType())
	metadata := gcp.MessageMetadata{}

	// Policy edits share the event type; another event type never matches;
	// an event whose payload names another organization is dropped.
	require.NoError(t, handler.Handle(t.Context(), approvalEvent(t, riskEvent, "org_example", audit.ActionRiskPolicyUpdate, uuid.New(), uuid.New()), metadata))
	require.NoError(t, handler.Handle(t.Context(), approvalEvent(t, string(events.AccessRoleV1.EventType()), "org_example", audit.ActionRiskPolicyApprovalRequest, uuid.New(), uuid.New()), metadata))
	mismatched := approvalEvent(t, riskEvent, "org_example", audit.ActionRiskPolicyApprovalRequest, uuid.New(), uuid.New())
	mismatched.SetOrganizationId("org_other")
	require.NoError(t, handler.Handle(t.Context(), mismatched, metadata))
	require.NoError(t, handler.Handle(t.Context(), nil, metadata))
	require.Empty(t, scheduler.started)
	require.Empty(t, scheduler.signaled)
}
