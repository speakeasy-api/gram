package admin

import (
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPromotionCandidateViewOverrideCapability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, reason string
		eligible, allowed    bool
	}{
		{name: "health override", status: "candidate", reason: "errors_observed", allowed: true},
		{name: "safe source required", status: "candidate", reason: "disabled"},
		{name: "restoration required", status: "rejected", reason: "errors_observed"},
		{name: "already eligible", status: "candidate", reason: "eligible", eligible: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			view := promotionCandidateView(catalogpromotion.Candidate{Status: tc.status, Reason: tc.reason, Eligible: tc.eligible})
			require.Equal(t, tc.allowed, view.CanOverrideEligibility)
		})
	}
}
