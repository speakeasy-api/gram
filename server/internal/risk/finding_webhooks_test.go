package risk_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	webhooksv1 "github.com/speakeasy-api/gram/infra/gen/gram/webhooks/v1"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestOutboxRiskFindingCreatedEmitterHonorsCutoverFlag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskFindingsClickHouseCutover, authCtx.ActiveOrganizationID, true)
	emitter := risk.NewOutboxRiskFindingCreatedEmitter(testenv.NewLogger(t), ti.conn, flags)
	findingID := uuid.New()
	candidate := risk.RiskFindingCreatedEvent{
		ProjectID:      *authCtx.ProjectID,
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
	afterEnabled, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, before+1, afterEnabled)

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

	flags.SetFlag(feature.FlagRiskFindingsClickHouseCutover, authCtx.ActiveOrganizationID, false)
	require.NoError(t, emitter.Emit(ctx, []risk.RiskFindingCreatedEvent{candidate}))
	afterDisabled, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, afterEnabled, afterDisabled)
}

type unreachableFlags struct{}

func (unreachableFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("flag service unreachable")
}

func (unreachableFlags) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, errors.New("flag service unreachable")
}

func (unreachableFlags) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, errors.New("flag service unreachable")
}

func TestOutboxRiskFindingCreatedEmitterFailsOnFlagLookupError(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	emitter := risk.NewOutboxRiskFindingCreatedEmitter(testenv.NewLogger(t), ti.conn, unreachableFlags{})
	findingID := uuid.New()
	err := emitter.Emit(ctx, []risk.RiskFindingCreatedEvent{{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Event: outbox.IdentifiedWebhookEvent[events.RiskFindingCreatedPayloadV1]{
			ID:      events.RiskFindingCreatedEventID(findingID),
			Payload: events.RiskFindingCreatedPayloadV1{ID: findingID},
		},
	}})
	require.Error(t, err, "an unresolved flag must fail the batch so redelivery retries it")
}
