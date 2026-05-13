package outbox_relay_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/svix/svix-webhooks/go/models"

	"github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay"
	bgactivitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	svixtest "github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
)

func TestFetchEvents_EmptyOutbox(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Empty(t, result.Events)
	require.False(t, result.HasMore)
}

func TestFetchEvents_ReturnsPendingRows(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-id", true)
	for range 3 {
		seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	}

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	require.False(t, result.HasMore)
}

func TestFetchEvents_HasMoreProbe(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-id", true)
	for range 51 {
		seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	}

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 50)
	require.True(t, result.HasMore)
}

func TestFetchEvents_ExcludesProcessed(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-id", true)
	processedID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	pendingID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	err := bgactivitiesrepo.New(inst.conn).MarkOutboxRelayProcessed(ctx, bgactivitiesrepo.MarkOutboxRelayProcessedParams{
		OutboxID:      processedID,
		SvixMessageID: conv.ToPGTextEmpty("svix-123"),
	})
	require.NoError(t, err)

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, pendingID, result.Events[0].OutboxID)
}

func TestFetchEvents_ExcludesDeadLettered(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-id", true)
	deadID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	pendingID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	err := bgactivitiesrepo.New(inst.conn).MarkOutboxRelayDeadLettered(ctx, bgactivitiesrepo.MarkOutboxRelayDeadLetteredParams{
		OutboxID:  deadID,
		LastError: conv.ToPGTextEmpty("permanent error"),
	})
	require.NoError(t, err)

	result, err := inst.relay.FetchEvents(ctx, outbox_relay.FetchEventArgs{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, pendingID, result.Events[0].OutboxID)
}

func TestFilterNoopEvents_Empty(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	result, err := inst.relay.FilterNoopEvents(ctx, nil)
	require.NoError(t, err)
	require.Nil(t, result)
}

func TestFilterNoopEvents_PassesThroughEnabled(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-123", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-123", WebhooksEnabled: true},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, outboxID, result[0].OutboxID)

	// no noop row should have been written
	_, relayErr := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.ErrorIs(t, relayErr, pgx.ErrNoRows)
}

func TestFilterNoopEvents_NoSvixAppID(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "", false)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "", WebhooksEnabled: false},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	require.Empty(t, result)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.Noop)
}

func TestFilterNoopEvents_WebhooksDisabled(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	orgID := seedOrg(t, inst.conn, "app-123", false)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-123", WebhooksEnabled: false},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	require.Empty(t, result)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.Noop)
}

func TestFilterNoopEvents_FeatureFlagDisabled(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	// Org has svix configured but feature flag is NOT enabled.
	orgID := seedOrg(t, inst.conn, "app-123", true)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-123", WebhooksEnabled: true},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	require.Empty(t, result)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.Noop)
}

func TestFilterNoopEvents_Mixed(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	// Org A: webhooks enabled — event should pass through.
	orgA := seedOrg(t, inst.conn, "app-a", true)
	enableWebhooksFeature(t, inst.conn, orgA)
	idA := seedOutboxEntry(t, inst.conn, orgA, "test.event", payload)

	// Org B: no svix app ID — event should be noop'd.
	orgB := seedOrg(t, inst.conn, "", false)
	idB := seedOutboxEntry(t, inst.conn, orgB, "test.event", payload)

	// Org C: feature flag disabled — event should be noop'd.
	orgC := seedOrg(t, inst.conn, "app-c", true)
	idC := seedOutboxEntry(t, inst.conn, orgC, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: idA, OrganizationID: orgA, SvixAppID: "app-a", WebhooksEnabled: true},
		{OutboxID: idB, OrganizationID: orgB, SvixAppID: "", WebhooksEnabled: false},
		{OutboxID: idC, OrganizationID: orgC, SvixAppID: "app-c", WebhooksEnabled: true},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, idA, result[0].OutboxID)

	q := testrepo.New(inst.conn)
	_, errA := q.GetOutboxRelayState(ctx, idA)
	require.ErrorIs(t, errA, pgx.ErrNoRows)

	stateB, err := q.GetOutboxRelayState(ctx, idB)
	require.NoError(t, err)
	require.True(t, stateB.Noop)

	stateC, err := q.GetOutboxRelayState(ctx, idC)
	require.NoError(t, err)
	require.True(t, stateC.Noop)
}

func TestFilterNoopEvents_CachesPerOrg(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()
	payload := mustMarshal(t, map[string]any{"key": "value"})

	// Same org, two events: feature flag disabled → both noop'd.
	orgID := seedOrg(t, inst.conn, "app-123", true)
	id1 := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)
	id2 := seedOutboxEntry(t, inst.conn, orgID, "test.event", payload)

	events := []*outbox_relay.Event{
		{OutboxID: id1, OrganizationID: orgID, SvixAppID: "app-123", WebhooksEnabled: true},
		{OutboxID: id2, OrganizationID: orgID, SvixAppID: "app-123", WebhooksEnabled: true},
	}

	result, err := inst.relay.FilterNoopEvents(ctx, events)
	require.NoError(t, err)
	// Feature flag not enabled, so both events are noop'd.
	require.Empty(t, result)

	q := testrepo.New(inst.conn)
	s1, err := q.GetOutboxRelayState(ctx, id1)
	require.NoError(t, err)
	require.True(t, s1.Noop)

	s2, err := q.GetOutboxRelayState(ctx, id2)
	require.NoError(t, err)
	require.True(t, s2.Noop)
}

