package gram

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

func TestPlatformMCPSetupResourcesStaticCallback(t *testing.T) {
	t.Parallel()

	outbound := &url.URL{Scheme: "https", Host: "outbound.example.com"}
	registration := &url.URL{Scheme: "https", Host: "registration.example.com"}
	for _, tt := range []struct {
		name         string
		registration *url.URL
		wantOrigin   *url.URL
	}{
		{name: "registration origin", registration: registration, wantOrigin: registration},
		{name: "nil registration uses outbound", registration: nil, wantOrigin: outbound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resources, err := platformMCPSetupResources(platformMCPConfig{
				AICatalog: aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())),

				CallbackOrigins: remotesessions.CallbackOrigins{
					Outbound: outbound, Registration: tt.registration, OrganizationHosts: nil,
				},
			})
			require.NoError(t, err)
			require.NotEmpty(t, resources)

			wantCallback := remotesessions.RemoteLoginCallbackURL(tt.wantOrigin)
			foundCallback := false
			for _, resource := range resources {
				require.Nil(t, resource.Render, resource.URI)
				if strings.Contains(resource.Body, wantCallback) {
					foundCallback = true
					require.Contains(t, resource.Text, wantCallback, resource.URI)
				}
				if tt.registration != nil {
					require.NotContains(t, resource.Text, remotesessions.RemoteLoginCallbackURL(outbound), resource.URI)
				}
			}
			require.True(t, foundCallback, "setup resources must contain callback %s", wantCallback)
		})
	}
}
