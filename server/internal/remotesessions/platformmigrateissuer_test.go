// platformmigrateissuer_test.go covers the platform-admin convergence surface —
// listGlobalIssuerConvergenceCandidates, getGlobalIssuerMigratePreflight, and
// migrateToGlobalIssuer — which folds one organization's remote_session_issuer
// onto the shared platform catalog entry for the same upstream.
//
// Two tests carry the weight. TestMigrateToGlobalIssuer_PreservesRemoteSessionWithoutReauth
// proves an already-authenticated subject's token still resolves across the tier
// crossing. TestMigrateToGlobalIssuer_ClientStaysReachableToItsOrganization
// proves the migrated clients stay reachable to their own organization
// afterwards, which is not automatic: a platform issuer carries no
// organization_id, so the issuer arm of the org-admin reachability predicate
// stops matching and the client's own organization_id becomes the only path back.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	adminrsgen "github.com/speakeasy-api/gram/server/gen/admin_remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func platformMigratePayload(sourceID, targetID string) *adminrsgen.MigrateToGlobalIssuerPayload {
	return &adminrsgen.MigrateToGlobalIssuerPayload{
		SourceID:     sourceID,
		TargetID:     targetID,
		SessionToken: nil,
	}
}

func platformPreflightPayload(sourceID, targetID string) *adminrsgen.GetGlobalIssuerMigratePreflightPayload {
	return &adminrsgen.GetGlobalIssuerMigratePreflightPayload{
		SourceID:     sourceID,
		TargetID:     targetID,
		SessionToken: nil,
	}
}

func convergenceCandidatesPayload(targetID string) *adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload {
	return &adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload{
		TargetID:     targetID,
		Cursor:       nil,
		Limit:        nil,
		SessionToken: nil,
	}
}

// seedConvergencePlatformIssuer creates a platform issuer describing the same
// upstream as the shared tenant fixtures, so a migration onto it clears the
// endpoint parity guard. seedGlobalRemoteIssuer derives its URL from the slug
// and therefore can never be a parity match for a tenant issuer; tests that
// only need a platform row to exist keep using it.
func seedConvergencePlatformIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	return seedRemoteIssuerWithURL(t, ctx, conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, slug, "https://idp.example.com")
}

// seedIssuerWithEndpoints creates an issuer whose issuer identifier and
// endpoints are set independently. seedRemoteIssuerWithURL derives the endpoints
// from the issuer URL, which cannot express the case the canonical-parity tests
// need: one identifier spelled two ways while both sides point at byte-identical
// endpoints.
func seedIssuerWithEndpoints(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID pgtype.Text, slug, issuerURL, authorizeURL, tokenURL string) uuid.UUID {
	t.Helper()
	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    organizationID,
		Slug:                              slug,
		Issuer:                            issuerURL,
		AuthorizationEndpoint:             conv.ToPGText(authorizeURL),
		TokenEndpoint:                     conv.ToPGText(tokenURL),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

// TestMigrateToGlobalIssuer_PreservesRemoteSessionWithoutReauth is the acceptance
// test for AIS-335: an existing remote_session keeps resolving its upstream
// access token after its client is re-pointed from an organization-level issuer
// onto a platform issuer, so no user re-authenticates across the tier crossing.
func TestMigrateToGlobalIssuer_PreservesRemoteSessionWithoutReauth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	mgr := newResolveManager(t, ti.conn, enc)

	sourceID := createRemoteIssuer(t, ctx, ti, "plat-mig-source", "")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-mig-target")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "plat-mig-usi")
	clientID := createRemoteClient(t, ctx, ti, sourceID, userIssuerID.String(), "plat-mig-client")

	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)
	sourceUUID, err := uuid.Parse(sourceID)
	require.NoError(t, err)

	subject := urn.NewUserSubject("plat-mig-subject")
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)

	_, err = repo.New(ti.conn).UpsertRemoteSession(ctx, repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		Scopes:                []string{},
	})
	require.NoError(t, err)

	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]remotesessions.UpstreamToken{sourceUUID: {Token: "upstream-access-token", Resource: "", RemoteSessionClientID: clientUUID}}, tokens)

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID, targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, result.ClientsMigrated)
	require.True(t, result.SourceDeleted)
	require.Equal(t, targetID.String(), result.Issuer.ID)

	// The same token value resolves, now keyed by the platform issuer. Only the
	// client's foreign key moved.
	tokens, err = mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]remotesessions.UpstreamToken{targetID: {Token: "upstream-access-token", Resource: "", RemoteSessionClientID: clientUUID}}, tokens)

	q := repo.New(ti.conn)
	activeSessions, err := q.CountActiveRemoteSessionsByClientID(ctx, clientUUID)
	require.NoError(t, err)
	require.Equal(t, int64(1), activeSessions, "migration must not delete remote_sessions")

	_, err = q.GetTenantRemoteSessionIssuerByID(ctx, sourceUUID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the source issuer should be soft-deleted")
}

