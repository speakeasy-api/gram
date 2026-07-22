// migrateissuer_test.go covers MigrateIssuer and GetIssuerMigratePreflight —
// consolidating two remote_session_issuers that describe the same upstream
// authorization server by re-pointing the source's clients onto the target and
// soft-deleting the source.
//
// The load-bearing test is TestMigrateIssuer_PreservesRemoteSessionWithoutReauth:
// it proves an already-authenticated subject's upstream token still resolves
// after a migration, which is the entire reason the endpoint re-points clients
// instead of deleting and re-creating them.

package remotesessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func migratePayload(sourceID, targetID string) *orgissuersgen.MigrateIssuerPayload {
	return &orgissuersgen.MigrateIssuerPayload{
		SourceID:     sourceID,
		TargetID:     targetID,
		SessionToken: nil,
		ApikeyToken:  nil,
	}
}

func migratePreflightPayload(sourceID, targetID string) *orgissuersgen.GetIssuerMigratePreflightPayload {
	return &orgissuersgen.GetIssuerMigratePreflightPayload{
		SourceID:     sourceID,
		TargetID:     targetID,
		SessionToken: nil,
		ApikeyToken:  nil,
	}
}

// seedDivergentOrgLevelRemoteIssuer creates an organization-level issuer whose
// authorization-server metadata differs from the one every other fixture uses,
// so the parity guard has something to reject.
func seedDivergentOrgLevelRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) uuid.UUID {
	t.Helper()
	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug,
		Issuer:                            "https://other-idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://other-idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://other-idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

// seedProjectRemoteIssuer creates a project-specific issuer that also carries
// its organization_id, matching what CreateIssuer persists. The shared
// createRemoteIssuerInProject fixture leaves organization_id NULL, which the
// org-scoped loader cannot see at all — that would make a scope-ladder test
// pass for the wrong reason.
func seedProjectRemoteIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	issuer, err := repo.New(conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(projectID),
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              slug,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)
	return issuer.ID
}

// TestMigrateIssuer_PreservesRemoteSessionWithoutReauth is the acceptance test
// for AIS-290: an existing remote_session keeps resolving its upstream access
// token across a migration, so no user re-authenticates. The resolved map is
// keyed by remote_session_issuer_id, so the key moves from source to target
// while the token value and the session row stay untouched.
func TestMigrateIssuer_PreservesRemoteSessionWithoutReauth(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	mgr := newResolveManager(t, ti.conn, enc)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-reauth-source", "")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-reauth-target")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "mig-reauth-usi")
	clientID := createRemoteClient(t, ctx, ti, sourceID, userIssuerID.String(), "mig-reauth-client")

	clientUUID, err := uuid.Parse(clientID)
	require.NoError(t, err)
	sourceUUID, err := uuid.Parse(sourceID)
	require.NoError(t, err)

	subject := urn.NewUserSubject("mig-reauth-subject")
	accessEnc, err := enc.Encrypt([]byte("upstream-access-token"))
	require.NoError(t, err)

	session, err := repo.New(ti.conn).InsertRemoteSession(ctx, repo.InsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userIssuerID,
		RemoteSessionClientID: clientUUID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)

	// Before: the token resolves under the source issuer's id.
	tokens, err := mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{sourceUUID: "upstream-access-token"}, tokens)

	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerMigrate)
	require.NoError(t, err)

	result, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 1, result.ClientsMigrated)
	require.True(t, result.SourceDeleted)
	require.Equal(t, targetID.String(), result.Issuer.ID)

	// After: the same token value resolves, now keyed by the target issuer.
	// Nothing re-authenticated; only the client's foreign key moved.
	tokens, err = mgr.ResolveAccessTokens(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuerID, subject, "")
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]string{targetID: "upstream-access-token"}, tokens)

	// The session row itself was neither deleted nor rewritten.
	q := repo.New(ti.conn)
	activeSessions, err := q.CountActiveRemoteSessionsByClientID(ctx, clientUUID)
	require.NoError(t, err)
	require.Equal(t, int64(1), activeSessions, "migration must not delete remote_sessions")

	client, err := q.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ProjectID:      *authCtx.ProjectID,
		ID:             clientUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
	require.Equal(t, targetID, client.RemoteSessionClient.RemoteSessionIssuerID, "the client should now point at the target issuer")

	// The source is soft-deleted and no longer resolvable in the org.
	_, err = q.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             sourceUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.Error(t, err, "the source issuer should be soft-deleted")

	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerMigrate)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter, "a dedicated migrate audit event should be recorded")

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionRemoteSessionIssuerMigrate)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, urn.NewRemoteSessionIssuer(sourceUUID).String(), metadata["source_remote_session_issuer_urn"])
	require.Equal(t, urn.NewRemoteSessionIssuer(targetID).String(), metadata["target_remote_session_issuer_urn"])
	require.InDelta(t, float64(1), metadata["clients_migrated"], 0)

	// Sanity: the surviving session still belongs to the same client row.
	require.Equal(t, clientUUID, session.RemoteSessionClientID)
}

