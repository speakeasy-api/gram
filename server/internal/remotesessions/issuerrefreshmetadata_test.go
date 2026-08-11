package remotesessions_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// newIssuerPayloadForURL builds a project-tier create payload whose issuer URL
// is the supplied fake upstream, so a later refresh actually reaches it. The
// endpoints are seeded with placeholder values that differ from what the
// upstream advertises, which is what makes "refresh overwrote them" observable.
func newIssuerPayloadForURL(slug, issuerURL string) *gen.CreateRemoteSessionIssuerPayload {
	payload := newIssuerPayload(slug)
	payload.Issuer = issuerURL
	payload.AuthorizationEndpoint = conv.PtrEmpty("https://stale.example.com/authorize")
	payload.TokenEndpoint = conv.PtrEmpty("https://stale.example.com/token")
	payload.RegistrationEndpoint = conv.PtrEmpty("https://stale.example.com/register")
	payload.JwksURI = conv.PtrEmpty("https://stale.example.com/jwks")
	return payload
}

func TestRefreshRemoteSessionIssuerMetadata_OverwritesStaleEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-happy", upstream.URL))
	require.NoError(t, err)
	require.Equal(t, "https://stale.example.com/authorize", *created.AuthorizationEndpoint)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.DiscoveryWarnings)

	require.Equal(t, upstream.URL+"/authorize", *result.Issuer.AuthorizationEndpoint)
	require.Equal(t, upstream.URL+"/token", *result.Issuer.TokenEndpoint)
	require.Equal(t, upstream.URL+"/register", *result.Issuer.RegistrationEndpoint)
	require.Equal(t, upstream.URL+"/jwks", *result.Issuer.JwksURI)
	require.Equal(t, []string{"openid"}, result.Issuer.ScopesSupported)
}

// A refresh restates the issuer's whole discovered surface, so an endpoint the
// upstream has stopped advertising is cleared rather than left behind.
func TestRefreshRemoteSessionIssuerMetadata_ClearsWithdrawnEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "registration_endpoint")
		doc["scopes_supported"] = []string{}
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-clear", upstream.URL))
	require.NoError(t, err)
	require.NotNil(t, created.RegistrationEndpoint)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, result.Issuer.RegistrationEndpoint, "an issuer that stopped advertising DCR has its registration endpoint cleared")
	require.Empty(t, result.Issuer.ScopesSupported, "a withdrawn *_supported array is cleared to empty, not left stale")
}

// Refresh writes only RFC 8414-derived columns. Gram's own behavior and display
// fields are not discoverable and must survive untouched.
func TestRefreshRemoteSessionIssuerMetadata_LeavesGramOwnedFieldsAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	payload := newIssuerPayloadForURL("idp-refresh-untouched", upstream.URL)
	payload.Name = conv.PtrEmpty("Production IdP")
	payload.Oidc = conv.PtrEmpty(true)
	payload.Passthrough = conv.PtrEmpty(true)
	payload.ClientSetupDocumentationURL = conv.PtrEmpty("https://docs.example.com/setup")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, "Production IdP", *result.Issuer.Name)
	require.True(t, result.Issuer.Oidc)
	require.True(t, result.Issuer.Passthrough)
	require.Equal(t, "https://docs.example.com/setup", *result.Issuer.ClientSetupDocumentationURL)
	require.Equal(t, "idp-refresh-untouched", result.Issuer.Slug)
	require.Equal(t, upstream.URL, result.Issuer.Issuer)
}

// An upstream that omits an optional *_supported array decodes to a nil slice.
// Those columns are NOT NULL, so a nil reaching the update violates the
// constraint; orEmptySlice is what stands between that and a 500. All four
// arrays are OPTIONAL in RFC 8414, so this is the common case, not an edge one.
func TestRefreshRemoteSessionIssuerMetadata_HandlesAbsentSupportedArrays(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "scopes_supported")
		delete(doc, "grant_types_supported")
		delete(doc, "response_types_supported")
		delete(doc, "token_endpoint_auth_methods_supported")
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-no-arrays", upstream.URL))
	require.NoError(t, err)
	require.NotEmpty(t, created.ScopesSupported)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Issuer.ScopesSupported)
	require.Empty(t, result.Issuer.GrantTypesSupported)
	require.Empty(t, result.Issuer.ResponseTypesSupported)
	require.Empty(t, result.Issuer.TokenEndpointAuthMethodsSupported)
}