// TestMigrateToGlobalIssuer_ClientStaysReachableToItsOrganization proves a
// migrated client is still listable, and still tenant-owned, once it sits on a
// platform issuer.
//
// This is the assertion the tier crossing puts at risk. Org-admin reads reach a
// client through (i.organization_id = @org OR c.organization_id = @org), and a
// platform issuer's organization_id is NULL, so afterwards only the client's own
// column can match. Every tenant client writer populates it, which is what makes
// the crossing safe; this test is what would fail if that stopped being true.
func TestMigrateToGlobalIssuer_ClientStaysReachableToItsOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-reach-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-reach-target")
	clientUUID := seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, sourceID, "plat-reach-client")

	q := repo.New(ti.conn)

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, result.ClientsMigrated)

	// The org-admin listing is the surface that would go blind.
	listed, err := q.ListOrganizationRemoteSessionClientsByIssuerID(ctx, repo.ListOrganizationRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: targetID,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		Cursor:                uuid.NullUUID{},
		LimitValue:            50,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1, "the migrated client must still list for its organization")
	require.Equal(t, clientUUID, listed[0].RemoteSessionClient.ID)
	require.Equal(t, targetID, listed[0].RemoteSessionClient.RemoteSessionIssuerID)
	require.Equal(t, authCtx.ActiveOrganizationID, listed[0].RemoteSessionClient.OrganizationID.String, "the client keeps its own organization")

	// Moving onto a platform issuer must not promote the client itself: a global
	// client is one with no project and no organization, and this one has both.
	_, err = q.GetGlobalRemoteSessionClientByID(ctx, clientUUID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "a tenant client must not become a global client by migrating onto a platform issuer")
}

// TestMigrateToGlobalIssuer_LegacyProjectSourceWithNullOrganization proves a
// project-scoped issuer written before organization_id existed on the table can
// still be consolidated. scopeOf classifies it as project scope, and the upward
// branch of the scope ladder short-circuits before any organization comparison
// when the target is a platform issuer.
//
// It carries a client whose organization_id is NULL too, the shape that predates
// the column. Such a row is not made worse by the migration: the source issuer's
// organization_id was already NULL, so the issuer arm of the org-admin
// reachability predicate never matched it either. The project tier still reaches
// it, since those queries filter on c.project_id and never join the issuer.
func TestMigrateToGlobalIssuer_LegacyProjectSourceWithNullOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	// createRemoteIssuerInProject deliberately leaves organization_id NULL.
	sourceID := createRemoteIssuerInProject(t, ctx, ti.conn, *authCtx.ProjectID, "plat-legacy-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-legacy-target")
	clientUUID := seedProjectRemoteClientNoOrg(t, ctx, ti.conn, *authCtx.ProjectID, sourceID, "plat-legacy-client")

	q := repo.New(ti.conn)

	source, err := q.GetTenantRemoteSessionIssuerByID(ctx, sourceID)
	require.NoError(t, err)
	require.False(t, source.OrganizationID.Valid, "fixture should start with a NULL organization_id")

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, result.ClientsMigrated)

	// A NULL backfill value leaves the column alone rather than erroring.
	migrated, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      *authCtx.ProjectID,
		ID:             clientUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err, "the project tier still reaches the client through c.project_id")
	require.Equal(t, targetID, migrated.RemoteSessionClient.RemoteSessionIssuerID)
	require.False(t, migrated.RemoteSessionClient.OrganizationID.Valid, "a NULL source organization leaves the client's organization_id alone")

	_, err = q.GetTenantRemoteSessionIssuerByID(ctx, sourceID)
	require.ErrorIs(t, err, pgx.ErrNoRows, "the source issuer should be soft-deleted")
	require.True(t, result.SourceDeleted)
}

