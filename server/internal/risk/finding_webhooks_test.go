package risk_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOutboxRiskFindingCreatedEmitterPublishesStableEvent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	emitter := risk.NewOutboxRiskFindingCreatedEmitter(ti.conn)
	findingID := uuid.New()
	candidate := risk.RiskFindingCreatedEvent{
		OrganizationID: authCtx.ActiveOrganizationID,
		Event: outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]{
			ID: events.RiskFindingCreatedEventID(findingID),
			Payload: events.RiskFindingCreatedPayloadV1{
				ID:                findingID,
				ProjectID:         *authCtx.ProjectID,
				OrganizationID:    authCtx.ActiveOrganizationID,
				RiskPolicyID:      uuid.New(),
				RiskPolicyVersion: 1,
				ChatMessageID:     uuid.New(),
				RuleID:            "rule",
				Description:       "description",
				Confidence:        0.9,
				Tags:              []string{"secret"},
				CreatedAt:         time.Now().UTC(),
			},
		},
	}

	before, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.NoError(t, emitter.Emit(ctx, []risk.RiskFindingCreatedEvent{candidate}))
	after, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	outboxRows, err := testrepo.New(ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	eventID := events.RiskFindingCreatedEventID(findingID)
	idx := slices.IndexFunc(outboxRows, func(row testrepo.ListPublishOutboxRowsRow) bool { return row.PublicID == eventID })
	require.NotEqual(t, -1, idx, "outbox row must carry the finding's stable event id")
	require.Equal(t, authCtx.ActiveOrganizationID, outboxRows[idx].OrganizationID)
	envelope := new(webhooksv1.Event)
	require.NoError(t, proto.Unmarshal(outboxRows[idx].Message, envelope))
	require.Equal(t, eventID.String(), envelope.GetEventId())
	var payload events.RiskFindingCreatedPayloadV1
	require.NoError(t, json.Unmarshal(envelope.GetPayload(), &payload))
	require.Equal(t, findingID, payload.ID)

}
