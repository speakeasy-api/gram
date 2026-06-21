package memory

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/threadsource"
)

func TestExtractProvenanceSlack(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindSlack, "slack:T1:C123:171.001",
		[]byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001","user_id":"U456"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, threadsource.KindSlack, *got.Kind)
	require.NotNil(t, got.UserID)
	require.Equal(t, "U456", *got.UserID)
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "slack:T1:C123:171.001", *got.CorrelationID)
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceSlackMissingUser(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindSlack, "slack:T1:C123:171.001",
		[]byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001"}`))

	require.Nil(t, got.UserID)
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "slack:T1:C123:171.001", *got.CorrelationID)
}

func TestExtractProvenanceDashboard(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindDashboard, "0d9e0001-aaaa-bbbb-cccc-000000000001",
		[]byte(`{"user_id":"user_abc"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, threadsource.KindDashboard, *got.Kind)
	require.NotNil(t, got.UserID)
	require.Equal(t, "user_abc", *got.UserID)
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "0d9e0001-aaaa-bbbb-cccc-000000000001", *got.CorrelationID)
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceCronHasNoSpeaker(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindCron, "cron:f0e9d8c7-0000-0000-0000-000000000001",
		[]byte(`{"trigger_instance_id":"f0e9d8c7-0000-0000-0000-000000000001","schedule":"0 * * * *"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, threadsource.KindCron, *got.Kind)
	require.Nil(t, got.UserID, "automated triggers have no human speaker")
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "cron:f0e9d8c7-0000-0000-0000-000000000001", *got.CorrelationID)
}

func TestExtractProvenanceWakeHasNoSpeaker(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindWake, "wake:f0e9d8c7-0000-0000-0000-000000000002",
		[]byte(`{"trigger_instance_id":"f0e9d8c7-0000-0000-0000-000000000002"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, threadsource.KindWake, *got.Kind)
	require.Nil(t, got.UserID)
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "wake:f0e9d8c7-0000-0000-0000-000000000002", *got.CorrelationID)
}

func TestExtractProvenanceUnknownKindKeepsKindAndCorrelation(t *testing.T) {
	t.Parallel()

	// source_kind is an open enum: unknown kinds are recorded verbatim with
	// the correlation id but no speaker.
	got := extractProvenance("carrier-pigeon", "pigeon:coop-7", []byte(`{"user_id":"who"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, "carrier-pigeon", *got.Kind)
	require.Nil(t, got.UserID)
	require.NotNil(t, got.CorrelationID)
	require.Equal(t, "pigeon:coop-7", *got.CorrelationID)
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceMalformedRefJSON(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindSlack, "slack:T1:C123:171.001", []byte(`{not json`))

	require.NotNil(t, got.Kind)
	require.Equal(t, threadsource.KindSlack, *got.Kind)
	require.Nil(t, got.UserID)
	require.NotNil(t, got.CorrelationID, "correlation id does not depend on the ref payload")
	require.Equal(t, "slack:T1:C123:171.001", *got.CorrelationID)
}

func TestExtractProvenanceEmptyCorrelationIDIsNil(t *testing.T) {
	t.Parallel()

	got := extractProvenance(threadsource.KindSlack, "", []byte(`{"user_id":"U1"}`))

	require.Nil(t, got.CorrelationID)
}