// TestMigrateToGlobalIssuer_CanonicalIssuerURLMatch proves two spellings that
// differ only by a trailing slash consolidate. Those duplicates are the
// population this endpoint exists to clean up, so the parity guard compares the
// issuer identifier canonically.
func TestMigrateToGlobalIssuer_CanonicalIssuerURLMatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Same endpoints on both sides, so the only difference is how the issuer
	// identifier is spelled.
	authorizeURL := "https://canon-idp.example.com/authorize"
	tokenURL := "https://canon-idp.example.com/token"
	sourceID := seedIssuerWithEndpoints(t, ctx, ti.conn, conv.ToPGText(authCtx.ActiveOrganizationID), "plat-canon-source", "https://canon-idp.example.com/", authorizeURL, tokenURL)
	targetID := seedIssuerWithEndpoints(t, ctx, ti.conn, pgtype.Text{String: "", Valid: false}, "plat-canon-target", "https://canon-idp.example.com", authorizeURL, tokenURL)

	preflight, err := ti.service.GetGlobalIssuerMigratePreflight(withAdmin(t, ctx), platformPreflightPayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Empty(t, preflight.EndpointMismatches, "a trailing-slash difference in the issuer identifier is not a mismatch")
	require.True(t, preflight.CanMigrate)

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.True(t, result.SourceDeleted)
}

// TestMigrateToGlobalIssuer_EndpointMismatchConflict proves parity is still a hard
// block: the endpoints are compared literally even though the issuer identifier
// is compared canonically.
func TestMigrateToGlobalIssuer_EndpointMismatchConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedDivergentOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-mismatch-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-mismatch-target")

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeConflict)

	preflight, err := ti.service.GetGlobalIssuerMigratePreflight(withAdmin(t, ctx), platformPreflightPayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.Contains(t, mismatchedFields(preflight.EndpointMismatches), "issuer")
}

// TestMigrateToGlobalIssuer_DuplicateBindingConflict proves the
// one-client-per-(user_session_issuer, remote_session_issuer) invariant is still
// enforced when one organization folds two of its own issuers onto the same
// platform target. No database constraint expresses it.
func TestMigrateToGlobalIssuer_DuplicateBindingConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-dup-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-dup-target")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "plat-dup-usi")

	seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, sourceID, "plat-dup-source-client", userIssuerID)
	seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, targetID, "plat-dup-target-client", userIssuerID)

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeConflict)

	preflight, err := ti.service.GetGlobalIssuerMigratePreflight(withAdmin(t, ctx), platformPreflightPayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.NotEmpty(t, preflight.ConflictingMcpServerNames)
}

// TestMigrateToGlobalIssuer_GlobalSourceBadRequest proves a platform issuer named
// as the source is refused with an explanation rather than a 404 for a row the
// admin can see in the catalog.
func TestMigrateToGlobalIssuer_GlobalSourceBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "plat-globalsrc-source")
	targetID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "plat-globalsrc-target")

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestMigrateToGlobalIssuer_UnknownSourceNotFound proves an id that names nothing
// is still a plain 404, so the bad-request above is specific to the global
// partition rather than a blanket answer.
func TestMigrateToGlobalIssuer_UnknownSourceNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	targetID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "plat-unknown-target")

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(uuid.New().String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestMigrateToGlobalIssuer_TenantTargetNotFound proves the target must be a
// platform issuer: an organization-level target is not addressable here.
func TestMigrateToGlobalIssuer_TenantTargetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-tenanttgt-source")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-tenanttgt-target")

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestMigrateToGlobalIssuer_CrossOrgSourceSucceeds proves the endpoint is
// cross-tenant by construction: the source belongs to an organization the caller
// is not a member of, which the org-admin endpoint deliberately forbids.
func TestMigrateToGlobalIssuer_CrossOrgSourceSucceeds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "plat-crossorg-other")
	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrgID, "plat-crossorg-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-crossorg-target")
	clientUUID := seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrgID, sourceID, "plat-crossorg-client")

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, result.ClientsMigrated)

	listed, err := repo.New(ti.conn).ListOrganizationRemoteSessionClientsByIssuerID(ctx, repo.ListOrganizationRemoteSessionClientsByIssuerIDParams{
		RemoteSessionIssuerID: targetID,
		OrganizationID:        conv.ToPGText(otherOrgID),
		Cursor:                uuid.NullUUID{},
		LimitValue:            50,
	})
	require.NoError(t, err)
	require.Len(t, listed, 1, "the migrated client must remain reachable to its own organization")
	require.Equal(t, clientUUID, listed[0].RemoteSessionClient.ID)
}

// TestMigrateToGlobalIssuer_EmptySourceIsIdempotentSuccess proves migrating an
// issuer with no clients succeeds with a zero count rather than erroring.
func TestMigrateToGlobalIssuer_EmptySourceIsIdempotentSuccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-empty-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-empty-target")

	result, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 0, result.ClientsMigrated)
	require.True(t, result.SourceDeleted)
}

