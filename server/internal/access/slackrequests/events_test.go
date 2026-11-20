package slackrequests_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/access/slackrequests"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type fakeScheduler struct {
	started  []slackrequests.Target
	signaled []slackrequests.Target
}

func (s *fakeScheduler) StartNotify(_ context.Context, target slackrequests.Target) error {
	s.started = append(s.started, target)
	return nil
}

func (s *fakeScheduler) SignalDecided(_ context.Context, target slackrequests.Target) error {
	s.signaled = append(s.signaled, target)
	return nil
}

func requestEvent(t *testing.T, eventType, organizationID string, action audit.Action, requestID uuid.UUID) *webhooksv1.Event {
	t.Helper()
	// The audit logger stores the request's API view as is.
	snapshot, err := json.Marshal(map[string]any{"ID": requestID.String(), "RequesterUserID": "user_example", "Scope": "chat:read", "Status": "pending"})
	require.NoError(t, err)
	payload, err := json.Marshal(events.AuditLogCreatedPayloadV1{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		ActorID:        "user_example",
		ActorType:      "user",
		Action:         string(action),
		SubjectID:      requestID.String(),
		SubjectType:    "access_request",
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		AfterSnapshot:  snapshot,
	})
	require.NoError(t, err)
	return webhooksv1.Event_builder{EventId: new(uuid.NewString()), OrganizationId: new(organizationID), EventType: new(eventType), Payload: payload, CreatedAt: nil}.Build()
}

func TestEventsStartAndWakeTheSlackRun(t *testing.T) {
	t.Parallel()
	scheduler := &fakeScheduler{started: nil, signaled: nil}
	handler := slackrequests.NewEventHandler(testenv.NewLogger(t), scheduler)
	eventType := string(events.AccessRequestV1.EventType())
	requestID := uuid.New()
	want := slackrequests.Target{OrganizationID: "org_example", RequestID: requestID}
	metadata := gcp.MessageMetadata{}

	for _, action := range []audit.Action{audit.ActionAccessRequestCreate, audit.ActionAccessRequestApprove, audit.ActionAccessRequestDeny, audit.ActionAccessRequestWithdraw} {
		require.NoError(t, handler.Handle(t.Context(), requestEvent(t, eventType, "org_example", action, requestID), metadata))
	}
	require.Equal(t, []slackrequests.Target{want}, scheduler.started)
	require.Equal(t, []slackrequests.Target{want, want, want}, scheduler.signaled)
}

func TestEventsIgnoreEverythingElse(t *testing.T) {
	t.Parallel()
	scheduler := &fakeScheduler{started: nil, signaled: nil}
	handler := slackrequests.NewEventHandler(testenv.NewLogger(t), scheduler)
	eventType := string(events.AccessRequestV1.EventType())
	metadata := gcp.MessageMetadata{}

	require.NoError(t, handler.Handle(t.Context(), requestEvent(t, eventType, "org_example", audit.ActionAccessGrantCreateTemporary, uuid.New()), metadata))
	require.NoError(t, handler.Handle(t.Context(), requestEvent(t, string(events.AccessRoleV1.EventType()), "org_example", audit.ActionAccessRequestCreate, uuid.New()), metadata))
	mismatched := requestEvent(t, eventType, "org_example", audit.ActionAccessRequestCreate, uuid.New())
	mismatched.SetOrganizationId("org_other")
	require.NoError(t, handler.Handle(t.Context(), mismatched, metadata))
	require.NoError(t, handler.Handle(t.Context(), nil, metadata))
	require.Empty(t, scheduler.started)
	require.Empty(t, scheduler.signaled)
}
