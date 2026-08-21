package remotesessions_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidptest"
	gen "github.com/speakeasy-api/gram/server/gen/remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// newIssuerPayload returns a CreateRemoteSessionIssuerPayload with a fresh
// project-unique slug.
func newIssuerPayload(slug string) *gen.CreateRemoteSessionIssuerPayload {
	authEP := "https://idp.example.com/authorize"
	tokenEP := "https://idp.example.com/token"
	regEP := "https://idp.example.com/register"
	jwksURI := "https://idp.example.com/jwks"
	oidc := false
	passthrough := false
	return &gen.CreateRemoteSessionIssuerPayload{
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             &authEP,
		TokenEndpoint:                     &tokenEP,
		RegistrationEndpoint:              &regEP,
		JwksURI:                           &jwksURI,
		ScopesSupported:                   []string{"openid", "profile"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              &oidc,
		Passthrough:                       &passthrough,
	}
}

func TestCreateRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-create"))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.ID)
	require.Equal(t, "idp-create", result.Slug)
	require.Equal(t, "https://idp.example.com", result.Issuer)
	require.NotNil(t, result.AuthorizationEndpoint)
	require.Equal(t, "https://idp.example.com/authorize", *result.AuthorizationEndpoint)
	require.False(t, result.Oidc)

	// The project's organization id is populated from the auth context.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotEmpty(t, result.ProjectID)
	require.Equal(t, authCtx.ActiveOrganizationID, result.OrganizationID)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

// TestCreateRemoteSessionIssuer_DuplicateSlug maps a duplicate-slug insert to a
// 409 conflict rather than an opaque unexpected fault.
func TestCreateRemoteSessionIssuer_DuplicateSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-dup-slug"))
	require.NoError(t, err)

	_, err = ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-dup-slug"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestCreateRemoteSessionIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Hand the caller only read scope; create should be denied.
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	})

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-rbac"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateRemoteSessionIssuer_BadRequestEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	payload := newIssuerPayload("")
	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateRemoteSessionIssuer_NameStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "My IdP"
	payload := newIssuerPayload("idp-name-stored")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "My IdP", *result.Name)

	// The audit subject display name reflects the name when set.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, "My IdP", record.SubjectDisplay)
}

func TestCreateRemoteSessionIssuer_NameTrimmed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "  Trimmed Name  "
	payload := newIssuerPayload("idp-name-trimmed")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Name)
	require.Equal(t, "Trimmed Name", *result.Name)
}

func TestCreateRemoteSessionIssuer_NameEmptyTreatedAsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "   "
	payload := newIssuerPayload("idp-name-empty")
	payload.Name = &name

	result, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, result.Name)

	// With no name, the audit subject display name falls back to the issuer URL.
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerCreate)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example.com", record.SubjectDisplay)
}

func TestCreateRemoteSessionIssuer_InvalidLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	badID := "not-a-uuid"
	payload := newIssuerPayload("idp-bad-logo")
	payload.LogoAssetID = &badID

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetRemoteSessionIssuer_ByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-get-id"))
	require.NoError(t, err)

	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
	require.Equal(t, created.Slug, fetched.Slug)
}

func TestGetRemoteSessionIssuer_BySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-get-slug"))
	require.NoError(t, err)

	slug := created.Slug
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, fetched.ID)
}

func TestGetRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	_, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetRemoteSessionIssuer_BothIDAndSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "x"
	_, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &id,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// getByIssuerURL is the lookup automatic setup performs before deciding whether
// to create an identity provider for a freshly discovered upstream.
func getByIssuerURL(issuerURL string) *gen.GetRemoteSessionIssuerPayload {
	return &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             nil,
		Issuer:           &issuerURL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

func projectAndOrgID(t *testing.T, ctx context.Context) (uuid.UUID, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID, authCtx.ActiveOrganizationID
}

// A miss is the normal path for a new upstream, not an error condition: the
// caller reads the 404 as "nothing describes this URL yet" and creates one.
func TestGetRemoteSessionIssuerByURL_NotFoundWhenNothingMatches(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL("https://miss.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetRemoteSessionIssuerByURL_FindsProjectIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	existing := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "byurl-existing", "https://reuse.example.com")

	found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL("https://reuse.example.com"))
	require.NoError(t, err)
	require.Equal(t, existing.String(), found.ID)
	require.Equal(t, "byurl-existing", found.Slug)
}

// The canonical form collapses trailing slashes, the scheme's default port, and
// host case, so a caller spelling the upstream any of these ways finds the same
// stored row.
func TestGetRemoteSessionIssuerByURL_MatchesEquivalentSpellings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	existing := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "byurl-spelling", "https://spelling.example.com/oauth")

	for _, spelling := range []string{
		"https://spelling.example.com/oauth",
		"https://spelling.example.com/oauth/",
		"https://spelling.example.com:443/oauth",
		"https://SPELLING.example.com/oauth",
		"  https://spelling.example.com/oauth  ",
	} {
		found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(spelling))
		require.NoError(t, err, spelling)
		require.Equal(t, existing.String(), found.ID, spelling)
	}
}

