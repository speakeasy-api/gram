package usersessions_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestRevokeUserSessionClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "revoke-client-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "to-revoke")
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientRevoke)
	require.NoError(t, err)

	err = ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionClientRevoke)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	// Subsequent get returns not-found.
	_, err = ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Revoking a client must invalidate every access token already issued
// through it. Without the post-commit revocation-cache push those tokens keep
// validating until they expire on their own clock (up to an hour), because
// the Bearer path never rechecks the session row's liveness.
func TestRevokeUserSessionClient_CascadedSessionsRevoked(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "revoke-client-cascade",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "cascade-target")
	require.NoError(t, err)

	sessions := make([]repo.UserSession, 0, 3)
	for i := range 3 {
		session, err := seedUserSessionForClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), client.ID, urn.NewUserSubject(fmt.Sprintf("cascade-user-%d", i)))
		require.NoError(t, err)
		sessions = append(sessions, session)
	}

	// A session on the same issuer but bound to a different client must be
	// left alone: revoking one client cannot invalidate another's tokens.
	otherClient, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "cascade-bystander")
	require.NoError(t, err)
	bystander, err := seedUserSessionForClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), otherClient.ID, urn.NewUserSubject("cascade-bystander-user"))
	require.NoError(t, err)

	for _, session := range sessions {
		require.False(t, jtiRevoked(t, ctx, ti.redis, session.Jti))
	}

	beforeSessionRevokes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)

	err = ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	for _, session := range sessions {
		require.True(t, jtiRevoked(t, ctx, ti.redis, session.Jti), "client revoke must push every cascaded session jti into the revocation cache")
	}
	require.False(t, jtiRevoked(t, ctx, ti.redis, bystander.Jti), "another client's session must not be revoked")

	// One audit entry per cascaded session, not one covering the batch.
	afterSessionRevokes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionUserSessionRevoke)
	require.NoError(t, err)
	require.Equal(t, beforeSessionRevokes+int64(len(sessions)), afterSessionRevokes)
}

// A revocation-cache failure must not be reported as success: the client and
// its sessions are already committed as deleted, so a silent success would
// claim a security control took effect while access tokens are still live.
func TestRevokeUserSessionClient_RevocationCacheFailure(t *testing.T) {
	t.Parallel()

	revoker := &failingTokenRevoker{}
	ctx, ti := newTestServiceWithRevoker(t, revoker)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "revoke-client-cache-fail",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "cache-fail-target")
	require.NoError(t, err)

	for i := range 2 {
		_, err := seedUserSessionForClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), client.ID, urn.NewUserSubject(fmt.Sprintf("cache-fail-user-%d", i)))
		require.NoError(t, err)
	}

	err = ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)

	// Every jti is attempted rather than bailing on the first failure, so a
	// partial outage still shrinks the blast radius as far as it can.
	require.Equal(t, 2, revoker.Calls())

	// The database side committed regardless — the failure is in the cache
	// push, which happens after commit.
	_, err = ti.service.GetUserSessionClient(ctx, &gen.GetUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

// One jti failing must not stop the rest: a partial push still shrinks the
// blast radius, and the reported counts must reflect what actually landed.
func TestRevokeUserSessionClient_PartialRevocationFailure(t *testing.T) {
	t.Parallel()

	revoker := &partialTokenRevoker{}
	ctx, ti := newTestServiceWithRevoker(t, revoker)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "revoke-client-partial-fail",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "partial-fail-target")
	require.NoError(t, err)

	jtis := make([]string, 0, 3)
	for i := range 3 {
		session, err := seedUserSessionForClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), client.ID, urn.NewUserSubject(fmt.Sprintf("partial-fail-user-%d", i)))
		require.NoError(t, err)
		jtis = append(jtis, session.Jti)
	}
	revoker.failOn(jtis[1])

	err = ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
	require.ErrorContains(t, err, "2 of 3 sessions invalidated")

	// The failure must not short-circuit the two that would have succeeded.
	require.ElementsMatch(t, jtis, revoker.Calls())
}

func TestRevokeUserSessionClient_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRevokeUserSessionClient_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "rbac-revoke-client",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "rbac-target")
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Read-only on the project; revoke needs write.
	ctx = withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)

	err = ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               client.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestRevokeUserSessionClient_SiblingProjectNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	sp := seedSiblingProject(t, ctx, ti, "revoke-cli-sibling")

	err := ti.service.RevokeUserSessionClient(ctx, &gen.RevokeUserSessionClientPayload{
		ID:               sp.clientID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:             sp.clientID,
		ProjectID:      sp.projectID,
		OrganizationID: "",
	})
	require.NoError(t, err, "sibling project's client must survive the caller's revoke")
	require.False(t, live.Deleted)
}
