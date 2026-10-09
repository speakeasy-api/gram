package proxy

import (
	"context"
	"net"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestValidateRemoteMCPCatalogURL(t *testing.T) {
	t.Parallel()
	resolver := dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.23.45.67")}, nil
	}})
	option, err := guardian.WithInternalCatalogCIDR("10.23.45.67/32")
	require.NoError(t, err)
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(resolver), option)
	for _, tc := range []struct {
		url   string
		valid bool
	}{
		{"https://alpha.catalog.dev.speakeasy.com/okta/mcp", true},
		{"https://beta.catalog.dev.speakeasy.com:443/okta/mcp", true},
		{"http://alpha.catalog.dev.speakeasy.com/okta/mcp", false},
		{"https://alpha.catalog.dev.speakeasy.com:8443/okta/mcp", true},
		{"https://alternate.example.test/okta/mcp", true},
		{"https://10.23.45.67/okta/mcp", true},
		{"https://user@alternate.example.test/okta/mcp", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			_, err := ValidateRemoteMCPURL(t.Context(), policy, tc.url)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
