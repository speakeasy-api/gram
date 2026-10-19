package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestRevokeUserSessionConsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "revoke-consent-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "revoke-consent-client")
	require.NoError(t, err)

	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("revoke-target"))
	require.NoError(t, err)

	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	variant, err := repo.New(ti.conn).CreateUserSessionConsent(ctx, repo.CreateUserSessionConsentParams{
		SubjectUrn: consent.SubjectUrn, UserSessionClientID: client.ID, RemoteSetHash: "different-upstream-permissions",
	})
	require.NoError(t, err)
	other, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("another-person"))
	require.NoError(t, err)
	session, err := seedUserSessionForClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), client.ID, consent.SubjectUrn)
	require.NoError(t, err)
	for _, parent := range []repo.UserSessionConsent{consent, variant} {
		_, err := repo.New(ti.conn).CreateUserSessionConsentResource(ctx, repo.CreateUserSessionConsentResourceParams{
			ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, UserSessionConsentID: parent.ID,
			Resource: "mcp_server:" + uuid.NewString(), InventoryHash: "approved-inventory", RemoteSetHash: parent.RemoteSetHash,
			SessionDurationHours: 1,
		})
		require.NoError(t, err)
	}

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)

	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionConsentRevoke)
	require.NoError(t, err)
	require.Equal(t, before+2, after)
	for _, parent := range []repo.UserSessionConsent{consent, variant} {
		_, err := repo.New(ti.conn).GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{ID: parent.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	remaining, err := repo.New(ti.conn).CountLiveUserSessionConsentResourcesFixture(ctx, repo.CountLiveUserSessionConsentResourcesFixtureParams{ProjectID: *auth.ProjectID, ConsentIds: []uuid.UUID{consent.ID, variant.ID}})
	require.NoError(t, err)
	require.Zero(t, remaining)
	_, err = repo.New(ti.conn).GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{ID: other.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err, "another person's approval survives")
	_, err = repo.New(ti.conn).GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{ID: session.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err, "withdrawal does not revoke existing sessions")

	// A second revoke is a no-op against an already soft-deleted row, so the
	// row is no longer visible — the same id reads as not-found.
	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevokeUserSessionConsent_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevokeUserSessionConsent_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "rbac-revoke-consent",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "rbac-revoke-client")
	require.NoError(t, err)

	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("rbac-target"))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only on the project; revoke needs write on the owning issuer.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	err = ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               consent.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRevokeUserSessionConsent_SiblingProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "revoke-consent-sibling")

	err := ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{
		ID:               sp.consentID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{
		ID:             sp.consentID,
		ProjectID:      sp.projectID,
		OrganizationID: "",
	})
	require.NoError(t, err, "sibling project's consent must survive the caller's revoke")
	require.False(t, live.Deleted)
}

func TestRevokeUserSessionConsent_ClientRevokedWhileWaiting(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		Slug: "consent-client-race", AuthnChallengeMode: "chain", SessionDurationHours: 24,
	})
	require.NoError(t, err)
	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "consent-client-race")
	require.NoError(t, err)
	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("consent-race-user"))
	require.NoError(t, err)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	txRepo := repo.New(tx)
	_, err = txRepo.LockUserSessionConsentClient(ctx, repo.LockUserSessionConsentClientParams{
		UserSessionClientID: client.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
	})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- ti.service.RevokeUserSessionConsent(ctx, &gen.RevokeUserSessionConsentPayload{ID: consent.ID.String()})
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, ti.conn, testenv.BackendPID(tx), "%LockUserSessionConsentClient%")
	_, err = txRepo.RevokeUserSessionClient(ctx, repo.RevokeUserSessionClientParams{
		ID: client.ID, ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireOopsCode(t, <-done, oops.CodeNotFound)
}
