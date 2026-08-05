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

	inst := newHandlerTestInstance(t)
	ineligible := seedOrg(t, inst.conn, "app_123", false)
	eligible := seedOrg(t, inst.conn, "app_456", true)

	droppedEvent := uuid.NewString()
	deliveredEvent := uuid.NewString()

	rec := &svixRecorder{}
	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
		Return(&models.MessageOut{Id: "msg_1"}, nil).
		Run(rec.record)

	err := inst.handler.Handle(t.Context(), newEvent(ineligible, droppedEvent, []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	err = inst.handler.Handle(t.Context(), newEvent(eligible, deliveredEvent, []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	calls := rec.observed()
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].msg)
	require.NotNil(t, calls[0].msg.EventId)
	require.Equal(t, deliveredEvent, *calls[0].msg.EventId)
}

// TestAllowSet_RoutesEachOrgToItsOwnApp is the property a routing bug would
// break and nothing else here would catch: every other test seeds a single
// organization, so a resolver that returned the same application for everyone
// would satisfy all of them.
//
// The two organizations are interleaved rather than handled in sequence,
// because the cache and its singleflight are shared process-wide — resolving
// one organization must not populate or answer for another.
func TestAllowSet_RoutesEachOrgToItsOwnApp(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	first := seedOrg(t, inst.conn, "app_first", true)
	second := seedOrg(t, inst.conn, "app_second", true)

	rec := &svixRecorder{}
	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
		Return(&models.MessageOut{Id: "msg_1"}, nil).
		Run(rec.record)

	events := []struct {
		orgID string
		app   string
	}{
		{first, "app_first"},
		{second, "app_second"},
		{first, "app_first"},   // served from cache
		{second, "app_second"}, // served from cache
	}

	want := map[string]string{}
	for _, ev := range events {
		eventID := uuid.NewString()
		want[eventID] = ev.app

		err := inst.handler.Handle(t.Context(), newEvent(ev.orgID, eventID, []byte(validPayload)), gcp.MessageMetadata{})
		require.NoError(t, err)
	}

	// Event id to the app it was addressed to, so a swap is visible as a
	// mismatch on a named event rather than as a count that still adds up.
	routed := map[string]string{}
	for _, call := range rec.observed() {
		require.NotNil(t, call.msg)
		require.NotNil(t, call.msg.EventId)

		routed[*call.msg.EventId] = call.appID
	}

	require.Equal(t, want, routed)
}

// TestAllowSet_CachesIneligibleOrg pins the cost of caching the negative
// answer: an organization that enables webhooks is picked up within a TTL
// rather than on the next event. That bound already applied to disabling them,
// and without it every event for an organization that will never receive
// webhooks — the large majority — would cost a query.
func TestAllowSet_CachesIneligibleOrg(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
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
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
}
