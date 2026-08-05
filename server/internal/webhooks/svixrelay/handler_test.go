package svixrelay_test

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/svix/svix-webhooks/go/models"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	svixtest "github.com/speakeasy-api/gram/server/internal/thirdparty/svix/svixtest"
)

const validPayload = `{"id":"a","action":"asset.create"}`

func TestHandle_DeliversForEligibleOrg(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "app_123", true)
	eventID := uuid.NewString()

	rec := &svixRecorder{}
	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
		Return(&models.MessageOut{Id: "msg_1"}, nil).
		Run(rec.record)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, eventID, []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	calls := rec.observed()
	require.Len(t, calls, 1)

	require.Equal(t, "app_123", calls[0].appID,
		"the event is addressed to the Svix application configured on its own organization")

	in := calls[0].msg
	require.NotNil(t, in)
	require.NotNil(t, in.EventId)
	require.Equal(t, eventID, *in.EventId,
		"the envelope's event id is what makes redelivery idempotent on Svix's side")
	require.Equal(t, "audit_log.asset_event_v1", in.EventType)
	require.Equal(t, "asset.create", in.Payload["action"])
}

func TestHandle_DropsEventMissingIdentifiers(t *testing.T) {
	t.Parallel()

	t.Run("no organization id", func(t *testing.T) {
		t.Parallel()

		inst := newHandlerTestInstance(t)

		err := inst.handler.Handle(t.Context(), newEvent("", uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
		require.NoError(t, err, "no organization can be resolved on a later attempt either")
		inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("no event id", func(t *testing.T) {
		t.Parallel()

		// Eligible org, so reaching Svix would mean the guard did not fire —
		// and delivering without an idempotency key makes each redelivery a
		// duplicate webhook instead of a 409.
		inst := newHandlerTestInstance(t)
		orgID := seedOrg(t, inst.conn, "app_123", true)

		err := inst.handler.Handle(t.Context(), newEvent(orgID, "", []byte(validPayload)), gcp.MessageMetadata{})
		require.NoError(t, err)
		inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
	})
}

func TestHandle_DropsWhenOrgHasNoSvixApp(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "", true)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err, "an ineligible org is an acknowledged drop, not a retry")
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
}

func TestHandle_DropsWhenWebhooksDisabled(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "app_123", false)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
}

func TestHandle_DropsUnknownOrg(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)

	err := inst.handler.Handle(t.Context(), newEvent(uuid.NewString(), uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err)
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
}

func TestHandle_DropsUnreadablePayload(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "app_123", true)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte("not json")), gcp.MessageMetadata{})
	require.NoError(t, err, "a malformed payload will not become well-formed on redelivery")
	inst.svixSrv.AssertNotCalled(t, "CreateMessage", mock.Anything, mock.Anything, mock.Anything)
}

// TestHandle_AcksDuplicate is the behaviour change from the old Temporal relay.
// Svix answers a repeated eventId with 409, and because event_id is stable
// across redeliveries that response means the event is already there. The old
// classifier treated it as a permanent 4xx and dead-lettered it, which would
// turn every at-least-once redelivery into a lost event here.
func TestHandle_AcksDuplicate(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "app_123", true)

	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &svixtest.HTTPStatusError{Code: 409, Msg: "conflict"})

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
	require.NoError(t, err, "409 means already delivered, which is success seen twice")
}

func TestHandle_AcksPermanentRejections(t *testing.T) {
	t.Parallel()

	for _, status := range []int{400, 403, 404, 422} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			inst := newHandlerTestInstance(t)
			orgID := seedOrg(t, inst.conn, "app_123", true)

			inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
				Return(nil, &svixtest.HTTPStatusError{Code: status, Msg: "rejected"})

			err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
			require.NoError(t, err,
				"nacking would burn the whole delivery budget resending something Svix already refused")
		})
	}
}

func TestHandle_NacksTransientFailures(t *testing.T) {
	t.Parallel()

	for _, status := range []int{408, 429, 500, 502, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			inst := newHandlerTestInstance(t)
			orgID := seedOrg(t, inst.conn, "app_123", true)

			inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
				Return(nil, &svixtest.HTTPStatusError{Code: status, Msg: "try again"})

			err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
			require.Error(t, err,
				"returning the error nacks the message so the subscription's retry policy takes over")
		})
	}
}
