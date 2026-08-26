package hosts_test

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hosts"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const (
	canonicalURL = "https://app.getgram.ai"
	secondaryURL = "https://ai.speakeasy.com"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func newConn(t *testing.T) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "hosts_testdb")
	require.NoError(t, err)
	return conn
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

// newHosts builds a host model over conn with canonicalURL as the canonical
// host and the given extra platform hosts.
func newHosts(t *testing.T, conn *pgxpool.Pool, platform ...string) *hosts.Hosts {
	t.Helper()

	extra := make([]*url.URL, 0, len(platform))
	for _, raw := range platform {
		extra = append(extra, mustParse(t, raw))
	}

	h, err := hosts.New(testenv.NewLogger(t), conn, mustParse(t, canonicalURL), extra, mustParse(t, canonicalURL))
	require.NoError(t, err)
	return h
}

// seedOrg inserts an organization with the given default host. An empty
// defaultHost leaves the column NULL.
func seedOrg(t *testing.T, conn *pgxpool.Pool, orgID, defaultHost string) {
	t.Helper()

	require.NoError(t, orgRepo.New(conn).CreateOrganizationMetadata(t.Context(), orgRepo.CreateOrganizationMetadataParams{
		ID:   orgID,
		Name: orgID,
		Slug: orgID,
	}))

	require.NoError(t, testrepo.New(conn).SetOrganizationDefaultHostFixture(t.Context(), testrepo.SetOrganizationDefaultHostFixtureParams{
		ID:          orgID,
		DefaultHost: conv.PtrToPGTextEmpty(conv.PtrEmpty(defaultHost)),
	}))
}

// seedDomain inserts a verified custom domain at the given scope.
func seedDomain(t *testing.T, conn *pgxpool.Pool, orgID, domain, scope string, activated bool) {
	t.Helper()

	require.NoError(t, testrepo.New(conn).CreateScopedCustomDomainFixture(t.Context(), testrepo.CreateScopedCustomDomainFixtureParams{
		OrganizationID: orgID,
		Domain:         domain,
		Scope:          scope,
		Verified:       true,
		Activated:      activated,
	}))
}

func TestNew_CanonicalIsAlwaysAPlatformHost(t *testing.T) {
	t.Parallel()

	h := newHosts(t, newConn(t))

	require.True(t, h.IsPlatform("app.getgram.ai"))
	require.False(t, h.IsPlatform("ai.speakeasy.com"))
	require.Equal(t, []*url.URL{mustParse(t, canonicalURL)}, h.PlatformHosts())
}

func TestNew_DeduplicatesCanonicalFromPlatformList(t *testing.T) {
	t.Parallel()

	h := newHosts(t, newConn(t), canonicalURL, secondaryURL)

	require.Equal(t, []*url.URL{mustParse(t, canonicalURL), mustParse(t, secondaryURL)}, h.PlatformHosts())
}

func TestNew_RejectsMissingConfiguration(t *testing.T) {
	t.Parallel()

	conn := newConn(t)
	logger := testenv.NewLogger(t)
	canonical := mustParse(t, canonicalURL)

	_, err := hosts.New(logger, nil, canonical, nil, canonical)
	require.Error(t, err)

	_, err = hosts.New(logger, conn, nil, nil, canonical)
	require.Error(t, err)

	_, err = hosts.New(logger, conn, canonical, nil, nil)
	require.Error(t, err)

	_, err = hosts.New(logger, conn, canonical, []*url.URL{{}}, canonical)
	require.Error(t, err)
}

// The outbound callback is what upstream OAuth providers hold; moving the
// canonical host must not move it.
func TestOutboundCallback_DoesNotFollowCanonicalHost(t *testing.T) {
	t.Parallel()

	h, err := hosts.New(testenv.NewLogger(t), newConn(t),
		mustParse(t, secondaryURL), nil, mustParse(t, canonicalURL))
	require.NoError(t, err)

	require.Equal(t, secondaryURL, h.Canonical().String())
	require.Equal(t, secondaryURL, h.Auth().String())
	require.Equal(t, canonicalURL, h.OutboundCallback().String())
}

func TestParseList(t *testing.T) {
	t.Parallel()

	parsed, err := hosts.ParseList("  https://app.getgram.ai ,, https://ai.speakeasy.com  ")
	require.NoError(t, err)
	require.Equal(t, []*url.URL{mustParse(t, canonicalURL), mustParse(t, secondaryURL)}, parsed)

	empty, err := hosts.ParseList("")
	require.NoError(t, err)
	require.Empty(t, empty)

	_, err = hosts.ParseList("not-a-url")
	require.Error(t, err)
}

