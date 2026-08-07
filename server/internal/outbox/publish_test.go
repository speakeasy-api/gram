package outbox_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	orgsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestPublish_StoresTopicAndMarshaledMessage(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	eventID := uuid.NewString()
	eventType := "audit_log.asset_event_v1"
	msg := webhooksv1.Event_builder{
		EventId:        &eventID,
		OrganizationId: &orgID,
		EventType:      &eventType,
		Payload:        []byte(`{"a":1}`),
	}.Build()

	res, err := outbox.Publish(t.Context(), inst.conn, orgID, outbox.Message{Proto: msg})
	require.NoError(t, err)
	require.NotZero(t, res.ID)

	row, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), res.ID)
	require.NoError(t, err)
	require.Equal(t, "gram.webhooks.v1.Event", row.Topic,
		"the row must name its destination by proto full name so the relay stays topic-agnostic")

	var decoded webhooksv1.Event
	require.NoError(t, proto.Unmarshal(row.Message, &decoded))
	require.Equal(t, eventID, decoded.GetEventId())
	require.JSONEq(t, `{"a":1}`, string(decoded.GetPayload()))
}

func TestPublish_RejectsUndeclaredTopic(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	// SvixRelay is a subscription marker: it declares no topic option, so it is
	// absent from the generated registry and must be rejected at the write.
	_, err := outbox.Publish(t.Context(), inst.conn, orgID, outbox.Message{Proto: &webhooksv1.SvixRelay{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a declared pubsub topic",
		"an undeclared topic should fail at the write, where the mistake is still attached to its cause")
}

func TestPublish_RejectsOversizedMessage(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	// A row larger than Pub/Sub accepts would sit at the head of an id-ordered
	// queue failing forever, blocking every message behind it.
	huge := webhooksv1.Event_builder{Payload: []byte(strings.Repeat("x", 10*1024*1024))}.Build()

	_, err := outbox.Publish(t.Context(), inst.conn, orgID, outbox.Message{Proto: huge})
	require.Error(t, err)
	require.Contains(t, err.Error(), "over the")
	require.Equal(t, int64(0), inst.countRows(t))
}

// TestPublishWebhookEvent_EventIDMatchesRow guards the invariant the Svix
// idempotency key depends on: the id inside the envelope and the id on the row
// must be the same value, or a redelivery would present a different key and be
// treated as a new event.
func TestPublishWebhookEvent_EventIDMatchesRow(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	res, err := outbox.PublishWebhookEvent(t.Context(), inst.conn, orgID, events.AssetV1, events.AuditLogCreatedPayloadV1{
		ID:             uuid.New(),
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	row, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), res.ID)
	require.NoError(t, err)

	var decoded webhooksv1.Event
	require.NoError(t, proto.Unmarshal(row.Message, &decoded))
	require.Equal(t, row.PublicID.String(), decoded.GetEventId())
	require.Equal(t, res.PublicID.String(), decoded.GetEventId())
}

func TestPublishWebhookEvent_SetsEventTypeAttribute(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	res, err := outbox.PublishWebhookEvent(t.Context(), inst.conn, orgID, events.AssetV1, events.AuditLogCreatedPayloadV1{
		ID:             uuid.New(),
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	row, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), res.ID)
	require.NoError(t, err)

	var attrs map[string]string
	require.NoError(t, json.Unmarshal(row.Attributes, &attrs))
	require.Equal(t, string(events.AssetV1.EventType()), attrs["event_type"],
		"subscription filters can only see attributes, never the message body")
}

// TestPublishWebhookEvents_MintsDistinctIDs would fail against a shared id: the
// unique index on public_id turns a collision into a write error rather than a
// silent duplicate, and the batch path has to survive it.
func TestPublishWebhookEvents_MintsDistinctIDs(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	payloads := []events.AuditLogCreatedPayloadV1{
		{ID: uuid.New(), OrganizationID: orgID},
		{ID: uuid.New(), OrganizationID: orgID},
		{ID: uuid.New(), OrganizationID: orgID},
	}

	res, err := outbox.PublishWebhookEvents(t.Context(), inst.conn, orgID, events.AssetV1, payloads)
	require.NoError(t, err)
	require.Equal(t, int64(3), res.Count)
	require.Equal(t, int64(3), inst.countRows(t))
}

// TestWebhookEnvelopeRoundTrips is the drift guard between the typed event
// catalog and the transport envelope: whatever an EventDef declares must come
// back out of the envelope unchanged.
func TestWebhookEnvelopeRoundTrips(t *testing.T) {
	t.Parallel()

	inst := newOutboxTestInstance(t)
	orgID := inst.seedOrg(t)

	payload := events.AuditLogCreatedPayloadV1{
		ID:                 uuid.New(),
		OrganizationID:     orgID,
		Action:             "asset.create",
		SubjectType:        "asset",
		SubjectDisplayName: "Test Asset",
	}

	res, err := outbox.PublishWebhookEvent(t.Context(), inst.conn, orgID, events.AssetV1, payload)
	require.NoError(t, err)

	row, err := testrepo.New(inst.conn).GetPublishOutboxRow(t.Context(), res.ID)
	require.NoError(t, err)

	var decoded webhooksv1.Event
	require.NoError(t, proto.Unmarshal(row.Message, &decoded))

	var out events.AuditLogCreatedPayloadV1
	require.NoError(t, json.Unmarshal(decoded.GetPayload(), &out))
	require.Equal(t, payload.ID, out.ID)
	require.Equal(t, payload.Action, out.Action)
	require.Equal(t, payload.SubjectDisplayName, out.SubjectDisplayName)
	require.Equal(t, string(events.AssetV1.EventType()), decoded.GetEventType())
	require.Equal(t, orgID, decoded.GetOrganizationId())
}

func (i *outboxTestInstance) seedOrg(t *testing.T) string {
	t.Helper()

	orgID := uuid.NewString()
	_, err := orgsrepo.New(i.conn).UpsertOrganizationMetadata(t.Context(), orgsrepo.UpsertOrganizationMetadataParams{
		ID:   orgID,
		Name: orgID,
		Slug: orgID,
	})
	require.NoError(t, err)

	return orgID
}

func (i *outboxTestInstance) countRows(t *testing.T) int64 {
	t.Helper()

	n, err := testrepo.New(i.conn).CountPublishOutboxRows(t.Context())
	require.NoError(t, err)

	return n
}
