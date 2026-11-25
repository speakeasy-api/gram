package growthsignals_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/growthsignals"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	handlerOrganizationID = "<ORGANIZATION_ID>"
	handlerEventID        = "<EVENT_ID>"
	handlerUserID         = "<USER_ID>"
	handlerUserEmail      = "person@example.test"
)

// handlerProjectID is the project a test audit record is scoped to.
var handlerProjectID = uuid.MustParse("11111111-2222-3333-4444-555555555555")

func auditWebhookEvent(t *testing.T, eventType string, action audit.Action) *webhooksv1.Event {
	t.Helper()

	payload, err := json.Marshal(events.AuditLogCreatedPayloadV1{
		ID:                 uuid.MustParse("99999999-8888-7777-6666-555555555555"),
		OrganizationID:     handlerOrganizationID,
		ProjectID:          uuid.NullUUID{UUID: handlerProjectID, Valid: true},
		ActorID:            handlerUserID,
		ActorType:          string(urn.PrincipalTypeUser),
		ActorDisplayName:   "Placeholder Person",
		ActorSlug:          "placeholder-person",
		Action:             string(action),
		SubjectID:          "<SUBJECT_ID>",
		SubjectType:        "mcp_server",
		SubjectDisplayName: "Widgets Server",
		SubjectSlug:        "widgets-server",
		ActingSurface:      string(audit.SurfaceDashboard),
		BeforeSnapshot:     nil,
		AfterSnapshot:      nil,
		Metadata:           nil,
		ActingClientID:     "",
	})
	require.NoError(t, err)

	eventID := handlerEventID
	organizationID := handlerOrganizationID
	createdAt := "2026-09-03T12:00:00Z"

	return webhooksv1.Event_builder{
		EventId:        &eventID,
		OrganizationId: &organizationID,
		EventType:      &eventType,
		Payload:        payload,
		CreatedAt:      &createdAt,
	}.Build()
}

func newTestEventHandler(t *testing.T) (*growthsignals.EventHandler, *capturePostHog) {
	t.Helper()

	client := &capturePostHog{}
	enricher := &fakeEnricher{
		organization: growthsignals.OrganizationDetails{Slug: "acme", Name: "Acme Incorporated"},
		project:      growthsignals.ProjectDetails{Slug: "widgets", Name: "Widgets"},
		userEmails:   map[string]string{handlerUserID: handlerUserEmail},
	}
	emitter := growthsignals.NewEmitter(testenv.NewLogger(t), client, enricher, emitterSiteURL())

	return growthsignals.NewEventHandler(testenv.NewLogger(t), emitter), client
}

func testMessageMetadata() gcp.MessageMetadata {
	return gcp.MessageMetadata{ID: "<MESSAGE_ID>", Attributes: nil, DeliveryAttempt: nil}
}

func TestEventHandlerEmitsCuratedActivity(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, string(events.RemoteMcpServerV1.EventType()), audit.ActionRemoteMcpServerCreate)

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))

	captured := client.Captured()
	require.Len(t, captured, 1)
	require.Equal(t, growthsignals.EventName, captured[0].Name)
	require.Equal(t, handlerUserEmail, captured[0].DistinctID)

	properties := captured[0].Properties
	require.Equal(t, "mcp_server_created", properties["activity"])
	require.Equal(t, "remote", properties["mcp_kind"])
	require.Equal(t, handlerOrganizationID, properties["organization_id"])
	require.Equal(t, "acme", properties["organization_slug"])
	require.Equal(t, handlerProjectID.String(), properties["project_id"])
	require.Equal(t, "widgets", properties["project_slug"])
	require.Equal(t, handlerUserEmail, properties["actor_email"])
	require.Equal(t, "Placeholder Person", properties["actor_name"])
	require.Equal(t, "Widgets Server", properties["subject_name"])
	require.Equal(t, string(audit.SurfaceDashboard), properties["acting_surface"])
	require.Equal(t, string(audit.ActionRemoteMcpServerCreate), properties["audit_action"])
	require.Equal(t, "https://app.example.test/acme", properties["dashboard_url"])
}

// An excluded action is the high-volume kind the firehose exists in spite of,
// so it must not reach PostHog at all.
func TestEventHandlerSkipsExcludedAction(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, string(events.AssistantToolCallV1.EventType()), audit.ActionAssistantToolCall)

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))
	require.Empty(t, client.Captured())
}

// An action nobody curated still ships, so the firehose covers new audit
// coverage the day it lands rather than when someone remembers to list it.
func TestEventHandlerPassesThroughUncuratedAction(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, string(events.ToolsetV1.EventType()), audit.ActionToolsetCreate)

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))

	captured := client.Captured()
	require.Len(t, captured, 1)
	require.Equal(t, "toolset_create", captured[0].Properties["activity"])
	require.NotContains(t, captured[0].Properties, "mcp_kind")
}

func TestEventHandlerIgnoresNonAuditEventType(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, "organization.usage_v1", audit.ActionProjectCreate)

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))
	require.Empty(t, client.Captured())
}

func TestEventHandlerAcksUnreadablePayload(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, string(events.ProjectV1.EventType()), audit.ActionProjectCreate)
	event.SetPayload([]byte("{not json"))

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))
	require.Empty(t, client.Captured())
}

func TestEventHandlerAcksMalformedEnvelope(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	metadata := testMessageMetadata()

	require.NoError(t, handler.Handle(t.Context(), nil, metadata))

	blankEventID := auditWebhookEvent(t, string(events.ProjectV1.EventType()), audit.ActionProjectCreate)
	blankEventID.SetEventId("")
	require.NoError(t, handler.Handle(t.Context(), blankEventID, metadata))

	blankOrganizationID := auditWebhookEvent(t, string(events.ProjectV1.EventType()), audit.ActionProjectCreate)
	blankOrganizationID.SetOrganizationId("")
	require.NoError(t, handler.Handle(t.Context(), blankOrganizationID, metadata))

	require.Empty(t, client.Captured())
}

// The topic is at-least-once and this handler shares a message with its
// siblings, so a sibling failure redelivers the event to everyone. The stable
// outbox id is what lets PostHog collapse the redelivery into one occurrence.
func TestEventHandlerStampsDeduplicationKey(t *testing.T) {
	t.Parallel()

	handler, client := newTestEventHandler(t)
	event := auditWebhookEvent(t, string(events.RemoteMcpServerV1.EventType()), audit.ActionRemoteMcpServerCreate)

	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))
	require.NoError(t, handler.Handle(t.Context(), event, testMessageMetadata()))

	captured := client.Captured()
	require.Len(t, captured, 2, "the handler itself does not dedupe; PostHog does")
	require.Equal(t, handlerEventID, captured[0].Properties["$insert_id"])
	require.Equal(t, captured[0].Properties["$insert_id"], captured[1].Properties["$insert_id"],
		"a redelivery of one event must carry one key")
}