// An upstream that is unreachable or erroring is not caller error on refresh:
// the caller supplied only an id, and Gram chose the URL from the stored row.
// Reporting a transient outage as 4xx would make SDK retry policies treat it as
// terminal.
func TestRefreshRemoteSessionIssuerMetadata_UnreachableUpstreamIsGatewayError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := statusOnlyServer(t, http.StatusServiceUnavailable)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-upstream-down", upstream.URL))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeGatewayError)
	require.Contains(t, err.Error(), "Unexpected HTTP 503")
}

// issuerProbeCandidates falls back to the origin-root well-known URL when the
// path-aware candidates miss, and gateways serving metadata only there advertise
// the origin rather than the configured path. That mismatch is created by
// Gram's own probe order, so a refresh must tolerate it or every issuer created
// through the fallback becomes permanently unrefreshable.
func TestRefreshRemoteSessionIssuerMetadata_AcceptsOriginWhenIssuerHasPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-origin", upstream.URL+"/tenant"))
	require.NoError(t, err)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", *result.Issuer.AuthorizationEndpoint)
	require.Equal(t, upstream.URL+"/tenant", result.Issuer.Issuer, "the stored issuer URL is never rewritten")
	require.NotEmpty(t, result.DiscoveryWarnings, "the divergence is still surfaced")
}

// The origin relaxation is no wider than the fallback that motivates it: a
// sibling path on the same host is a different tenant on a multi-tenant IdP,
// and adopting its endpoints would send users to the wrong place.
func TestRefreshRemoteSessionIssuerMetadata_AbortsOnSiblingPathIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	var upstream *httptest.Server
	upstream = fakeIssuerServer(t, func(doc map[string]any) {
		doc["issuer"] = upstream.URL + "/other-tenant"
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-sibling", upstream.URL+"/tenant"))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "another authorization server")
}

// The issuer claim is never overwritten. A document naming a different issuer
// aborts the refresh rather than silently repointing OAuth at another
// authorization server.
func TestRefreshRemoteSessionIssuerMetadata_AbortsOnIssuerMismatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		doc["issuer"] = "https://attacker.example.com"
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-mismatch", upstream.URL))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "another authorization server")

	// Nothing was persisted: the stale endpoints are still the stale endpoints.
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://stale.example.com/authorize", *fetched.AuthorizationEndpoint)
}

// A document advertising no authorization_endpoint describes an issuer that
// cannot complete an OAuth flow. Discovery hands it back as a last resort on
// create, but persisting it over working endpoints would break every session
// the issuer mints, so a refresh distrusts the whole document.
func TestRefreshRemoteSessionIssuerMetadata_AbortsOnMissingAuthorizationEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "authorization_endpoint")
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-no-authz", upstream.URL))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "authorization_endpoint")

	// The abort happens before the transaction opens, so the stale endpoints
	// survive. Asserting this is what would catch the check being moved after
	// the write.
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://stale.example.com/authorize", *fetched.AuthorizationEndpoint)
}

func TestRefreshRemoteSessionIssuerMetadata_AbortsOnMissingTokenEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "token_endpoint")
	})

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-no-token", upstream.URL))
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.Contains(t, err.Error(), "token_endpoint")

	// The abort happens before the transaction opens, so the stale endpoints
	// survive. Asserting this is what would catch the check being moved after
	// the write.
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "https://stale.example.com/authorize", *fetched.AuthorizationEndpoint)
}