// http and https on one host are different upstreams and must not be equated.
func TestGetRemoteSessionIssuerByURL_DoesNotMatchAcrossSchemes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "byurl-https", "https://scheme.example.com")

	_, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL("http://scheme.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Precedence is project > organization > platform. All three tiers describe the
// same upstream here, and the project's own issuer has to win.
func TestGetRemoteSessionIssuerByURL_PrefersProjectOverInheritedTiers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://tiers.example.com"

	// Seeded platform-first so creation order is the opposite of tier order: if
	// resolution ever fell back to row order this test would fail.
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "tiers-platform", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "tiers-org", issuerURL)
	projectIssuer := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiers-project", issuerURL)

	found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(issuerURL))
	require.NoError(t, err)
	require.Equal(t, projectIssuer.String(), found.ID)
}

func TestGetRemoteSessionIssuerByURL_PrefersOrganizationOverPlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://orgwins.example.com"

	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "orgwins-platform", issuerURL)
	orgIssuer := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "orgwins-org", issuerURL)

	found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(issuerURL))
	require.NoError(t, err)
	require.Equal(t, orgIssuer.String(), found.ID)
}

// The platform catalog is the point of the inheritance work: an install against
// an upstream a platform admin already curated must find that issuer rather than
// fork a tenant copy of it, and must then accept a tenant-owned client.
func TestGetRemoteSessionIssuerByURL_FindsSeededPlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformIssuer := seedGlobalRemoteIssuer(t, ctx, ti.conn, "platformseed")

	found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL("https://platformseed.example.com"))
	require.NoError(t, err)
	require.Equal(t, platformIssuer.String(), found.ID)
	require.Empty(t, found.ProjectID)
	require.Empty(t, found.OrganizationID)

	requirePlatformIssuerUnchanged(t, ctx, ti.conn, platformIssuer)

	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "platformseed-usi")
	clientID := createRemoteClient(t, ctx, ti, platformIssuer.String(), userIssuerID.String(), "platformseed-client")
	require.NotEmpty(t, clientID)
}

// Several project-tier rows on one URL are normal, because the manual attach
// form creates unconditionally. The answer has to be deterministic anyway.
func TestGetRemoteSessionIssuerByURL_BreaksIntraTierTiesByOldest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	const issuerURL = "https://tiebreak.example.com"

	oldest := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiebreak-first", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiebreak-second", issuerURL)
	seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "tiebreak-third", issuerURL)

	// Repeated because a non-deterministic pick would still agree with the
	// expected answer some of the time.
	for range 5 {
		found, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(issuerURL))
		require.NoError(t, err)
		require.Equal(t, oldest.String(), found.ID)
	}
}

