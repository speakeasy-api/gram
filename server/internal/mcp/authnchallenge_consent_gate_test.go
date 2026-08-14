package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestConsentToolFilteringEnabled pins the two enablement paths for the
// consent-screen tool picker: the staged rollout flag and the organization
// admin's product-feature opt-in, either of which turns it on.
func TestConsentToolFilteringEnabled(t *testing.T) {
	t.Parallel()

	const orgID = "org-1"
	logger := testenv.NewLogger(t)

	checker := func(enabled bool) func(ctx context.Context, organizationID string, f string) bool {
		return func(_ context.Context, organizationID string, f string) bool {
			require.Equal(t, orgID, organizationID)
			require.Equal(t, string(productfeatures.FeatureConsentToolFiltering), f)
			return enabled
		}
	}

	flagOn := &feature.InMemory{}
	flagOn.SetFlag(feature.FlagConsentToolFiltering, orgID, true)

	cases := []struct {
		name    string
		service *Service
		want    bool
	}{
		{"all off", &Service{features: &feature.InMemory{}, platformFeatureChecker: checker(false)}, false},
		{"flag on", &Service{features: flagOn, platformFeatureChecker: checker(false)}, true},
		{"org feature on", &Service{features: &feature.InMemory{}, platformFeatureChecker: checker(true)}, true},
		{"nil checker degrades to flag", &Service{features: &feature.InMemory{}, platformFeatureChecker: nil}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.service.consentToolFilteringEnabled(t.Context(), logger, orgID))
		})
	}
}