func TestRefreshRemoteSessionIssuerMetadata_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-audit", upstream.URL))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// An edit that commits while discovery is in flight must not show up in the
// refresh's audit diff. Refresh reads the row before the transaction opens, to
// learn which URL to discover against, but its audit before-snapshot has to
// come from a locked re-read inside the transaction; otherwise the operator's
// rename lands in the refresh's before/after diff and is attributed to it.
//
// The upstream handler is the one place guaranteed to run inside that window,
// so the competing write is issued from there. That makes the interleaving
// deterministic rather than a timing hope.
func TestRefreshRemoteSessionIssuerMetadata_AuditSnapshotExcludesConcurrentEdit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	var issuerID atomic.Pointer[string]
	var once sync.Once
	renameErr := make(chan error, 1)

	upstream := fakeIssuerServer(t, func(_ map[string]any) {
		once.Do(func() {
			id := issuerID.Load()
			if id == nil {
				renameErr <- nil
				return
			}
			_, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
				ID:                                *id,
				Name:                              conv.PtrEmpty("Renamed During Discovery"),
				Slug:                              nil,
				Issuer:                            nil,
				LogoAssetID:                       nil,
				ClientSetupDocumentationURL:       nil,
				AuthorizationEndpoint:             nil,
				TokenEndpoint:                     nil,
				RegistrationEndpoint:              nil,
				JwksURI:                           nil,
				ServiceDocumentation:              nil,
				OpPolicyURI:                       nil,
				OpTosURI:                          nil,
				ScopesSupported:                   nil,
				GrantTypesSupported:               nil,
				ResponseTypesSupported:            nil,
				TokenEndpointAuthMethodsSupported: nil,
				ClientIDMetadataDocumentSupported: nil,
				Oidc:                              nil,
				Passthrough:                       nil,
				SessionToken:                      nil,
				ApikeyToken:                       nil,
				ProjectSlugInput:                  nil,
			})
			renameErr <- err
		})
	})

	payload := newIssuerPayloadForURL("idp-refresh-concurrent", upstream.URL)
	payload.Name = conv.PtrEmpty("Original Name")

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	issuerID.Store(&created.ID)

	result, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NoError(t, <-renameErr, "the competing rename must land during discovery for this test to mean anything")

	// The refresh writes the newest update entry, since it commits after the
	// rename it raced.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Renamed During Discovery", before["Name"],
		"the before-snapshot must come from the locked read inside the transaction, not the pre-discovery read")

	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Renamed During Discovery", after["Name"],
		"a refresh never writes name, so it is unchanged across the entry")

	require.Equal(t, "Renamed During Discovery", *result.Issuer.Name)
}

func TestRefreshRemoteSessionIssuerMetadata_RequiresProjectWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayloadForURL("idp-refresh-rbac", upstream.URL))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read scope alone is enough to fetch metadata, which persists nothing, but
	// not to refresh it, which does.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err = ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRefreshRemoteSessionIssuerMetadata_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Organization-level issuers are readable from a project by inheritance but are