// Another organization's issuer for the same upstream must stay invisible.
func TestGetRemoteSessionIssuerByURL_IgnoresOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "byurl-other-org")
	seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(otherOrgID), "byurl-foreign", "https://foreign.example.com")

	_, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL("https://foreign.example.com"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetRemoteSessionIssuerByURL_RejectsInvalidIssuerURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, issuerURL := range []string{
		"idp.example.com",
		"ftp://idp.example.com",
		"https://idp.example.com?tenant=acme",
		"https://idp.example.com#fragment",
	} {
		_, err := ti.service.GetRemoteSessionIssuer(ctx, getByIssuerURL(issuerURL))
		require.Error(t, err, issuerURL)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

// The three selectors are mutually exclusive, and a blank issuer counts as
// absent so it cannot silently become a fourth "match everything" mode.
func TestGetRemoteSessionIssuerByURL_RejectsAmbiguousSelectors(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	existing := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "byurl-ambiguous", "https://ambiguous.example.com")

	id := existing.String()
	slug := "byurl-ambiguous"
	issuerURL := "https://ambiguous.example.com"
	blank := "   "

	for _, payload := range []*gen.GetRemoteSessionIssuerPayload{
		{ID: &id, Slug: nil, Issuer: &issuerURL, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil},
		{ID: nil, Slug: &slug, Issuer: &issuerURL, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil},
		{ID: &id, Slug: &slug, Issuer: &issuerURL, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil},
		{ID: nil, Slug: nil, Issuer: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil},
		{ID: nil, Slug: nil, Issuer: &blank, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil},
	} {
		_, err := ti.service.GetRemoteSessionIssuer(ctx, payload)
		require.Error(t, err)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

// A lookup is a read, so project:read is enough. This is the scope that lets a
// read-only principal answer "does an issuer for this URL exist?".
func TestGetRemoteSessionIssuerByURL_AllowsProjectRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectID, orgID := projectAndOrgID(t, ctx)

	existing := seedRemoteIssuerWithURL(t, ctx, ti.conn, conv.ToNullUUID(projectID), conv.ToPGText(orgID), "byurl-readonly", "https://readonly.example.com")

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeProjectRead,
		Selector: authz.NewSelector(authz.ScopeProjectRead, projectID.String()),
	})

	found, err := ti.service.GetRemoteSessionIssuer(readOnlyCtx, getByIssuerURL("https://readonly.example.com"))
	require.NoError(t, err)
	require.Equal(t, existing.String(), found.ID)
}

func TestListRemoteSessionIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-list-1"))
	require.NoError(t, err)
	_, err = ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-list-2"))
	require.NoError(t, err)

	result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Items), 2)
}

func TestListRemoteSessionIssuers_PaginationTraversal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const total = 5
	wantIDs := make(map[string]bool, total)
	for range total {
		created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload(uuid.NewString()))
		require.NoError(t, err)
		wantIDs[created.ID] = true
	}

	pageSize := 2
	gotIDs := make(map[string]bool, total)
	var cursor *string
	pages := 0
	for {
		pages++
		require.Less(t, pages, 10, "pagination did not terminate")
		result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
			Cursor:           cursor,
			Limit:            &pageSize,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
		require.NoError(t, err)
		for _, item := range result.Items {
			require.False(t, gotIDs[item.ID], "duplicate id across pages: %s", item.ID)
			gotIDs[item.ID] = true
		}
		if result.NextCursor == nil {
			break
		}
		cursor = result.NextCursor
	}
	for id := range wantIDs {
		require.True(t, gotIDs[id], "issuer %s missing from paginated traversal", id)
	}
}

func TestListRemoteSessionIssuers_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// No grants installed for this principal; list should be denied.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpdateRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-update"))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)

	newSlug := "idp-update-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              &newSlug,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	require.Equal(t, "idp-update-renamed", updated.Slug)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestUpdateRemoteSessionIssuer_SetsName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-update-name"))
	require.NoError(t, err)
	require.Nil(t, created.Name)

	name := "Renamed IdP"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &name,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "Renamed IdP", *updated.Name)
}

// An explicit empty string clears the name to NULL, mirroring the nullable
// endpoint columns.
func TestUpdateRemoteSessionIssuer_ClearsName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "Initial Name"
	createPayload := newIssuerPayload("idp-clear-name")
	createPayload.Name = &name
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)
	require.NotNil(t, created.Name)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Name: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.Name)
}

// An omitted name (nil) leaves the existing value untouched.
func TestUpdateRemoteSessionIssuer_OmittedNameKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	name := "Keep Me"
	createPayload := newIssuerPayload("idp-keep-name")
	createPayload.Name = &name
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)

	newSlug := "idp-keep-name-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:   created.ID,
		Slug: &newSlug,
		Name: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	require.Equal(t, "Keep Me", *updated.Name)
}

func TestUpdateRemoteSessionIssuer_SetsLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-update-logo"))
	require.NoError(t, err)
	require.Nil(t, created.LogoAssetID)

	assetID := createTestImageAsset(t, ctx, ti.conn).String()
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:          created.ID,
		LogoAssetID: &assetID,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.LogoAssetID)
	require.Equal(t, assetID, *updated.LogoAssetID)
}

// An explicit empty string clears the logo to NULL, mirroring the name
// column's sentinel.
func TestUpdateRemoteSessionIssuer_ClearsLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	assetID := createTestImageAsset(t, ctx, ti.conn).String()
	createPayload := newIssuerPayload("idp-clear-logo")
	createPayload.LogoAssetID = &assetID
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)
	require.NotNil(t, created.LogoAssetID)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:          created.ID,
		LogoAssetID: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.LogoAssetID)
}