func TestMigrateToGlobalIssuer_SameIssuerBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	targetID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "plat-same-target")

	_, err := ti.service.MigrateToGlobalIssuer(withAdmin(t, ctx), platformMigratePayload(targetID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestPlatformMigrateMethods_RequireAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-authz-source")
	targetID := seedGlobalRemoteIssuer(t, ctx, ti.conn, "plat-authz-target")

	_, err := ti.service.MigrateToGlobalIssuer(ctx, platformMigratePayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.GetGlobalIssuerMigratePreflight(ctx, platformPreflightPayload(sourceID.String(), targetID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.ListGlobalIssuerConvergenceCandidates(ctx, convergenceCandidatesPayload(targetID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestGetGlobalIssuerMigratePreflight_ReportsTargetTenantClientCount proves the
// preflight surfaces how many tenant-owned clients the target already carries.
// Those permanently block deleting the platform issuer and only their owning
// organizations can clear them, so a migration is effectively one-way.
func TestGetGlobalIssuerMigratePreflight_ReportsTargetTenantClientCount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-oneway-source")
	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-oneway-target")

	preflight, err := ti.service.GetGlobalIssuerMigratePreflight(withAdmin(t, ctx), platformPreflightPayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 0, preflight.TargetTenantClientCount)
	require.True(t, preflight.CanMigrate)

	otherOrgID := createOrganization(t, ctx, ti.conn, "plat-oneway-other")
	seedOrgLevelRemoteClient(t, ctx, ti.conn, otherOrgID, targetID, "plat-oneway-existing-client")

	preflight, err = ti.service.GetGlobalIssuerMigratePreflight(withAdmin(t, ctx), platformPreflightPayload(sourceID.String(), targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, preflight.TargetTenantClientCount)
	require.True(t, preflight.CanMigrate, "another organization's client on the target is a warning, not a blocker")
}

// TestListGlobalIssuerConvergenceCandidates_MatchesCanonicalSpellings proves discovery
// finds the trailing-slash and default-port duplicates that convergence exists
// to clean up, and that it never offers a platform issuer or an unrelated
// upstream.
func TestListGlobalIssuerConvergenceCandidates_MatchesCanonicalSpellings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	targetID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "plat-cand-target", "https://cand-idp.example.com")

	exact := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "plat-cand-exact", "https://cand-idp.example.com")
	slashed := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "plat-cand-slash", "https://cand-idp.example.com/")
	ported := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "plat-cand-port", "https://cand-idp.example.com:443")
	unrelated := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), "plat-cand-other", "https://elsewhere.example.com")
	otherPlatform := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "plat-cand-otherplat", "https://cand-idp.example.com")

	result, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), convergenceCandidatesPayload(targetID.String()))
	require.NoError(t, err)

	found := make(map[string]*adminrsgen.IssuerConvergenceCandidate, len(result.Items))
	for _, item := range result.Items {
		found[item.Issuer.ID] = item
	}

	require.Contains(t, found, exact.String())
	require.Contains(t, found, slashed.String(), "a trailing-slash duplicate is exactly what convergence is for")
	require.Contains(t, found, ported.String(), "an explicit default port is the same authority")
	require.NotContains(t, found, unrelated.String(), "a different upstream must never be offered")
	require.NotContains(t, found, otherPlatform.String(), "platform issuers are not tenant convergence candidates")
	require.NotContains(t, found, targetID.String(), "the target must not offer itself")

	// The trailing-slash candidate is discovered because its issuer identifier
	// canonicalizes to the target's, and the identifier is not reported as a
	// mismatch. Its endpoints, which this fixture derives from the URL and so
	// spells with a doubled slash, still are: endpoints are request targets rather
	// than identities and stay compared literally.
	require.NotContains(t, mismatchedFields(found[slashed.String()].EndpointMismatches), "issuer", "a canonical match is not an issuer mismatch")
	require.Contains(t, mismatchedFields(found[slashed.String()].EndpointMismatches), "token_endpoint")

	require.Empty(t, found[exact.String()].EndpointMismatches, "an identical issuer must have no blockers at all")
	require.Equal(t, orgID, found[exact.String()].OrganizationID)
}

