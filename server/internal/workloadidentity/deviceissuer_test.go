package workloadidentity_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func TestDeviceIssuerURL_RoundTrips(t *testing.T) {
	t.Parallel()

	issuer := workloadidentity.DeviceIssuerURL("https://app.example.com/", "org-123")
	require.Equal(t, "https://app.example.com/workload/devices/org-123", issuer)

	organizationID, ok := workloadidentity.DeviceIssuerOrganization("https://app.example.com", issuer)
	require.True(t, ok)
	require.Equal(t, "org-123", organizationID)
}

func TestDeviceIssuerOrganization_RefusesOtherURLs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		issuer string
	}{
		{name: "other host", issuer: "https://attacker.example/workload/devices/org-123"},
		{name: "platform issuer", issuer: "https://token.actions.githubusercontent.com"},
		{name: "no organization", issuer: "https://app.example.com/workload/devices/"},
		{name: "extra segment", issuer: "https://app.example.com/workload/devices/org-123/extra"},
		{name: "query", issuer: "https://app.example.com/workload/devices/org-123?x=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, ok := workloadidentity.DeviceIssuerOrganization("https://app.example.com", tc.issuer)
			require.False(t, ok)
		})
	}
}

func TestDeviceIssuerPrefix_MatchesCanonicalSpellings(t *testing.T) {
	t.Parallel()

	for _, issuer := range []string{
		"https://app.example.com/workload/devices/org-123",
		"https://app.example.com:443/workload/devices/org-123",
		"https://APP.example.com/workload/devices/org-123",
		"https://app.example.com/workload/devices/org-123/",
	} {
		require.True(t, workloadidentity.IsUnderDeviceIssuerPrefix("https://app.example.com", issuer), issuer)
		organizationID, ok := workloadidentity.DeviceIssuerOrganization("https://app.example.com", issuer)
		require.True(t, ok, issuer)
		require.Equal(t, "org-123", organizationID, issuer)
	}

	require.True(t, workloadidentity.IsUnderDeviceIssuerPrefix("https://app.example.com", "https://app.example.com/workload/devices"))
	require.False(t, workloadidentity.IsUnderDeviceIssuerPrefix("https://app.example.com", "https://app.example.com/workload/devices-other/org-123"))
	require.False(t, workloadidentity.IsUnderDeviceIssuerPrefix("https://app.example.com", "https://app.example.com:8443/workload/devices/org-123"))
}