// An omitted logo asset id (nil) leaves the existing value untouched.
func TestUpdateRemoteSessionIssuer_OmittedLogoAssetIDKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	assetID := createTestImageAsset(t, ctx, ti.conn).String()
	createPayload := newIssuerPayload("idp-keep-logo")
	createPayload.LogoAssetID = &assetID
	created, err := ti.service.CreateRemoteSessionIssuer(ctx, createPayload)
	require.NoError(t, err)

	newSlug := "idp-keep-logo-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:          created.ID,
		Slug:        &newSlug,
		LogoAssetID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.LogoAssetID)
	require.Equal(t, assetID, *updated.LogoAssetID)
}

// A malformed logo asset id is rejected as a 400 before the update query
// runs; the query casts the text parameter to uuid, so letting it through
// would surface as a Postgres cast error instead.
func TestUpdateRemoteSessionIssuer_InvalidLogoAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-bad-logo"))
	require.NoError(t, err)

	badID := "not-a-uuid"
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:          created.ID,
		LogoAssetID: &badID,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.NewString()
	slug := "anything"
	_, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                id,
		Slug:                              &slug,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// An explicit empty string on any of the four nullable endpoint fields
// clears the column to NULL. registration_endpoint clearing is the
// operator-facing path for disabling DCR on a saved issuer.
func TestUpdateRemoteSessionIssuer_ClearsNullableEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-clear"))
	require.NoError(t, err)
	require.NotNil(t, created.AuthorizationEndpoint)
	require.NotNil(t, created.TokenEndpoint)
	require.NotNil(t, created.RegistrationEndpoint)
	require.NotNil(t, created.JwksURI)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              nil,
		Issuer:                            nil,
		AuthorizationEndpoint:             &empty,
		TokenEndpoint:                     &empty,
		RegistrationEndpoint:              &empty,
		JwksURI:                           &empty,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	require.Nil(t, updated.AuthorizationEndpoint)
	require.Nil(t, updated.TokenEndpoint)
	require.Nil(t, updated.RegistrationEndpoint)
	require.Nil(t, updated.JwksURI)
}

// Omitting a nullable endpoint field keeps the existing value rather than
// clearing it. Guards against future regressions in the three-state
// COALESCE/CASE shape of UpdateRemoteSessionIssuer.
func TestUpdateRemoteSessionIssuer_OmittedKeepsExisting(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-keep"))
	require.NoError(t, err)
	require.NotNil(t, created.RegistrationEndpoint)

	newSlug := "idp-keep-renamed"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              &newSlug,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.RegistrationEndpoint)
	require.Equal(t, *created.RegistrationEndpoint, *updated.RegistrationEndpoint)
}

func TestUpdateRemoteSessionIssuer_BadRequestEmptySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-empty-slug"))
	require.NoError(t, err)

	empty := ""
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              &empty,
		Issuer:                            nil,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionIssuer_BadRequestEmptyIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-empty-issuer"))
	require.NoError(t, err)

	empty := ""
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		SessionToken:                      nil,
		ApikeyToken:                       nil,
		ProjectSlugInput:                  nil,
		ID:                                created.ID,
		Slug:                              nil,
		Issuer:                            &empty,
		AuthorizationEndpoint:             nil,
		TokenEndpoint:                     nil,
		RegistrationEndpoint:              nil,
		JwksURI:                           nil,
		ScopesSupported:                   nil,
		GrantTypesSupported:               nil,
		ResponseTypesSupported:            nil,
		TokenEndpointAuthMethodsSupported: nil,
		Oidc:                              nil,
		Passthrough:                       nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteRemoteSessionIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-delete"))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Subsequent reads should miss.
	_, err = ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestDeleteRemoteSessionIssuer_SerializedAgainstClientBinding proves the
// delete takes the client-binding advisory lock before its count-then-delete.
// Nothing else serializes the two: the delete only rewrites deleted_at, so its
// row lock never conflicts with the one a client insert's foreign key takes,
// and a create committing between the count and the delete would strand a live
// client on a deleted issuer. Holding the lock from another transaction (which
// is what every client writer does) must therefore block the delete until that
// transaction ends.
func TestDeleteRemoteSessionIssuer_SerializedAgainstClientBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-delete-lock"))
	require.NoError(t, err)

	issuerID, err := uuid.Parse(created.ID)
	require.NoError(t, err)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, repo.New(tx).LockRemoteSessionIssuerForClientBinding(ctx, issuerID))

	done := make(chan error, 1)
	go func() {
		done <- ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
			ID:               created.ID,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		})
	}()

	require.Never(t, func() bool { return len(done) > 0 }, 500*time.Millisecond, 25*time.Millisecond,
		"delete completed while another transaction held the client-binding lock")

	require.NoError(t, tx.Rollback(ctx))

	require.Eventually(t, func() bool { return len(done) > 0 }, 30*time.Second, 25*time.Millisecond,
		"delete did not complete after the client-binding lock was released")
	require.NoError(t, <-done)
}

