package svixrelay_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/svix/svix-webhooks/go/models"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// TestAllowSet_ResolvesOrgsIndependently is the point of resolving per
// organization: one ineligible organization's events cost a single cached "no"
// and say nothing about the next organization's eligibility.
func TestAllowSet_ResolvesOrgsIndependently(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t, true)
	ineligible := seedOrg(t, inst.conn, "app_123", false)
	eligible := seedOrg(t, inst.conn, "app_456", true)

	droppedEvent := uuid.NewString()
	deliveredEvent := uuid.NewString()

	var captured []string
	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything).
		Return(&models.MessageOut{Id: "msg_1"}, nil).
		Run(func(args mock.Arguments) {
			in, ok := args.Get(1).(*models.MessageIn)
			require.True(t, ok)
			require.NotNil(t, in.EventId)
			captured = append(captured, *in.EventId)
		})

	err := inst.handler.Handle(t.Context(), newEvent(ineligible, droppedEvent, []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	err = inst.handler.Handle(t.Context(), newEvent(eligible, deliveredEvent, []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	require.Equal(t, []string{deliveredEvent}, captured)
}

// TestAllowSet_CachesIneligibleOrg pins the cost of caching the negative
// answer: an organization that enables webhooks is picked up within a TTL
// rather than on the next event. That bound already applied to disabling them,
// and without it every event for an organization that will never receive
// webhooks — the large majority — would cost a query.
func TestAllowSet_CachesIneligibleOrg(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t, true)
	orgID := seedOrg(t, inst.conn, "app_123", false)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	err = testrepo.New(inst.conn).SetOrgWebhookConfig(t.Context(), testrepo.SetOrgWebhookConfigParams{
		OrganizationID:  orgID,
		SvixAppID:       conv.ToPGTextEmpty("app_123"),
		WebhooksEnabled: pgtype.Bool{Bool: true, Valid: true},
	})
	require.NoError(t, err)

	err = inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything)
}
