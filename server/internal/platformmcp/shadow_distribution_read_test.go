package platformmcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestAggregateAdmissionStateUsesMostRestrictiveResult(t *testing.T) {
	t.Parallel()

	require.Equal(t, admission.StateCovered, aggregateAdmissionState(admission.StateEmptyAudience, admission.StateCovered))
	require.Equal(t, admission.StateCovered, aggregateAdmissionState(admission.StateNotRequired, admission.StateCovered))
	require.Equal(t, admission.StateApprovalRequired, aggregateAdmissionState(admission.StateCovered, admission.StateApprovalRequired))
}

func TestUnavailableDistributionAdmissionIsIncompleteAndBounded(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, time.September, 10, 1, 2, 3, 0, time.UTC)
	result := unavailableDistributionAdmission(func() time.Time { return checkedAt })

	require.Equal(t, DistributionAdmissionUnavailable, result.State)
	require.Empty(t, result.Mode)
	require.Equal(t, admission.MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, result.MissingAudienceCounts)
	require.Equal(t, checkedAt.Format(time.RFC3339Nano), result.CheckedAt)
	require.False(t, result.Complete)
}

type stubDistributionAdmissionReader struct {
	plugin DistributionAdmission
	target DistributionAdmission
}

func (s stubDistributionAdmissionReader) ForPlugin(_ context.Context, _ string, _, _ uuid.UUID) DistributionAdmission {
	return s.plugin
}

func (s stubDistributionAdmissionReader) ForTarget(_ context.Context, _ string, _ uuid.UUID, _ string) DistributionAdmission {
	return s.target
}

func (s stubDistributionAdmissionReader) NotApplicable(_ context.Context, _ string, _ uuid.UUID) DistributionAdmission {
	return DistributionAdmission{State: DistributionAdmissionNotApplicable, Mode: "legacy", MissingAudienceCounts: admission.MissingAudienceCounts{Everyone: 0, Roles: 0, Groups: 0, Attributes: 0, Users: 0}, CheckedAt: "2026-09-10T00:00:00Z", Complete: true}
}
