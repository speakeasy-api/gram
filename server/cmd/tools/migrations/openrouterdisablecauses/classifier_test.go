package openrouterdisablecauses

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyComposesCanonicalCauses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		projection Projection
		want       []string
	}{
		{name: "enabled", projection: Projection{LegacyDisabled: false, Trial: TrialDemoted, Billing: BillingInactive, Admin: AdminDisabled}, want: []string{}},
		{name: "admin", projection: Projection{LegacyDisabled: true, Admin: AdminDisabled}, want: []string{CauseAdminLock}},
		{name: "trial", projection: Projection{LegacyDisabled: true, Trial: TrialDemoted}, want: []string{CauseTrialDemotion}},
		{name: "billing", projection: Projection{LegacyDisabled: true, Billing: BillingInactive}, want: []string{CauseBillingInactive}},
		{name: "admin and trial", projection: Projection{LegacyDisabled: true, Admin: AdminDisabled, Trial: TrialDemoted}, want: []string{CauseAdminLock, CauseTrialDemotion}},
		{name: "admin and billing", projection: Projection{LegacyDisabled: true, Admin: AdminDisabled, Billing: BillingInactive}, want: []string{CauseAdminLock, CauseBillingInactive}},
		{name: "trial and billing", projection: Projection{LegacyDisabled: true, Trial: TrialDemoted, Billing: BillingInactive}, want: []string{CauseTrialDemotion, CauseBillingInactive}},
		{name: "all", projection: Projection{LegacyDisabled: true, Admin: AdminDisabled, Trial: TrialDemoted, Billing: BillingInactive}, want: []string{CauseAdminLock, CauseTrialDemotion, CauseBillingInactive}},
		{name: "latest admin enable removes evidence", projection: Projection{LegacyDisabled: true, Admin: AdminEnabled, Trial: TrialDemoted}, want: []string{CauseTrialDemotion}},
		{name: "active billing adds no evidence", projection: Projection{LegacyDisabled: true, Admin: AdminDisabled, Billing: BillingActive}, want: []string{CauseAdminLock}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tt.projection)
			require.True(t, got.Classified)
			require.Empty(t, got.AmbiguousReason)
			require.Equal(t, tt.want, got.Causes)
		})
	}
}

func TestClassifyLeavesUnsafeRowsAmbiguous(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		projection Projection
		wantReason string
	}{
		{name: "no provenance", projection: Projection{LegacyDisabled: true}, wantReason: AmbiguousNoProvenance},
		{name: "contradictory trial", projection: Projection{LegacyDisabled: true, Trial: TrialContradictory, Admin: AdminDisabled}, wantReason: AmbiguousTrialProjection},
		{name: "inconsistent billing", projection: Projection{LegacyDisabled: true, Billing: BillingInconsistent, Admin: AdminDisabled}, wantReason: AmbiguousBillingProjection},
		{name: "malformed audit", projection: Projection{LegacyDisabled: true, Admin: AdminMalformed, Trial: TrialDemoted}, wantReason: AmbiguousAdminAudit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tt.projection)
			require.False(t, got.Classified)
			require.Nil(t, got.Causes)
			require.Equal(t, tt.wantReason, got.AmbiguousReason)
		})
	}
}

func TestCanonicalizeCausesDeduplicatesAndOrders(t *testing.T) {
	t.Parallel()

	got, err := CanonicalizeCauses([]string{CauseBillingInactive, CauseAdminLock, CauseTrialDemotion, CauseAdminLock})
	require.NoError(t, err)
	require.Equal(t, []string{CauseAdminLock, CauseTrialDemotion, CauseBillingInactive}, got)

	_, err = CanonicalizeCauses([]string{"not_a_cause"})
	require.Error(t, err)
}
