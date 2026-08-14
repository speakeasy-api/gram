package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
)

type testNewModelEligibility struct {
	eligible bool
	err      error
}

func (e testNewModelEligibility) EligibleForPlatformMCP(_ context.Context, _ string) (bool, error) {
	return e.eligible, e.err
}

type testEvaluationProvider struct {
	evaluation feature.Evaluation
}

func (p testEvaluationProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, nil
}

func (p testEvaluationProvider) IsFlagEnabledLocal(context.Context, feature.Flag, string, map[string]string, map[string]string) (bool, error) {
	return false, nil
}

func (p testEvaluationProvider) FlagPayload(context.Context, feature.Flag, string, map[string]string) ([]byte, error) {
	return nil, nil
}

func (p testEvaluationProvider) EvaluateFlag(context.Context, feature.Flag, string, map[string]string) (feature.Evaluation, error) {
	return p.evaluation, nil
}

func TestAdmissionChecker(t *testing.T) {
	t.Parallel()

	rollout := &feature.InMemory{}
	rollout.SetFlag(feature.FlagPlatformMCP, "organization-enabled", true)
	rollout.SetFlag(feature.FlagPlatformMCP, "organization-disabled", false)

	tests := []struct {
		name         string
		organization string
		capability   testCapabilityChecker
		eligibility  testNewModelEligibility
		want         Admission
		wantErr      string
	}{
		{
			name:         "enabled",
			organization: "organization-enabled",
			capability:   testCapabilityChecker{enabled: true},
			eligibility:  testNewModelEligibility{eligible: true},
			want:         AdmissionEnabled,
		},
		{
			name:         "capability disabled",
			organization: "organization-enabled",
			capability:   testCapabilityChecker{},
			eligibility:  testNewModelEligibility{eligible: true},
			want:         AdmissionDisabled,
		},
		{
			name:         "rollout disabled",
			organization: "organization-disabled",
			capability:   testCapabilityChecker{enabled: true},
			eligibility:  testNewModelEligibility{eligible: true},
			want:         AdmissionDisabled,
		},
		{
			name:         "rollout missing is indeterminate",
			organization: "organization-missing",
			capability:   testCapabilityChecker{enabled: true},
			eligibility:  testNewModelEligibility{eligible: true},
			want:         AdmissionIndeterminate,
		},
		{
			name:         "eligibility disabled",
			organization: "organization-enabled",
			capability:   testCapabilityChecker{enabled: true},
			eligibility:  testNewModelEligibility{},
			want:         AdmissionDisabled,
		},
		{
			name:         "capability lookup failure is indeterminate",
			organization: "organization-enabled",
			capability:   testCapabilityChecker{err: errors.New("unavailable")},
			eligibility:  testNewModelEligibility{eligible: true},
			want:         AdmissionIndeterminate,
			wantErr:      "check platform mcp capability",
		},
		{
			name:         "eligibility lookup failure is indeterminate",
			organization: "organization-enabled",
			capability:   testCapabilityChecker{enabled: true},
			eligibility:  testNewModelEligibility{err: errors.New("unavailable")},
			want:         AdmissionIndeterminate,
			wantErr:      "check platform mcp new-model eligibility",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewAdmissionChecker(test.capability, rollout, test.eligibility).Evaluate(t.Context(), test.organization, "organization-slug")
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestAdmissionCheckerRequiresExplicitEnabledRollout(t *testing.T) {
	t.Parallel()

	checker := NewAdmissionChecker(
		testCapabilityChecker{enabled: true},
		testEvaluationProvider{evaluation: feature.Evaluation(99)},
		testNewModelEligibility{eligible: true},
	)

	admission, err := checker.Evaluate(t.Context(), "organization", "organization")
	require.NoError(t, err)
	require.Equal(t, AdmissionIndeterminate, admission)
}

func TestAdmissionCheckerReturnsIndeterminateForInvalidOrCanceledContext(t *testing.T) {
	t.Parallel()

	rollout := &feature.InMemory{}
	rollout.SetFlag(feature.FlagPlatformMCP, "organization", true)
	checker := NewAdmissionChecker(testCapabilityChecker{enabled: true}, rollout, testNewModelEligibility{eligible: true})

	admission, err := checker.Evaluate(t.Context(), "", "organization")
	require.NoError(t, err)
	require.Equal(t, AdmissionIndeterminate, admission)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	admission, err = checker.Evaluate(ctx, "organization", "organization")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, AdmissionIndeterminate, admission)
}
