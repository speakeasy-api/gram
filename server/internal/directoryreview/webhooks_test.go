package directoryreview_test

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type reviewWebhook struct {
	EventType string
	EventID   string
	Payload   events.DirectoryReviewPayloadV1
}

// reviewOutboxMessages returns the outbox messages of directory review
// events, oldest first. Seeding the organization enqueues others.
func reviewOutboxMessages(t *testing.T, conn *pgxpool.Pool) []testrepo.ListPublishOutboxRowsRow {
	t.Helper()
	rows, err := testrepo.New(conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)

	out := make([]testrepo.ListPublishOutboxRowsRow, 0, len(rows))
	for _, row := range rows {
		var attributes map[string]string
		require.NoError(t, json.Unmarshal(row.Attributes, &attributes))
		switch attributes["event_type"] {
		case string(events.DirectoryReviewOpenedV1.EventType()), string(events.DirectoryReviewResolvedV1.EventType()):
			out = append(out, row)
		}
	}
	return out
}

func listReviewWebhooks(t *testing.T, conn *pgxpool.Pool) []reviewWebhook {
	t.Helper()
	rows := reviewOutboxMessages(t, conn)

	out := make([]reviewWebhook, 0, len(rows))
	for _, row := range rows {
		var event webhooksv1.Event
		require.NoError(t, proto.Unmarshal(row.Message, &event))
		require.Equal(t, row.PublicID.String(), event.GetEventId())
		var payload events.DirectoryReviewPayloadV1
		require.NoError(t, json.Unmarshal(event.GetPayload(), &payload))
		out = append(out, reviewWebhook{EventType: event.GetEventType(), EventID: event.GetEventId(), Payload: payload})
	}
	return out
}

func TestReviewChangesPublishWebhooks(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	sweep(t, conn)
	require.Empty(t, listReviewWebhooks(t, conn), "a sweep that changes nothing publishes nothing")

	group := seedGroup(t, conn, "group_new")
	changes, err := directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, changes.Opened, 1)
	item := changes.Opened[0]

	opened := listReviewWebhooks(t, conn)
	require.Len(t, opened, 1)
	require.Equal(t, string(events.DirectoryReviewOpenedV1.EventType()), opened[0].EventType)
	require.Equal(t, events.DirectoryReviewEventID(item.ID, "opened").String(), opened[0].EventID)
	payload := opened[0].Payload
	require.Equal(t, item.ID, payload.ID)
	require.Equal(t, testOrg, payload.OrganizationID)
	require.Equal(t, directoryreview.KindNewGroup, payload.Kind)
	require.Equal(t, "group", payload.SourceKind)
	require.NotNil(t, payload.DirectoryGroupID)
	require.Equal(t, group, *payload.DirectoryGroupID)
	require.Equal(t, directoryreview.StatePending, payload.State)
	require.EqualValues(t, 1, payload.Occurrence)
	require.EqualValues(t, 1, payload.PendingCount)

	// Replaying the event opens nothing, so nothing more is published.
	_, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	require.Len(t, listReviewWebhooks(t, conn), 1)

	// Mapping the group settles the item and announces it.
	mapGroup(t, conn, group)
	_, err = directoryreview.ReconcileGroup(t.Context(), conn, testOrg, group)
	require.NoError(t, err)
	all := listReviewWebhooks(t, conn)
	require.Len(t, all, 2)
	resolved := all[1]
	require.Equal(t, string(events.DirectoryReviewResolvedV1.EventType()), resolved.EventType)
	require.Equal(t, events.DirectoryReviewEventID(item.ID, "resolved").String(), resolved.EventID)
	require.Equal(t, directoryreview.StateSuperseded, resolved.Payload.State)
	require.EqualValues(t, 0, resolved.Payload.PendingCount)
}

func TestAttributeReviewWebhooksCarryNoAttributeText(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	mapAttribute(t, conn, "department", "Engineering")
	seedUser(t, conn, "user_a", `{"department":"Engineering"}`)
	sweep(t, conn)

	changes := setUserAttributes(t, conn, "user_a", `{"department":"Sales"}`)
	require.NotEmpty(t, changes.Opened)

	rows := reviewOutboxMessages(t, conn)
	require.Len(t, rows, len(changes.Opened)+len(changes.Superseded))
	for _, row := range rows {
		require.NotContains(t, string(row.Message), "department")
		require.NotContains(t, string(row.Message), "Sales")
		require.NotContains(t, string(row.Message), "Engineering")
	}
	for _, webhook := range listReviewWebhooks(t, conn) {
		require.Equal(t, "attribute", webhook.Payload.SourceKind)
		require.Nil(t, webhook.Payload.DirectoryGroupID)
	}
}