func TestResolve(t *testing.T) {
	t.Parallel()

	conn := newConn(t)

	const (
		orgPlain      = "org-no-preference"
		orgSecondary  = "org-default-secondary"
		orgAppDomain  = "org-app-domain"
		orgStaleHost  = "org-stale-default"
		orgMCPOnly    = "org-mcp-only"
		orgDeactivate = "org-deactivated-app-domain"
		orgBoth       = "org-both-scopes"
	)

	seedOrg(t, conn, orgPlain, "")
	seedOrg(t, conn, orgSecondary, "ai.speakeasy.com")
	seedOrg(t, conn, orgAppDomain, "gram.customer.example")
	seedDomain(t, conn, orgAppDomain, "gram.customer.example", "app", true)
	seedOrg(t, conn, orgStaleHost, "gone.customer.example")
	seedOrg(t, conn, orgMCPOnly, "mcp.customer.example")
	seedDomain(t, conn, orgMCPOnly, "mcp.customer.example", "mcp", true)
	seedOrg(t, conn, orgDeactivate, "pending.customer.example")
	seedDomain(t, conn, orgDeactivate, "pending.customer.example", "app", false)

	// One organization holding both scopes at once, which the per-scope unique
	// index allows and the previous one-domain-per-org index did not.
	seedOrg(t, conn, orgBoth, "app.both.example")
	seedDomain(t, conn, orgBoth, "mcp.both.example", "mcp", true)
	seedDomain(t, conn, orgBoth, "app.both.example", "app", true)

	h := newHosts(t, conn, secondaryURL)

	tests := []struct {
		name        string
		requestHost string
		orgID       string
		want        string
		description string
	}{
		{
			name:        "request host wins when it is a platform host",
			requestHost: "ai.speakeasy.com",
			orgID:       orgPlain,
			want:        secondaryURL,
			description: "a first-party host the request already arrived on is kept",
		},
		{
			name:        "request host wins when it is the org's app-scoped domain",
			requestHost: "gram.customer.example",
			orgID:       orgAppDomain,
			want:        "https://gram.customer.example",
			description: "an org served on its own app domain keeps rendering URLs there",
		},
		{
			name:        "another org's app domain does not count as a request host",
			requestHost: "gram.customer.example",
			orgID:       orgPlain,
			want:        canonicalURL,
			description: "app domains are per-org, so this falls through to the canonical host",
		},
		{
			name:        "org default host is used when the request host is unusable",
			requestHost: "unknown.example.com",
			orgID:       orgSecondary,
			want:        secondaryURL,
			description: "second fallback: the org's configured default host",
		},
		{
			name:        "org default host is used when there is no request",
			requestHost: "",
			orgID:       orgSecondary,
			want:        secondaryURL,
			description: "background callers have no request and start at the default host",
		},
		{
			name:        "org default host pointing at a domain the org no longer has falls back",
			requestHost: "",
			orgID:       orgStaleHost,
			want:        canonicalURL,
			description: "validity is re-checked on read, so a deleted domain degrades to canonical",
		},
		{
			name:        "an mcp-scoped domain is not a valid default host",
			requestHost: "mcp.customer.example",
			orgID:       orgMCPOnly,
			want:        canonicalURL,
			description: "only app-scoped domains may render control-plane URLs",
		},
		{
			name:        "a not-yet-activated app domain is not a valid default host",
			requestHost: "",
			orgID:       orgDeactivate,
			want:        canonicalURL,
			description: "the domain must be verified and activated to be served",
		},
		{
			name:        "an org may hold an mcp and an app domain at once",
			requestHost: "",
			orgID:       orgBoth,
			want:        "https://app.both.example",
			description: "only the app-scoped one is eligible to render control-plane URLs",
		},
		{
			name:        "canonical host when the org has no preference",
			requestHost: "",
			orgID:       orgPlain,
			want:        canonicalURL,
			description: "third fallback: the canonical host",
		},
		{
			name:        "canonical host when there is no organization at all",
			requestHost: "unknown.example.com",
			orgID:       "",
			want:        canonicalURL,
			description: "no org means no org-scoped host to prefer",
		},
		{
			name:        "canonical host for an organization that does not exist",
			requestHost: "",
			orgID:       "org-missing",
			want:        canonicalURL,
			description: "a missing org row is not an error, just no preference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var req *http.Request
			if tt.requestHost != "" {
				req = httptest.NewRequest(http.MethodGet, "https://"+tt.requestHost+"/test", nil)
				req.Host = tt.requestHost
			}

			require.Equal(t, tt.want, h.Resolve(t.Context(), req, tt.orgID).String(), tt.description)
		})
	}
}