// not writable there — they refresh through the org tier, matching how
// updateRemoteSessionIssuer scopes its write.
func TestRefreshRemoteSessionIssuerMetadata_RejectsInheritedOrgIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "idp-refresh-inherited")

	_, err := ti.service.RefreshRemoteSessionIssuerMetadata(ctx, &gen.RefreshRemoteSessionIssuerMetadataPayload{
		ID:               orgIssuerID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// --- Organization tier ---

func TestFetchIssuerMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	draft, err := ti.service.FetchIssuerMetadata(ctx, &orgissuersgen.FetchIssuerMetadataPayload{
		Issuer:       upstream.URL,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL, draft.Issuer)
	require.Equal(t, upstream.URL+"/authorize", *draft.AuthorizationEndpoint)
	require.Equal(t, upstream.URL+"/token", *draft.TokenEndpoint)
	require.Empty(t, draft.DiscoveryWarnings)
}

func TestFetchIssuerMetadata_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.FetchIssuerMetadata(ctx, &orgissuersgen.FetchIssuerMetadataPayload{
		Issuer:       upstream.URL,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestFetchIssuerMetadata_BadURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.FetchIssuerMetadata(ctx, &orgissuersgen.FetchIssuerMetadataPayload{
		Issuer:       "ftp://not-http",
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// One org-tier endpoint covers organization-level rows...
func TestRefreshIssuerMetadata_OrganizationLevelIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	payload := newCreateIssuerPayload("org-refresh-orglevel", nil)
	payload.Issuer = upstream.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty("https://stale.example.com/authorize")

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)
	require.Empty(t, created.ProjectID)

	result, err := ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", *result.Issuer.AuthorizationEndpoint)
	require.Empty(t, result.Issuer.ProjectID, "refresh does not re-scope the issuer")
}

// ...and project-specific rows in the same organization, which is why the
// Remote Identity Providers page needs only this one endpoint for both tables.
func TestRefreshIssuerMetadata_ProjectSpecificIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	payload := newCreateIssuerPayload("org-refresh-projlevel", &projectID)
	payload.Issuer = upstream.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty("https://stale.example.com/authorize")

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotEmpty(t, created.ProjectID)

	result, err := ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", *result.Issuer.AuthorizationEndpoint)
	require.Equal(t, projectID, result.Issuer.ProjectID, "refresh does not re-scope the issuer")
}

func TestRefreshIssuerMetadata_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	payload := newCreateIssuerPayload("org-refresh-rbac", nil)
	payload.Issuer = upstream.URL

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err = ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Cross-organization isolation: an issuer in another org is not refreshable,
// and reports not-found rather than leaking its existence.
func TestRefreshIssuerMetadata_CrossOrgIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "refresh-other-org")
	foreignIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrgID, "refresh-foreign-issuer")

	_, err := ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           foreignIssuerID.String(),
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRefreshIssuerMetadata_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	payload := newCreateIssuerPayload("org-refresh-audit", nil)
	payload.Issuer = upstream.URL

	created, err := ti.service.CreateIssuer(ctx, payload)
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	_, err = ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           created.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// The org tier must not reach global issuers: GetOrganizationRemoteSessionIssuerByID
// requires organization_id to match, and a global issuer's is NULL. Converse of
// TestRefreshGlobalIssuerMetadata_RejectsOrgScopedIssuer.
func TestRefreshIssuerMetadata_RejectsGlobalIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	global, err := ti.service.CreateGlobalIssuer(adminCtx, createGlobalIssuer(t, "org-refresh-rejects-global"))
	require.NoError(t, err)

	_, err = ti.service.RefreshIssuerMetadata(ctx, &orgissuersgen.RefreshIssuerMetadataPayload{
		ID:           global.ID,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// --- Shared update query: the identity match that stands in for a row lock ---
//
// Discovery runs outside the transaction, so the row is read before the write.
// UpdateRemoteSessionIssuerDiscoveredMetadata re-asserts the row's identity
// instead of taking a lock, and the handlers turn a zero-row result into a 409.
// These pin each clause of that WHERE: without them the whole no-lock argument
// rests on an untested query.

// discoveredMetadataParams builds a minimal valid parameter set targeting the
// supplied row, which each test then perturbs on exactly one axis.
func discoveredMetadataParams(issuer repo.RemoteSessionIssuer) repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams {
	return repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams{
		AuthorizationEndpoint:             "https://refreshed.example.com/authorize",
		TokenEndpoint:                     "https://refreshed.example.com/token",
		RegistrationEndpoint:              "",
		JwksUri:                           "",
		ServiceDocumentation:              "",
		OpPolicyUri:                       "",
		OpTosUri:                          "",
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{},
		ResponseTypesSupported:            []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		ClientIDMetadataDocumentSupported: false,
		ID:                                issuer.ID,
		Issuer:                            issuer.Issuer,
		ProjectID:                         issuer.ProjectID,
		OrganizationID:                    issuer.OrganizationID,
	}
}

// Baseline: unperturbed identity writes the row.
func TestUpdateRemoteSessionIssuerDiscoveredMetadata_MatchesOnUnchangedIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("query-identity-match"))
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: uuid.NullUUID{UUID: uuid.MustParse(created.ProjectID), Valid: true},
	})
	require.NoError(t, err)

	updated, err := repo.New(ti.conn).UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, discoveredMetadataParams(stored))
	require.NoError(t, err)
	require.Equal(t, "https://refreshed.example.com/authorize", updated.AuthorizationEndpoint.String)
}