// TestMigrateIssuer_EmptySourceIsIdempotentSuccess proves migrating an issuer
// with no clients succeeds with a zero count rather than erroring — the source
// is still soft-deleted, so a retried migration converges.
func TestMigrateIssuer_EmptySourceIsIdempotentSuccess(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-empty-source", "")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-empty-target")

	result, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID.String()))
	require.NoError(t, err)
	require.Equal(t, 0, result.ClientsMigrated)
	require.True(t, result.SourceDeleted)

	// A second attempt cannot find the now-deleted source.
	_, err = ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestMigrateIssuer_EndpointMismatchConflict proves the parity guard blocks a
// migration onto an issuer describing a different authorization server, which
// would silently break token refresh for the migrated sessions.
func TestMigrateIssuer_EndpointMismatchConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-mismatch-source", "")
	targetID := seedDivergentOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-mismatch-target")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	// The source survives a refused migration.
	sourceUUID, err := uuid.Parse(sourceID)
	require.NoError(t, err)
	_, err = repo.New(ti.conn).GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{
		ID:             sourceUUID,
		OrganizationID: conv.ToPGText(authCtx.ActiveOrganizationID),
	})
	require.NoError(t, err)
}

// TestMigrateIssuer_DuplicateBindingConflict proves the migration refuses when
// the same user_session_issuer already has a client on both issuers. Re-pointing
// would put two clients on one (user_session_issuer, remote_session_issuer) pair,
// which ResolveAccessTokens asserts against at serve time.
func TestMigrateIssuer_DuplicateBindingConflict(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Both issuers live in the caller's project: a sideways migration, so the
	// scope ladder permits the pair and only the binding conflict blocks it.
	sourceID := createRemoteIssuer(t, ctx, ti, "mig-dupe-source", "")
	targetID := createRemoteIssuer(t, ctx, ti, "mig-dupe-target", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "mig-dupe-usi")

	createRemoteClient(t, ctx, ti, sourceID, userIssuerID.String(), "mig-dupe-source-client")
	createRemoteClient(t, ctx, ti, targetID, userIssuerID.String(), "mig-dupe-target-client")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeConflict)

	// Both clients survive: the endpoint never silently drops a session.
	count, err := repo.New(ti.conn).CountRemoteSessionClientsByIssuerID(ctx, uuid.MustParse(sourceID))
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

// TestMigrateIssuer_CrossProjectBadRequest proves the scope ladder refuses a
// sideways migration into a different project, which would leave this project's
// clients hanging off another project's issuer.
func TestMigrateIssuer_CrossProjectBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-xproj-source", "")
	otherProjectID := createProject(t, ctx, ti.conn, "mig-xproj-other")
	targetID := seedProjectRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, otherProjectID, "mig-xproj-target")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID, targetID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestMigrateIssuer_OrganizationToProjectBadRequest proves the ladder refuses a