// TestDeleteRemoteSessionIssuer_NotFound proves deleting an issuer the project
// does not own returns NotFound. The ownership pre-read makes a missing id and a
// non-owned id (another tenant's, or a platform issuer) indistinguishable, so
// the endpoint is no longer an existence oracle. No audit entry is written.
func TestDeleteRemoteSessionIssuer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount, "no audit entry when there was nothing to delete")
}

// fakeIssuerServer returns an httptest.Server that serves an RFC 8414
// well-known document derived from its own URL. Use the `mutate` callback to
// drop fields and exercise the warnings path.
func fakeIssuerServer(t *testing.T, mutate func(doc map[string]any)) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-authorization-server") {
			http.NotFound(w, r)
			return
		}
		doc := map[string]any{
			"issuer":                                server.URL,
			"authorization_endpoint":                server.URL + "/authorize",
			"token_endpoint":                        server.URL + "/token",
			"registration_endpoint":                 server.URL + "/register",
			"jwks_uri":                              server.URL + "/jwks",
			"scopes_supported":                      []string{"openid"},
			"grant_types_supported":                 []string{"authorization_code"},
			"response_types_supported":              []string{"code"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
			"code_challenge_methods_supported":      []string{"S256"},
		}
		if mutate != nil {
			mutate(doc)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDiscoverIssuerMetadataRejectsInsecureNonLoopbackIssuer(t *testing.T) {
	t.Parallel()

	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	_, err := remotesessions.DiscoverIssuerMetadata(t.Context(), policy, "http://identity.example")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTPS outside local loopback")
}

func TestDiscoverIssuerMetadataRejectsInsecureRedirect(t *testing.T) {
	t.Parallel()

	redirected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://identity.example/.well-known/oauth-authorization-server", http.StatusFound)
	}))
	t.Cleanup(redirected.Close)

	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	_, err = remotesessions.DiscoverIssuerMetadata(t.Context(), policy, redirected.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirect target must use HTTPS outside local loopback")
}

func TestDiscoverIssuerMetadataRejectsInsecureNonLoopbackEndpoints(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		endpoint string
		message  string
	}{
		"authorization_endpoint": {endpoint: "http://identity.example/authorize", message: "must use HTTPS outside local loopback"},
		"token_endpoint":         {endpoint: "http://identity.example/token", message: "must use HTTPS or the same local loopback origin"},
		"jwks_uri":               {endpoint: "http://identity.example/jwks", message: "must use HTTPS"},
		"registration_endpoint":  {endpoint: "http://identity.example/register", message: "must use HTTPS"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := fakeIssuerServer(t, func(doc map[string]any) {
				doc[name] = testCase.endpoint
			})
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)

			_, err = remotesessions.DiscoverIssuerMetadata(t.Context(), policy, server.URL)

			require.Error(t, err)
			require.Contains(t, err.Error(), "issuer metadata "+name+" "+testCase.message)
		})
	}
}

func TestDiscoverIssuerMetadataRejectsInsecureLoopbackServerEndpoints(t *testing.T) {
	t.Parallel()

	for name, endpoint := range map[string]string{
		"jwks_uri":              "http://127.0.0.1/jwks",
		"registration_endpoint": "http://localhost/register",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := fakeIssuerServer(t, func(doc map[string]any) {
				doc[name] = endpoint
			})
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
			require.NoError(t, err)

			_, err = remotesessions.DiscoverIssuerMetadata(t.Context(), policy, server.URL)

			require.Error(t, err)
			require.Contains(t, err.Error(), "issuer metadata "+name+" must use HTTPS")
		})
	}
}

func TestFetchRemoteSessionIssuerMetadata_HappyPath(t *testing.T) {
	t.Parallel()

	idp := devidptest.Launch(t, devidptest.LaunchOpts{})
	ctx, ti := newTestService(t)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           idp.OAuth21URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft)
	require.Equal(t, idp.OAuth21URL, draft.Issuer)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.NotNil(t, draft.JwksURI)
	require.NotNil(t, draft.RegistrationEndpoint)
	require.Empty(t, draft.DiscoveryWarnings)
}

