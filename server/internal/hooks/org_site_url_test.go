package hooks

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orghost"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

const testPlatformBaseURL = "https://platform.example.test"

// useTestOrgHosts gives the service a resolver that knows one extra platform
// host and stores defaultHost on the authenticated organization.
func useTestOrgHosts(t *testing.T, ctx context.Context, ti *testInstance, defaultHost pgtype.Text) {
	t.Helper()

	siteURL, err := url.Parse("https://app.example.test")
	require.NoError(t, err)
	serverURL, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)
	ti.service.orgHosts = orghost.New(orghost.Config{
		ServerURL:                  serverURL,
		SiteURL:                    siteURL,
		PlatformHosts:              map[string]string{"platform.example.test": testPlatformBaseURL},
		LegacyDefaultHost:          nil,
		NewOrganizationDefaultHost: nil,
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NoError(t, organizationsrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: defaultHost,
		ID:          authCtx.ActiveOrganizationID,
	}))
}

// requireDenyLinksOn denies a shadow-MCP call and a warn challenge and checks
// that the request link, the block link and the acknowledgement link all use
// baseURL.
func requireDenyLinksOn(t *testing.T, ctx context.Context, ti *testInstance, baseURL string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.service.riskScanner = blockEffectShadowMCPScanner{
		ingestUserScopedShadowMCPScanner: ingestUserScopedShadowMCPScanner{userID: authCtx.UserID},
		policyID:                         uuid.NewString(),
	}
	result, err := ti.service.Ingest(ctx, shadowMCPDenyPayload("org-host-shadow-"+uuid.NewString(), "call-org-host"))
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	effect := requireWireBlockEffect(t, result.Effects)
	requestURL, ok := effect["request_url"].(string)
	require.True(t, ok)
	require.Contains(t, requestURL, baseURL+"/risk-policy-bypass/request#request_token=")
	blockURL, ok := effect["block_url"].(string)
	require.True(t, ok)
	require.Contains(t, blockURL, baseURL+"/blocks/")

	ti.service.riskScanner = &stubResultScanner{result: &risk.ScanResult{
		Action:          "warn",
		PolicyID:        uuid.NewString(),
		PolicyName:      "danger",
		RuleID:          "dangerous-operation",
		Entity:          "destructive",
		MatchedValue:    "rm -rf /tmp/warn",
		CallFingerprint: "org-host-warn-fingerprint",
	}}
	result, err = ti.service.Ingest(ctx, canonicalWarnTestPayload(t, "claude", "tool", "org-host-warn-"+uuid.NewString()))
	require.NoError(t, err)
	require.Equal(t, "deny", result.Decision)
	require.NotNil(t, result.Message)
	require.Contains(t, *result.Message, baseURL+"/risk-policy-challenge/acknowledge#")
}

func TestDenyLinksUseOrganizationDefaultHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	useTestOrgHosts(t, ctx, ti, pgtype.Text{String: testPlatformBaseURL, Valid: true})

	requireDenyLinksOn(t, ctx, ti, testPlatformBaseURL)
}

// A failed organization read must not drop the deny links: they fall back to
// the legacy host, and the failure is not cached.
func TestDenyLinksFallBackToLegacyHostWhenOrganizationReadFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	useTestOrgHosts(t, ctx, ti, pgtype.Text{String: testPlatformBaseURL, Valid: true})

	// An organization id with no row makes GetOrganizationMetadata fail.
	missingOrgID := "org_missing_" + uuid.NewString()
	got := ti.service.orgSiteURL(ctx, missingOrgID)
	require.NotNil(t, got)
	require.Equal(t, "https://app.example.test", got.String())
	_, cached := ti.service.orgHostCache.get(missingOrgID, time.Now())
	require.False(t, cached)
}

// Consecutive deny links for an organization reuse the cached default host
// instead of reading the row again.
func TestDenyLinksCacheOrganizationDefaultHost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	useTestOrgHosts(t, ctx, ti, pgtype.Text{String: testPlatformBaseURL, Valid: true})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.Equal(t, testPlatformBaseURL, ti.service.orgSiteURL(ctx, authCtx.ActiveOrganizationID).String())

	require.NoError(t, organizationsrepo.New(ti.conn).SetOrganizationDefaultHostForTest(ctx, organizationsrepo.SetOrganizationDefaultHostForTestParams{
		DefaultHost: pgtype.Text{String: "", Valid: false},
		ID:          authCtx.ActiveOrganizationID,
	}))
	require.Equal(t, testPlatformBaseURL, ti.service.orgSiteURL(ctx, authCtx.ActiveOrganizationID).String())
}

func TestDenyLinksWithoutDefaultHostUseSiteURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	useTestOrgHosts(t, ctx, ti, pgtype.Text{String: "", Valid: false})

	requireDenyLinksOn(t, ctx, ti, "https://app.example.test")
}
