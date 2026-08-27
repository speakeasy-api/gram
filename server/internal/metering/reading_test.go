package metering_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/metering"
)

func TestScopeConstructorsExposeOnlyApplicableOwnership(t *testing.T) {
	t.Parallel()
	organizationID := "org-" + uuid.NewString()
	projectID := uuid.New()

	organizationScope := metering.OrganizationScope(organizationID)
	require.Equal(t, organizationID, organizationScope.OrganizationID())
	_, hasProject := organizationScope.ProjectID()
	require.False(t, hasProject)

	projectScope := metering.ProjectScope(organizationID, projectID)
	require.Equal(t, organizationID, projectScope.OrganizationID())
	storedProjectID, hasProject := projectScope.ProjectID()
	require.True(t, hasProject)
	require.Equal(t, projectID, storedProjectID)
}

func TestReadingIDUsesInitialV1Contract(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	reading, err := metering.NewUsage(metering.UsageInput{
		Meter: metering.AgentSessionStorage(),
		Scope: metering.ProjectScope(
			"org-test",
			uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		),
		OperationID: "chat_message:00000000-0000-0000-0000-000000000002",
		Value:       1,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})
	require.NoError(t, err)

	require.Equal(t, uuid.MustParse("c30a00ac-8471-56c4-b59e-f09ebd00ca21"), reading.ID())
}

func TestNewUsageRejectsUnknownDefinition(t *testing.T) {
	t.Parallel()
	definition := metering.Definition{}
	now := time.Now().UTC()

	_, err := metering.NewUsage(metering.UsageInput{
		Meter:       definition,
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "operation:" + uuid.NewString(),
		Value:       1,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})

	require.Error(t, err)
}

func TestNewUsageRejectsNonPositiveValue(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	_, err := metering.NewUsage(metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID: "operation:" + uuid.NewString(),
		Value:       0,
		OccurredAt:  now,
		ProducedAt:  now,
		Source:      "test",
		Attributes:  nil,
	})

	require.Error(t, err)
}

func TestNewAdjustmentRequiresTargetAndReason(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	base := metering.AdjustmentInput{
		Meter:             metering.AgentSessionStorage(),
		Scope:             metering.ProjectScope("org-"+uuid.NewString(), uuid.New()),
		OperationID:       "adjustment:" + uuid.NewString(),
		Value:             -1,
		OccurredAt:        now,
		ProducedAt:        now,
		CorrectsReadingID: uuid.Nil,
		Reason:            "",
		Source:            "test",
		Attributes:        nil,
	}

	_, err := metering.NewAdjustment(base)
	require.Error(t, err)

	base.CorrectsReadingID = uuid.New()
	_, err = metering.NewAdjustment(base)
	require.Error(t, err)
}
