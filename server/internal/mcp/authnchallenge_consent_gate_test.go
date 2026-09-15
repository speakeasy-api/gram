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

	checker := func(_ context.Context, organizationID string, f string) bool {
		require.Equal(t, orgID, organizationID)
		require.Equal(t, string(productfeatures.FeatureConsentToolFiltering), f)
		return true
	}

	service := &Service{platformFeatureChecker: checker}
	require.True(t, service.consentToolFilteringEnabled(t.Context(), logger, orgID))
}