func TestFetchRemoteSessionIssuerMetadata_WithWarnings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Drop jwks_uri and token_endpoint to force warnings.
	server := fakeIssuerServer(t, func(doc map[string]any) {
		delete(doc, "jwks_uri")
		delete(doc, "token_endpoint")
	})

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotEmpty(t, draft.DiscoveryWarnings)
	require.Nil(t, draft.JwksURI)
	require.Nil(t, draft.TokenEndpoint)
}

func TestFetchRemoteSessionIssuerMetadata_BadURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           "ftp://not-http",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// statusOnlyServer returns an httptest.Server that responds to the well-known
// path with the supplied HTTP status and no body. Use it to exercise the
// discoveryFailure → UserMessage path in FetchRemoteSessionIssuerMetadata.
func statusOnlyServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.well-known/oauth-authorization-server") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFetchRemoteSessionIssuerMetadata_NotFoundSurfacesWellKnownURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := statusOnlyServer(t, http.StatusNotFound)

	_, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Contains(t, err.Error(), "OAuth metadata not found at")
	require.Contains(t, err.Error(), "/.well-known/oauth-authorization-server")
}

func TestFetchRemoteSessionIssuerMetadata_UnexpectedStatusSurfacesCode(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := statusOnlyServer(t, http.StatusServiceUnavailable)

	_, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Contains(t, err.Error(), "Unexpected HTTP 503")
	require.Contains(t, err.Error(), "/.well-known/oauth-authorization-server")
}

func TestFetchRemoteSessionIssuerMetadata_OpenIDConfigurationFallback(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Upstream advertises metadata only under the OpenID Connect Discovery
	// path. Many IdPs (Auth0, Okta, Google) serve no oauth-authorization-server
	// document, so discovery must fall back to openid-configuration.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
			"registration_endpoint":  server.URL + "/register",
		})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server",
		"/.well-known/openid-configuration",
	}, probedPaths, "oauth-authorization-server first, then openid-configuration")
}

func TestFetchRemoteSessionIssuerMetadata_OriginStyleFallbackStripsPath(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Issuer carries a path component but the upstream serves metadata only at
	// the origin-root well-known URL (a common gateway / SPA catch-all shape).
	// The path-aware candidates 404, so discovery must fall back to the
	// path-stripped origin-style location.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		if r.URL.Path != "/.well-known/oauth-authorization-server" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"jwks_uri":               server.URL + "/jwks",
			"registration_endpoint":  server.URL + "/register",
		})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server/tenant",
		"/.well-known/openid-configuration/tenant",
		"/tenant/.well-known/openid-configuration",
		"/.well-known/oauth-authorization-server",
	}, probedPaths, "path-aware candidates 404, fall back to origin-style")
}

func TestFetchRemoteSessionIssuerMetadata_SkipsCatchAll200WithoutEndpoints(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A SPA/gateway catch-all answers every path-aware candidate with a 200
	// that parses but carries no usable OAuth endpoints. Discovery must treat
	// those as misses and keep probing until it reaches the origin-style
	// oauth-authorization-server URL that serves the real document.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 server.URL,
				"authorization_endpoint": server.URL + "/authorize",
				"token_endpoint":         server.URL + "/token",
				"jwks_uri":               server.URL + "/jwks",
				"registration_endpoint":  server.URL + "/register",
			})
			return
		}
		// Catch-all: 200 with no authorization_endpoint / token_endpoint.
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.AuthorizationEndpoint)
	require.NotNil(t, draft.TokenEndpoint)
	require.Equal(t, []string{
		"/.well-known/oauth-authorization-server/tenant",
		"/.well-known/openid-configuration/tenant",
		"/tenant/.well-known/openid-configuration",
		"/.well-known/oauth-authorization-server",
	}, probedPaths, "incomplete catch-all 200s skipped until the real document")
}

func TestFetchRemoteSessionIssuerMetadata_IncompleteDocReturnedAsLastResort(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Every candidate answers 200 with a parseable but endpoint-less document.
	// No candidate is usable, so discovery probes them all and surfaces the
	// first incomplete document (with warnings) rather than failing outright.
	var probedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probedPaths = append(probedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": server.URL})
	}))
	t.Cleanup(server.Close)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL + "/tenant",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, draft.AuthorizationEndpoint)
	require.Nil(t, draft.TokenEndpoint)
	require.NotEmpty(t, draft.DiscoveryWarnings)
	require.Len(t, probedPaths, 5, "all candidates probed before falling back to the incomplete document")
}

