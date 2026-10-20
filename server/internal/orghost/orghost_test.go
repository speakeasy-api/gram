package orghost_test

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orghost"
)

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func newResolver(t *testing.T, legacy string) *orghost.Resolver {
	t.Helper()
	var legacyURL *url.URL
	if legacy != "" {
		legacyURL = mustParse(t, legacy)
	}
	return orghost.New(orghost.Config{
		ServerURL:                  mustParse(t, "https://ai.example.com"),
		SiteURL:                    mustParse(t, "https://dashboard.ai.example.com"),
		PlatformHosts:              map[string]string{"app.example.com": "https://app.example.com"},
		LegacyDefaultHost:          legacyURL,
		NewOrganizationDefaultHost: nil,
	})
}

func stored(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func TestSiteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		legacy      string
		defaultHost pgtype.Text
		want        string
	}{
		{name: "null uses the legacy host", legacy: "https://app.example.com", defaultHost: pgtype.Text{String: "", Valid: false}, want: "https://app.example.com"},
		{name: "null without a legacy host uses the site URL", defaultHost: pgtype.Text{String: "", Valid: false}, want: "https://dashboard.ai.example.com"},
		{name: "stored platform host", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com"), want: "https://app.example.com"},
		{name: "stored bare platform host", legacy: "https://ai.example.com", defaultHost: stored("APP.example.com"), want: "https://app.example.com"},
		{name: "stored server host uses the site URL", legacy: "https://app.example.com", defaultHost: stored("https://ai.example.com"), want: "https://dashboard.ai.example.com"},
		{name: "stored host no longer a platform host", legacy: "https://app.example.com", defaultHost: stored("https://retired.example.com"), want: "https://app.example.com"},
		{name: "stored path", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com/path"), want: "https://ai.example.com"},
		{name: "stored root path", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com/"), want: "https://app.example.com"},
		{name: "stored userinfo", legacy: "https://ai.example.com", defaultHost: stored("https://user:secret@app.example.com"), want: "https://ai.example.com"},
		{name: "stored query", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com?next=1"), want: "https://ai.example.com"},
		{name: "stored empty query", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com?"), want: "https://ai.example.com"},
		{name: "stored fragment", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com#top"), want: "https://ai.example.com"},
		{name: "stored empty fragment", legacy: "https://ai.example.com", defaultHost: stored("https://app.example.com#"), want: "https://ai.example.com"},
		{name: "stored garbage", legacy: "https://ai.example.com", defaultHost: stored("not a host"), want: "https://ai.example.com"},
		{name: "stored bad scheme", legacy: "https://ai.example.com", defaultHost: stored("ftp://app.example.com"), want: "https://ai.example.com"},
		{name: "stored empty", legacy: "https://app.example.com", defaultHost: stored(""), want: "https://app.example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := newResolver(t, tt.legacy).SiteURL(tt.defaultHost)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestServerURLWithoutLegacyHostUsesServerURL(t *testing.T) {
	t.Parallel()

	resolver := newResolver(t, "")
	require.Equal(t, "https://ai.example.com", resolver.ServerURL(pgtype.Text{String: "", Valid: false}).String())
	require.Equal(t, "https://app.example.com", resolver.ServerURL(stored("https://app.example.com")).String())
}

func TestSiteURLIsNilWithoutConfiguredURL(t *testing.T) {
	t.Parallel()

	resolver := orghost.New(orghost.Config{
		ServerURL:                  mustParse(t, "https://ai.example.com"),
		SiteURL:                    nil,
		PlatformHosts:              map[string]string{"app.example.com": "https://app.example.com"},
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	})
	require.Nil(t, resolver.SiteURL(pgtype.Text{String: "", Valid: false}))
	require.Equal(t, "https://app.example.com", resolver.SiteURL(stored("https://app.example.com")).String())
}

func TestResolvedURLsAreCopies(t *testing.T) {
	t.Parallel()

	resolver := newResolver(t, "https://app.example.com")
	first := resolver.SiteURL(pgtype.Text{String: "", Valid: false})
	first.Path = "/mutated"
	require.Equal(t, "https://app.example.com", resolver.SiteURL(pgtype.Text{String: "", Valid: false}).String())
}

func TestNewOrganizationDefaultHost(t *testing.T) {
	t.Parallel()

	require.Equal(t, pgtype.Text{String: "", Valid: false}, newResolver(t, "").NewOrganizationDefaultHost())

	resolver := orghost.New(orghost.Config{
		ServerURL:                  mustParse(t, "https://ai.example.com"),
		SiteURL:                    nil,
		PlatformHosts:              nil,
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: mustParse(t, "https://ai.example.com"),
	})
	require.Equal(t, stored("https://ai.example.com"), resolver.NewOrganizationDefaultHost())
}

func TestStoredPlatformHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		defaultHost pgtype.Text
		wantServer  string
		wantSite    string
		wantOK      bool
	}{
		{name: "null", defaultHost: pgtype.Text{String: "", Valid: false}},
		{name: "extra platform host", defaultHost: stored("https://APP.example.com"), wantServer: "https://app.example.com", wantSite: "https://app.example.com", wantOK: true},
		{name: "server host", defaultHost: stored("https://ai.example.com"), wantServer: "https://ai.example.com", wantSite: "https://dashboard.ai.example.com", wantOK: true},
		{name: "host no longer a platform host", defaultHost: stored("https://retired.example.com")},
		{name: "malformed", defaultHost: stored("https://app.example.com/path")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			serverURL, siteURL, ok := newResolver(t, "https://app.example.com").StoredPlatformHost(tt.defaultHost)
			require.Equal(t, tt.wantOK, ok)
			if !tt.wantOK {
				require.Nil(t, serverURL)
				require.Nil(t, siteURL)
				return
			}
			require.Equal(t, tt.wantServer, serverURL.String())
			require.Equal(t, tt.wantSite, siteURL.String())
		})
	}
}

func TestStoredPlatformHostServerHostWithoutSiteURL(t *testing.T) {
	t.Parallel()

	resolver := orghost.New(orghost.Config{
		ServerURL:                  mustParse(t, "https://ai.example.com"),
		SiteURL:                    nil,
		PlatformHosts:              nil,
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	})
	_, _, ok := resolver.StoredPlatformHost(stored("https://ai.example.com"))
	require.False(t, ok)
}
