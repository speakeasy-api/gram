package admin

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/setupguides"
	"github.com/stretchr/testify/require"
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

func TestPromotionCandidateViewCarriesSetupGuides(t *testing.T) {
	t.Parallel()
	view := promotionCandidateView(catalogpromotion.Candidate{
		Status: "candidate", SetupGuides: []setupguides.Guide{{Slug: "slack", Title: "Slack"}},
		SetupGuidesVersion: "v0.0.1", EndpointGuideSlug: "slack",
	})
	require.Len(t, view.SetupGuides, 1)
	require.Equal(t, "slack", view.SetupGuides[0].Slug)
	require.Equal(t, "Slack", view.SetupGuides[0].Title)
	require.Equal(t, "v0.0.1", *view.SetupGuidesVersion)
	require.Equal(t, "slack", *view.EndpointGuideSlug)

	empty := promotionCandidateView(catalogpromotion.Candidate{Status: "promoted"})
	require.Nil(t, empty.SetupGuides)
	require.Nil(t, empty.SetupGuidesVersion)
	require.Nil(t, empty.EndpointGuideSlug)
}

func TestPromotionLengthErrorsGiveSafeFieldGuidance(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{catalogpromotion.ErrPublicTitleTooLong, catalogpromotion.ErrPublicDescriptionTooLong} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			err := promotionError(errors.Join(catalogpromotion.ErrInvalid, cause, errors.New("private rejected value")))
			var shared *oops.ShareableError
			require.ErrorAs(t, err, &shared)
			require.Equal(t, oops.CodeBadRequest, shared.Code)
			require.Contains(t, err.Error(), cause.Error())
			require.NotContains(t, err.Error(), "private rejected value")
		})
	}
}