func TestFetchRemoteSessionIssuerMetadata_IngestsDocumentationURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	server := fakeIssuerServer(t, func(doc map[string]any) {
		doc["service_documentation"] = "https://idp.example.com/docs"
		doc["op_policy_uri"] = "https://idp.example.com/policy"
		doc["op_tos_uri"] = "https://idp.example.com/tos"
	})

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, draft.ServiceDocumentation)
	require.Equal(t, "https://idp.example.com/docs", *draft.ServiceDocumentation)
	require.NotNil(t, draft.OpPolicyURI)
	require.Equal(t, "https://idp.example.com/policy", *draft.OpPolicyURI)
	require.NotNil(t, draft.OpTosURI)
	require.Equal(t, "https://idp.example.com/tos", *draft.OpTosURI)
}

// An issuer that advertises no documentation metadata yields nil draft fields
// rather than empty strings.
func TestFetchRemoteSessionIssuerMetadata_AbsentDocumentationURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	server := fakeIssuerServer(t, nil)

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, draft.ServiceDocumentation)
	require.Nil(t, draft.OpPolicyURI)
	require.Nil(t, draft.OpTosURI)
}

// An upstream issuer controls these values, and downstream surfaces render them
// as links. Anything that is not an absolute http(s) URL is dropped at parse
// time so it never reaches the create form.
func TestFetchRemoteSessionIssuerMetadata_DropsNonHTTPDocumentationURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	server := fakeIssuerServer(t, func(doc map[string]any) {
		doc["service_documentation"] = "javascript:alert(1)"
		doc["op_policy_uri"] = "/relative/policy"
		doc["op_tos_uri"] = "mailto:legal@idp.example.com"
	})

	draft, err := ti.service.FetchRemoteSessionIssuerMetadata(ctx, &gen.FetchRemoteSessionIssuerMetadataPayload{
		Issuer:           server.URL,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Nil(t, draft.ServiceDocumentation, "javascript: scheme dropped")
	require.Nil(t, draft.OpPolicyURI, "relative URL dropped")
	require.Nil(t, draft.OpTosURI, "mailto: scheme dropped")
}

func TestCreateRemoteSessionIssuer_PersistsDocumentationURLs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serviceDocumentation := "https://idp.example.com/docs"
	opPolicyURI := "https://idp.example.com/policy"
	opTosURI := "https://idp.example.com/tos"

	payload := newIssuerPayload("idp-docs-create")
	payload.ServiceDocumentation = &serviceDocumentation
	payload.OpPolicyURI = &opPolicyURI
	payload.OpTosURI = &opTosURI

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ServiceDocumentation)
	require.Equal(t, serviceDocumentation, *created.ServiceDocumentation)
	require.NotNil(t, created.OpPolicyURI)
	require.Equal(t, opPolicyURI, *created.OpPolicyURI)
	require.NotNil(t, created.OpTosURI)
	require.Equal(t, opTosURI, *created.OpTosURI)

	// The values survive a round trip through the read path.
	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, fetched.ServiceDocumentation)
	require.Equal(t, serviceDocumentation, *fetched.ServiceDocumentation)
}

// An empty documentation URL on create is stored as NULL, not as an empty
// string, so readers cannot tell the two apart.
func TestCreateRemoteSessionIssuer_EmptyDocumentationURLStoredAsNull(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	empty := ""
	payload := newIssuerPayload("idp-docs-empty")
	payload.ServiceDocumentation = &empty

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.Nil(t, created.ServiceDocumentation)
}

// Discovery drops malformed values, but a caller holding the write scope can
// POST these fields without ever calling discover. The handler is the boundary
// that caller cannot skip.
func TestCreateRemoteSessionIssuer_RejectsNonHTTPDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	hostile := "javascript:alert(document.cookie)"
	payload := newIssuerPayload("idp-docs-hostile")
	payload.ServiceDocumentation = &hostile

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestCreateRemoteSessionIssuer_RejectsRelativeDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	relative := "/docs"
	payload := newIssuerPayload("idp-docs-relative")
	payload.OpTosURI = &relative

	_, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionIssuer_SetsDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-docs-update"))
	require.NoError(t, err)
	require.Nil(t, created.ServiceDocumentation)

	serviceDocumentation := "https://idp.example.com/docs"
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                   created.ID,
		ServiceDocumentation: &serviceDocumentation,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.ServiceDocumentation)
	require.Equal(t, serviceDocumentation, *updated.ServiceDocumentation)
}