func TestRelayEvents_Empty(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	err := inst.relay.RelayEvents(ctx, nil)
	require.NoError(t, err)
}

func TestRelayEvents_SuccessfulDelivery(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	orgID := seedOrg(t, inst.conn, "app-success", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", mustMarshal(t, map[string]any{"action": "created"}))

	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything).
		Return(&models.MessageOut{
			Id:        "svix-msg-abc",
			EventType: "test.event",
			Payload:   map[string]any{"action": "created"},
			Timestamp: time.Now(),
		}, nil)

	err := inst.relay.RelayEvents(ctx, []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-success", WebhooksEnabled: true},
	})
	require.NoError(t, err)
	inst.svixSrv.AssertExpectations(t)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.ProcessedAt.Valid)
	require.False(t, state.DeadLettered)
	require.False(t, state.Noop)
	require.Equal(t, "svix-msg-abc", state.SvixMessageID.String)
}

func TestRelayEvents_PermanentError_400(t *testing.T) {
	t.Parallel()
	testRelayPermanentError(t, 400)
}

func TestRelayEvents_PermanentError_403(t *testing.T) {
	t.Parallel()
	testRelayPermanentError(t, 403)
}

func TestRelayEvents_PermanentError_404(t *testing.T) {
	t.Parallel()
	testRelayPermanentError(t, 404)
}

func testRelayPermanentError(t *testing.T, httpStatus int) {
	t.Helper()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	orgID := seedOrg(t, inst.conn, "app-perm", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", mustMarshal(t, map[string]any{"x": 1}))

	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything).
		Return(nil, &svixtest.HTTPStatusError{Code: httpStatus})

	err := inst.relay.RelayEvents(ctx, []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-perm", WebhooksEnabled: true},
	})
	require.NoError(t, err) // per-row errors are logged, not returned
	inst.svixSrv.AssertExpectations(t)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.DeadLettered)
	require.False(t, state.ProcessedAt.Valid)
	require.True(t, state.LastError.Valid)
}

func TestRelayEvents_TransientError_429(t *testing.T) {
	t.Parallel()
	testRelayTransientError(t, 429)
}

func TestRelayEvents_TransientError_500(t *testing.T) {
	t.Parallel()
	testRelayTransientError(t, 500)
}

func testRelayTransientError(t *testing.T, httpStatus int) {
	t.Helper()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	orgID := seedOrg(t, inst.conn, "app-trans", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", mustMarshal(t, map[string]any{"x": 1}))

	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything).
		Return(nil, &svixtest.HTTPStatusError{Code: httpStatus})

	err := inst.relay.RelayEvents(ctx, []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-trans", WebhooksEnabled: true},
	})
	require.NoError(t, err)
	inst.svixSrv.AssertExpectations(t)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.False(t, state.DeadLettered)
	require.False(t, state.ProcessedAt.Valid)
	require.Equal(t, int32(1), state.Attempts)
	require.True(t, state.LastError.Valid)
}

func TestRelayEvents_MaxAttemptsExceeded(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	orgID := seedOrg(t, inst.conn, "app-maxretry", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", mustMarshal(t, map[string]any{"x": 1}))

	// Pre-seed 9 failed attempts — next failure (attempt 10) should dead-letter.
	preloadAttempts(t, inst.conn, outboxID, 9)

	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything).
		Return(nil, &svixtest.HTTPStatusError{Code: 500})

	err := inst.relay.RelayEvents(ctx, []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-maxretry", WebhooksEnabled: true},
	})
	require.NoError(t, err)
	inst.svixSrv.AssertExpectations(t)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.DeadLettered)
	require.False(t, state.ProcessedAt.Valid)
}

func TestRelayEvents_InvalidPayload(t *testing.T) {
	t.Parallel()

	inst := newRelayTestInstance(t)
	ctx := t.Context()

	orgID := seedOrg(t, inst.conn, "app-badpayload", true)
	enableWebhooksFeature(t, inst.conn, orgID)
	// JSON array is valid JSONB but fails json.Unmarshal to map[string]any.
	outboxID := seedOutboxEntry(t, inst.conn, orgID, "test.event", []byte(`[1, 2, 3]`))

	err := inst.relay.RelayEvents(ctx, []*outbox_relay.Event{
		{OutboxID: outboxID, OrganizationID: orgID, SvixAppID: "app-badpayload", WebhooksEnabled: true},
	})
	require.NoError(t, err)
	// No Svix call should have been made.
	inst.svixSrv.AssertExpectations(t)

	state, err := testrepo.New(inst.conn).GetOutboxRelayState(ctx, outboxID)
	require.NoError(t, err)
	require.True(t, state.DeadLettered)
	require.True(t, state.LastError.Valid)
	require.Contains(t, state.LastError.String, "invalid payload")
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
