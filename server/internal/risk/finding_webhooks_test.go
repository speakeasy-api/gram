package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

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

	flags.SetFlag(feature.FlagRiskFindingsClickHouseCutover, authCtx.ActiveOrganizationID, false)
	require.NoError(t, emitter.Emit(ctx, []risk.RiskFindingCreatedEvent{candidate}))
	afterDisabled, err := testrepo.New(ti.conn).CountOutboxEntriesByEventType(ctx, string(events.RiskFindingCreatedV1.EventType()))
	require.NoError(t, err)
	require.Equal(t, afterEnabled, afterDisabled)
}