// A concurrent moveIssuer re-scopes the row between the read and the write.
func TestUpdateRemoteSessionIssuerDiscoveredMetadata_MissesOnChangedProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("query-identity-project"))
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: uuid.NullUUID{UUID: uuid.MustParse(created.ProjectID), Valid: true},
	})
	require.NoError(t, err)

	params := discoveredMetadataParams(stored)
	params.ProjectID = uuid.NullUUID{UUID: uuid.New(), Valid: true}

	_, err = repo.New(ti.conn).UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// A concurrent updateIssuer repoints the issuer URL between the read and the
// write, which would otherwise stamp one authorization server's endpoints onto
// another's URL.
func TestUpdateRemoteSessionIssuerDiscoveredMetadata_MissesOnChangedIssuerURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("query-identity-issuer"))
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: uuid.NullUUID{UUID: uuid.MustParse(created.ProjectID), Valid: true},
	})
	require.NoError(t, err)

	params := discoveredMetadataParams(stored)
	params.Issuer = "https://moved.example.com"

	_, err = repo.New(ti.conn).UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// A concurrent delete tombstones the row between the read and the write.
func TestUpdateRemoteSessionIssuerDiscoveredMetadata_MissesOnDeletedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("query-identity-deleted"))
	require.NoError(t, err)

	stored, err := repo.New(ti.conn).GetRemoteSessionIssuerByIDProjectOwned(ctx, repo.GetRemoteSessionIssuerByIDProjectOwnedParams{
		ID:        uuid.MustParse(created.ID),
		ProjectID: uuid.NullUUID{UUID: uuid.MustParse(created.ProjectID), Valid: true},
	})
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	_, err = repo.New(ti.conn).UpdateRemoteSessionIssuerDiscoveredMetadata(ctx, discoveredMetadataParams(stored))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// --- Global (platform admin) tier ---

func TestFetchGlobalIssuerMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)
	upstream := fakeIssuerServer(t, nil)

	draft, err := ti.service.FetchGlobalIssuerMetadata(ctx, &adminrsgen.FetchGlobalIssuerMetadataPayload{
		Issuer:       upstream.URL,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL, draft.Issuer)
	require.Equal(t, upstream.URL+"/token", *draft.TokenEndpoint)
}

func TestFetchGlobalIssuerMetadata_RequiresAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	upstream := fakeIssuerServer(t, nil)

	_, err := ti.service.FetchGlobalIssuerMetadata(ctx, &adminrsgen.FetchGlobalIssuerMetadataPayload{
		Issuer:       upstream.URL,
		SessionToken: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRefreshGlobalIssuerMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)
	upstream := fakeIssuerServer(t, nil)

	payload := createGlobalIssuer(t, "global-refresh")
	payload.Issuer = upstream.URL
	payload.AuthorizationEndpoint = conv.PtrEmpty("https://stale.example.com/authorize")

	created, err := ti.service.CreateGlobalIssuer(ctx, payload)
	require.NoError(t, err)

	result, err := ti.service.RefreshGlobalIssuerMetadata(ctx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{
		ID:           created.ID,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Equal(t, upstream.URL+"/authorize", *result.Issuer.AuthorizationEndpoint)
	require.Empty(t, result.Issuer.ProjectID)
	require.Empty(t, result.Issuer.OrganizationID)
}

func TestRefreshGlobalIssuerMetadata_RequiresAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshGlobalIssuerMetadata(ctx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRefreshGlobalIssuerMetadata_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = withAdmin(t, ctx)

	_, err := ti.service.RefreshGlobalIssuerMetadata(ctx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{
		ID:           uuid.NewString(),
		SessionToken: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A non-global issuer is invisible to the global tier: its id exists, but
// GetGlobalRemoteSessionIssuerByID requires both scope columns to be NULL.
func TestRefreshGlobalIssuerMetadata_RejectsOrgScopedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "global-refresh-scoped")

	_, err := ti.service.RefreshGlobalIssuerMetadata(adminCtx, &adminrsgen.RefreshGlobalIssuerMetadataPayload{
		ID:           orgIssuerID.String(),
		SessionToken: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}
