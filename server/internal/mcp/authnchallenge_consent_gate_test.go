package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestConsentToolFilteringEnabled pins the organization-admin product feature
// as the consent-screen tool picker's only enablement path.
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

	cases := []struct {
		name    string
		service *Service
		want    bool
	}{
		{"org feature off", &Service{platformFeatureChecker: checker(false)}, false},
		{"org feature on", &Service{platformFeatureChecker: checker(true)}, true},
		{"nil checker degrades to off", &Service{platformFeatureChecker: nil}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.service.consentToolFilteringEnabled(t.Context(), logger, orgID))
		})
	}
}