// downward migration, which would narrow the issuer's visibility below that of
// the clients being moved onto it.
func TestMigrateIssuer_OrganizationToProjectBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-down-source")
	targetID := createRemoteIssuer(t, ctx, ti, "mig-down-target", "")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(sourceID.String(), targetID))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestMigrateIssuer_SameIssuerBadRequest proves an issuer cannot be migrated
// onto itself, which would soft-delete the very issuer holding the clients.
func TestMigrateIssuer_SameIssuerBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "mig-self-issuer", "")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(issuerID, issuerID))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// TestMigrateIssuer_CrossOrgNotFound proves an issuer owned by another
// organization is invisible as either end of a migration.
func TestMigrateIssuer_CrossOrgNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherOrgID := createOrganization(t, ctx, ti.conn, "mig-other-org")
	foreignIssuerID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, otherOrgID, "mig-foreign-issuer")
	localIssuerID := createRemoteIssuer(t, ctx, ti, "mig-local-issuer", "")

	_, err := ti.service.MigrateIssuer(ctx, migratePayload(localIssuerID, foreignIssuerID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.MigrateIssuer(ctx, migratePayload(foreignIssuerID.String(), localIssuerID))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// TestMigrateIssuer_RBACForbidden proves the mutation requires org:admin, not
// merely org:read.
func TestMigrateIssuer_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-rbac-source", "")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-rbac-target")

	readOnlyCtx := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	_, err := ti.service.MigrateIssuer(readOnlyCtx, migratePayload(sourceID, targetID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// TestGetIssuerMigratePreflight_Clean reports the impact of a migration that
// would succeed: the clients that move, and no blockers.
func TestGetIssuerMigratePreflight_Clean(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-pf-clean-source", "")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-pf-clean-target")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "mig-pf-clean-usi")
	createRemoteClient(t, ctx, ti, sourceID, userIssuerID.String(), "mig-pf-clean-client")

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(sourceID, targetID.String()))
	require.NoError(t, err)
	require.True(t, preflight.CanMigrate)
	require.Equal(t, 1, preflight.ClientCount)
	require.Empty(t, preflight.EndpointMismatches)
	require.Empty(t, preflight.ConflictingMcpServerNames)
}

// TestGetIssuerMigratePreflight_ReportsEndpointMismatch proves the dialog can
// show the blocking fields before the admin confirms.
func TestGetIssuerMigratePreflight_ReportsEndpointMismatch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-pf-mismatch-source", "")
	targetID := seedDivergentOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-pf-mismatch-target")

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(sourceID, targetID.String()))
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.ElementsMatch(t, []string{"issuer", "token_endpoint", "authorization_endpoint"}, preflight.EndpointMismatches)
}

// TestGetIssuerMigratePreflight_ReportsConflictingBindings proves the preflight
// names the conflicts an admin must resolve, matching what the mutation rejects.
func TestGetIssuerMigratePreflight_ReportsConflictingBindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-pf-dupe-source", "")
	targetID := createRemoteIssuer(t, ctx, ti, "mig-pf-dupe-target", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "mig-pf-dupe-usi")

	createRemoteClient(t, ctx, ti, sourceID, userIssuerID.String(), "mig-pf-dupe-source-client")
	createRemoteClient(t, ctx, ti, targetID, userIssuerID.String(), "mig-pf-dupe-target-client")

	preflight, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(sourceID, targetID))
	require.NoError(t, err)
	require.False(t, preflight.CanMigrate)
	require.NotEmpty(t, preflight.ConflictingMcpServerNames)
}

// TestGetIssuerMigratePreflight_RBACForbidden proves the preflight requires
// org:read.
func TestGetIssuerMigratePreflight_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sourceID := createRemoteIssuer(t, ctx, ti, "mig-pf-rbac-source", "")
	targetID := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "mig-pf-rbac-target")

	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetIssuerMigratePreflight(ctx, migratePreflightPayload(sourceID, targetID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}
