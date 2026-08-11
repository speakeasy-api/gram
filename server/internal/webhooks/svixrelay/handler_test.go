package svixrelay_test

import (
	"encoding/json"
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

// TestHandle_PreservesLargeNumbersInPayload guards the payload's opacity. The
// handler has to decode it to hand the SDK a map, and a plain Unmarshal turns
// every number into a float64 — which rounds anything past 2^53 and rewrites
// large magnitudes in exponent notation, silently and without an error. The
// values below are chosen to land either side of that: 2^53+1 is off by one,
// and the 20-digit id loses its last three digits entirely.
func TestHandle_PreservesLargeNumbersInPayload(t *testing.T) {
	t.Parallel()

	inst := newHandlerTestInstance(t)
	orgID := seedOrg(t, inst.conn, "app_123", true)

	const payload = `{"id":"a","just_past_float64":9007199254740993,"external_id":12345678901234567890,"big_exp":1e21,"ordinary":5,"fractional":1.5}`

	rec := &svixRecorder{}
	inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
		Return(&models.MessageOut{Id: "msg_1"}, nil).
		Run(rec.record)

	err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(payload)), gcp.MessageMetadata{})
	require.NoError(t, err)

	calls := rec.observed()
	require.Len(t, calls, 1)
	require.NotNil(t, calls[0].msg)

	// Compared as literals, deliberately. Anything that parses the two sides
	// back into interface{} — JSONEq included — turns both into float64 and
	// rounds the expectation to match whatever it is handed, so the assertion
	// would hold no matter how badly the value had been mangled.
	literal := func(key string) string {
		v, ok := calls[0].msg.Payload[key]
		require.True(t, ok, "%s missing from the delivered payload", key)

		n, ok := v.(json.Number)
		require.Truef(t, ok, "%s arrived as %T, so its original literal is already gone", key, v)

		return n.String()
	}

	require.Equal(t, "9007199254740993", literal("just_past_float64"))
	require.Equal(t, "12345678901234567890", literal("external_id"))
	require.Equal(t, "1e21", literal("big_exp"))
	require.Equal(t, "5", literal("ordinary"))
	require.Equal(t, "1.5", literal("fractional"))
	require.Equal(t, "a", calls[0].msg.Payload["id"])
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

	for _, status := range []int{400, 404, 422} {
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

// TestHandle_NacksCredentialFailures covers the codes that say the relay's key
// is wrong rather than the message. They are indistinguishable from a permanent
// rejection at the call site and are answered for every event alike, so acking
// them would drain the whole topic into nothing for as long as the key stayed
// rotated or unset — the one 4xx class where the response is guaranteed to
// change once someone fixes the deployment.
func TestHandle_NacksCredentialFailures(t *testing.T) {
	t.Parallel()

	for _, status := range []int{401, 403} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			inst := newHandlerTestInstance(t)
			orgID := seedOrg(t, inst.conn, "app_123", true)

			inst.svixSrv.On("CreateMessage", mock.Anything, mock.Anything, mock.Anything).
				Return(nil, &svixtest.HTTPStatusError{Code: status, Msg: "unauthorized"})

			err := inst.handler.Handle(t.Context(), newEvent(orgID, uuid.NewString(), []byte(validPayload)), gcp.MessageMetadata{})
			require.Error(t, err,
				"a bad api key is fixed by an operator, not by discarding every event that hits it")
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
