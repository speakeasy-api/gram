package platformmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type testNewModelEligibility struct {
	eligible bool
	err      error
}

func (e testNewModelEligibility) EligibleForPlatformMCP(_ context.Context, _ string) (bool, error) {
	return e.eligible, e.err
}

func TestAdmissionChecker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		gate        testGate
		eligibility testNewModelEligibility
		want        Admission
		wantErr     string
	}{
		{
			name:        "enabled",
			gate:        testGate{enabled: true},
			eligibility: testNewModelEligibility{eligible: true},
			want:        AdmissionEnabled,
		},
		{
			name:        "gate disabled",
			gate:        testGate{},
			eligibility: testNewModelEligibility{eligible: true},
			want:        AdmissionDisabled,
		},
		{
			name:        "eligibility disabled",
			gate:        testGate{enabled: true},
			eligibility: testNewModelEligibility{},
			want:        AdmissionDisabled,
		},
		{
			name:        "gate lookup failure is indeterminate",
			gate:        testGate{err: errors.New("unavailable")},
			eligibility: testNewModelEligibility{eligible: true},
			want:        AdmissionIndeterminate,
			wantErr:     "check platform mcp gate",
		},
		{
			name:        "eligibility lookup failure is indeterminate",
			gate:        testGate{enabled: true},
			eligibility: testNewModelEligibility{err: errors.New("unavailable")},
			want:        AdmissionIndeterminate,
			wantErr:     "check platform mcp new-model eligibility",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewAdmissionChecker(test.gate, test.eligibility).Evaluate(t.Context(), "organization", "organization-slug")
			if test.wantErr != "" {
				require.ErrorContains(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, test.want, got)
		})
	}
}

func TestAdmissionCheckerReturnsIndeterminateForInvalidOrCanceledContext(t *testing.T) {
	t.Parallel()

	checker := NewAdmissionChecker(testGate{enabled: true}, testNewModelEligibility{eligible: true})

	admission, err := checker.Evaluate(t.Context(), "", "organization")
	require.NoError(t, err)
	require.Equal(t, AdmissionIndeterminate, admission)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	admission, err = checker.Evaluate(ctx, "organization", "organization")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, AdmissionIndeterminate, admission)
}
