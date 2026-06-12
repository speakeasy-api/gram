package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractProvenanceSlack(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindSlack, []byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001","user_id":"U456"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, sourceKindSlack, *got.Kind)
	require.NotNil(t, got.UserID)
	require.Equal(t, "U456", *got.UserID)
	require.NotNil(t, got.Channel)
	require.Equal(t, "C123", *got.Channel)
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceSlackMissingUser(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindSlack, []byte(`{"team_id":"T1","channel_id":"C123","thread_id":"171.001"}`))

	require.Nil(t, got.UserID)
	require.NotNil(t, got.Channel)
	require.Equal(t, "C123", *got.Channel)
}

func TestExtractProvenanceDashboard(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindDashboard, []byte(`{"user_id":"user_abc"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, sourceKindDashboard, *got.Kind)
	require.NotNil(t, got.UserID)
	require.Equal(t, "user_abc", *got.UserID)
	require.Nil(t, got.Channel, "dashboard has no channel concept")
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceCron(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindCron, []byte(`{"trigger_instance_id":"f0e9d8c7-0000-0000-0000-000000000001","schedule":"0 * * * *"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, sourceKindCron, *got.Kind)
	require.Nil(t, got.UserID, "automated triggers have no human speaker")
	require.NotNil(t, got.Channel)
	require.Equal(t, "f0e9d8c7-0000-0000-0000-000000000001", *got.Channel)
}

func TestExtractProvenanceWake(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindWake, []byte(`{"trigger_instance_id":"f0e9d8c7-0000-0000-0000-000000000002"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, sourceKindWake, *got.Kind)
	require.Nil(t, got.UserID)
	require.NotNil(t, got.Channel)
	require.Equal(t, "f0e9d8c7-0000-0000-0000-000000000002", *got.Channel)
}

func TestExtractProvenanceUnknownKindKeepsKindOnly(t *testing.T) {
	t.Parallel()

	got := extractProvenance("carrier-pigeon", []byte(`{"user_id":"who"}`))

	require.NotNil(t, got.Kind)
	require.Equal(t, "carrier-pigeon", *got.Kind)
	require.Nil(t, got.UserID)
	require.Nil(t, got.Channel)
	require.NotNil(t, got.Timestamp)
}

func TestExtractProvenanceMalformedRefJSON(t *testing.T) {
	t.Parallel()

	got := extractProvenance(sourceKindSlack, []byte(`{not json`))

	require.NotNil(t, got.Kind)
	require.Equal(t, sourceKindSlack, *got.Kind)
	require.Nil(t, got.UserID)
	require.Nil(t, got.Channel)
}