// An omitted field keeps the stored value; only an explicit empty string clears
// it. Re-discovery relies on this to drop a URL the issuer no longer advertises.
func TestUpdateRemoteSessionIssuer_ClearsDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serviceDocumentation := "https://idp.example.com/docs"
	opTosURI := "https://idp.example.com/tos"
	payload := newIssuerPayload("idp-docs-clear")
	payload.ServiceDocumentation = &serviceDocumentation
	payload.OpTosURI = &opTosURI

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.ServiceDocumentation)

	empty := ""
	updated, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                   created.ID,
		ServiceDocumentation: &empty,
	})
	require.NoError(t, err)
	require.Nil(t, updated.ServiceDocumentation, "explicit empty string clears the column")
	require.NotNil(t, updated.OpTosURI, "an omitted field keeps its stored value")
	require.Equal(t, opTosURI, *updated.OpTosURI)
}

func TestUpdateRemoteSessionIssuer_RejectsNonHTTPDocumentationURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRemoteSessionIssuer(ctx, newIssuerPayload("idp-docs-update-hostile"))
	require.NoError(t, err)

	hostile := "javascript:alert(1)"
	_, err = ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:          created.ID,
		OpPolicyURI: &hostile,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestListRemoteSessionIssuers_InheritsPlatformIssuer proves the project-scoped
// listing surfaces a platform (global) issuer once the caller opts in.
func TestListRemoteSessionIssuers_InheritsPlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "inherit-platform-list")

	result, err := ti.service.ListRemoteSessionIssuers(ctx, &gen.ListRemoteSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	found := false
	for _, item := range result.Items {
		if item.ID == platformID.String() {
			found = true
			require.Empty(t, item.ProjectID, "platform issuer carries no project")
			require.Empty(t, item.OrganizationID, "platform issuer carries no organization")
		}
	}
	require.True(t, found, "platform issuer should be inherited into the project listing")
}

// TestGetRemoteSessionIssuer_ResolvesPlatformIssuerByID proves a project-scoped
// get-by-id resolves a platform issuer.
func TestGetRemoteSessionIssuer_ResolvesPlatformIssuerByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "inherit-platform-get")
	idStr := platformID.String()

	fetched, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               &idStr,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, idStr, fetched.ID)
	require.Empty(t, fetched.ProjectID)
	require.Empty(t, fetched.OrganizationID)
}

// TestGetRemoteSessionIssuer_BySlugDoesNotResolvePlatform proves slug lookups
// stay strictly project-scoped: a platform issuer is not slug-addressable from a
// tenant context, even though it is now resolvable by id.
func TestGetRemoteSessionIssuer_BySlugDoesNotResolvePlatform(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedGlobalRemoteIssuer(t, ctx, ti.conn, "inherit-platform-slug")
	slug := "inherit-platform-slug"

	_, err := ti.service.GetRemoteSessionIssuer(ctx, &gen.GetRemoteSessionIssuerPayload{
		ID:               nil,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestUpdateRemoteSessionIssuer_CannotMutatePlatformIssuer proves a project
// admin cannot edit a platform issuer through the project-scoped update.
func TestUpdateRemoteSessionIssuer_CannotMutatePlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "refuse-proj-update")
	renamed := "Hijacked"

	_, err := ti.service.UpdateRemoteSessionIssuer(ctx, &gen.UpdateRemoteSessionIssuerPayload{
		ID:                                platformID.String(),
		Slug:                              nil,
		Issuer:                            nil,
		Name:                              &renamed,
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
	requireOopsCode(t, err, oops.CodeNotFound)

	requirePlatformIssuerUnchanged(t, ctx, ti.conn, platformID)
}

// TestDeleteRemoteSessionIssuer_CannotDeletePlatformIssuer proves a project
// admin deleting a platform issuer gets a clean NotFound and the row survives.
// Without the ownership pre-read this returned a silent success (the
// project-scoped delete matched nothing and the handler swallowed ErrNoRows).
func TestDeleteRemoteSessionIssuer_CannotDeletePlatformIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	platformID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "refuse-proj-delete")

	err := ti.service.DeleteRemoteSessionIssuer(ctx, &gen.DeleteRemoteSessionIssuerPayload{
		ID:               platformID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	requirePlatformIssuerUnchanged(t, ctx, ti.conn, platformID)
}
