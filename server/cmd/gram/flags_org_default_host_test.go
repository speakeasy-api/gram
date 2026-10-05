package gram

import (
	"flag"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

// orgDefaultHostCLIContext parses the organization default host flags from
// explicit values, so the test does not depend on the process environment.
func orgDefaultHostCLIContext(t *testing.T, legacy, newOrg string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("org-default-host", flag.ContinueOnError)
	require.NoError(t, (&cli.StringFlag{Name: legacyDefaultHostFlag, Value: legacy}).Apply(set))
	require.NoError(t, (&cli.StringFlag{Name: newOrgDefaultHostFlag, Value: newOrg}).Apply(set))
	return cli.NewContext(nil, set, nil)
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestOrgHostResolverFromCLIDefaultsKeepConfiguredURLs(t *testing.T) {
	t.Parallel()

	serverURL := mustParseURL(t, "https://ai.example.com")
	siteURL := mustParseURL(t, "https://dashboard.example.com")
	platformHosts := map[string]string{"app.example.com": "https://app.example.com"}

	resolver, err := orgHostResolverFromCLI(orgDefaultHostCLIContext(t, "", ""), serverURL, siteURL, "prod", platformHosts)
	require.NoError(t, err)

	null := pgtype.Text{String: "", Valid: false}
	require.Equal(t, "https://dashboard.example.com", resolver.SiteURL(null).String())
	require.Equal(t, "https://ai.example.com", resolver.ServerURL(null).String())
	require.Equal(t, null, resolver.NewOrganizationDefaultHost())
}

func TestOrgHostResolverFromCLIAppliesConfiguredHosts(t *testing.T) {
	t.Parallel()

	serverURL := mustParseURL(t, "https://ai.example.com")
	platformHosts := map[string]string{"app.example.com": "https://app.example.com"}

	resolver, err := orgHostResolverFromCLI(orgDefaultHostCLIContext(t, "https://app.example.com/", "https://ai.example.com"), serverURL, serverURL, "prod", platformHosts)
	require.NoError(t, err)

	null := pgtype.Text{String: "", Valid: false}
	require.Equal(t, "https://app.example.com", resolver.SiteURL(null).String())
	require.Equal(t, "https://app.example.com", resolver.ServerURL(null).String())
	require.Equal(t, pgtype.Text{String: "https://ai.example.com", Valid: true}, resolver.NewOrganizationDefaultHost())
	require.Equal(t, "https://ai.example.com", resolver.SiteURL(resolver.NewOrganizationDefaultHost()).String())
}

func TestOrgHostResolverFromCLIRejectsInvalidHosts(t *testing.T) {
	t.Parallel()

	serverURL := mustParseURL(t, "https://ai.example.com")
	platformHosts := map[string]string{"app.example.com": "https://app.example.com"}

	tests := []struct {
		name    string
		legacy  string
		newOrg  string
		wantErr string
	}{
		{name: "legacy host not served", legacy: "https://other.example.com", wantErr: "invalid --legacy-default-host: host other.example.com is neither the server URL host nor a platform host"},
		{name: "legacy path", legacy: "https://app.example.com/base", wantErr: "invalid --legacy-default-host: must be an origin without a path"},
		{name: "legacy HTTP", legacy: "http://app.example.com", wantErr: "invalid --legacy-default-host"},
		{name: "new org host not served", newOrg: "https://other.example.com", wantErr: "invalid --new-org-default-host: host other.example.com is neither the server URL host nor a platform host"},
		{name: "new org path", newOrg: "https://ai.example.com/base", wantErr: "invalid --new-org-default-host: must be an origin without a path"},
		{name: "new org bad scheme", newOrg: "ftp://ai.example.com", wantErr: "invalid --new-org-default-host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := orgHostResolverFromCLI(orgDefaultHostCLIContext(t, tt.legacy, tt.newOrg), serverURL, serverURL, "prod", platformHosts)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