// TestListGlobalIssuerConvergenceCandidates_ReportsInlineBlockers proves a near-miss
// candidate is offered with a field-level explanation rather than hidden, so the
// admin sees why it cannot be migrated instead of facing an empty list.
func TestListGlobalIssuerConvergenceCandidates_ReportsInlineBlockers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	targetID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "plat-blocker-target", "https://blocker-idp.example.com")

	divergent, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:      uuid.NullUUID{},
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:           "plat-blocker-source",
		Issuer:         "https://blocker-idp.example.com",
		// Same issuer identity, different token endpoint: a near miss the picker
		// must surface rather than drop.
		AuthorizationEndpoint:             conv.ToPGText("https://blocker-idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://blocker-idp.example.com/oauth/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)

	result, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), convergenceCandidatesPayload(targetID.String()))
	require.NoError(t, err)

	var candidate *adminrsgen.IssuerConvergenceCandidate
	for _, item := range result.Items {
		if item.Issuer.ID == divergent.ID.String() {
			candidate = item
		}
	}
	require.NotNil(t, candidate, "a near-miss candidate must still be listed")
	require.Contains(t, mismatchedFields(candidate.EndpointMismatches), "token_endpoint")
	require.NotContains(t, mismatchedFields(candidate.EndpointMismatches), "issuer")
}

func TestListGlobalIssuerConvergenceCandidates_TargetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), convergenceCandidatesPayload(uuid.New().String()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestListGlobalIssuerConvergenceCandidates_RejectsTenantTarget proves the listing is
// anchored on a platform issuer: asking for candidates against an organization's
// own issuer is not a supported question.
func TestListGlobalIssuerConvergenceCandidates_RejectsTenantTarget(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "plat-cand-tenanttgt")

	_, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), convergenceCandidatesPayload(targetID.String()))
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestListGlobalIssuerConvergenceCandidates_Paginates proves the cursor and the
// descending-id ordering agree, so paging cannot skip or repeat a candidate. The
// listing is the only way a platform admin discovers who could converge, and a
// silently dropped row is an organization that never gets migrated.
func TestListGlobalIssuerConvergenceCandidates_Paginates(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID

	targetID := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, pgtype.Text{String: "", Valid: false}, "plat-page-target", "https://page-idp.example.com")

	seeded := make(map[string]bool, 3)
	for _, slug := range []string{"plat-page-a", "plat-page-b", "plat-page-c"} {
		id := seedRemoteIssuerWithURL(t, ctx, ti.conn, uuid.NullUUID{}, conv.ToPGText(orgID), slug, "https://page-idp.example.com")
		seeded[id.String()] = true
	}

	limit := 2
	first, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), &adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload{
		TargetID:     targetID.String(),
		Cursor:       nil,
		Limit:        &limit,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.NotNil(t, first.NextCursor, "a full page must offer a cursor")

	second, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), &adminrsgen.ListGlobalIssuerConvergenceCandidatesPayload{
		TargetID:     targetID.String(),
		Cursor:       first.NextCursor,
		Limit:        &limit,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, second.Items, 1, "the tail page holds the remaining candidate")

	seen := make(map[string]bool, 3)
	for _, item := range append(append([]*adminrsgen.IssuerConvergenceCandidate{}, first.Items...), second.Items...) {
		require.False(t, seen[item.Issuer.ID], "a candidate must not appear on two pages")
		seen[item.Issuer.ID] = true
	}
	require.Equal(t, seeded, seen, "paging must return every candidate exactly once")
}

// TestListGlobalIssuerConvergenceCandidates_DerivesLegacyOwnerFromProject proves
// a project-scoped issuer written before organization_id existed on the table
// still names the organization that owns it. Those legacy duplicates are the
// population convergence exists to clean up, so reporting them as belonging to
// nobody would hide the owner of the rows most likely to be migrated.
func TestListGlobalIssuerConvergenceCandidates_DerivesLegacyOwnerFromProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	targetID := seedConvergencePlatformIssuer(t, ctx, ti.conn, "plat-legacyowner-target")

	// createRemoteIssuerInProject deliberately leaves organization_id NULL.
	legacyID := createRemoteIssuerInProject(t, ctx, ti.conn, *authCtx.ProjectID, "plat-legacyowner-source")

	legacy, err := repo.New(ti.conn).GetTenantRemoteSessionIssuerByID(ctx, legacyID)
	require.NoError(t, err)
	require.False(t, legacy.OrganizationID.Valid, "fixture should start with a NULL organization_id")

	result, err := ti.service.ListGlobalIssuerConvergenceCandidates(withAdmin(t, ctx), convergenceCandidatesPayload(targetID.String()))
	require.NoError(t, err)

	var candidate *adminrsgen.IssuerConvergenceCandidate
	for _, item := range result.Items {
		if item.Issuer.ID == legacyID.String() {
			candidate = item
		}
	}
	require.NotNil(t, candidate, "a legacy project-scoped issuer must still be offered as a candidate")
	require.Equal(t, authCtx.ActiveOrganizationID, candidate.OrganizationID, "the owner is derived from the issuer's project")
}
