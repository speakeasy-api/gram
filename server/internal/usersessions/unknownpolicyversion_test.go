package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestUserSessionManagement_UnknownDelegatedPolicyVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := seedLegacyProjectIssuer(t, ctx, ti.conn, "future-policy-session")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// Use a valid current policy body with an unknown version: the version,
	// rather than malformed JSON or unknown scopes, must prevent authorization.
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{
		authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
	})
	require.NoError(t, err)
	raw, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion1, policy)
	require.NoError(t, err)
	now := time.Now()
	r := repo.New(ti.conn)
	session, err := r.CreateUserSession(ctx, repo.CreateUserSessionParams{
		UserSessionIssuerID:    issuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SubjectUrn:             urn.NewAgentSubject(uuid.New()),
		DelegatedGrants:        raw,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 999, Valid: true},
		Jti:                    "jti-" + uuid.NewString(),
		RefreshTokenHash:       conv.ToPGText("hash-" + uuid.NewString()),
		RefreshExpiresAt:       pgtype.Timestamptz{Time: now.Add(time.Hour), InfinityModifier: 0, Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: now.Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	lookup := repo.GetUserSessionByIDParams{
		ID:             session.ID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	}
	stored, err := r.GetUserSessionByID(ctx, lookup)
	require.NoError(t, err)
	require.True(t, stored.DelegatedGrantsVersion.Valid)
	require.EqualValues(t, 999, stored.DelegatedGrantsVersion.Int32)
	decoded, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(stored.DelegatedGrantsVersion.Int32), stored.DelegatedGrants)
	require.Error(t, err, "a manageable credential must not become an authorizable credential")
	require.Empty(t, decoded.Effective)

	listed, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{})
	require.NoError(t, err, "management must not decode the target session's future policy")
	require.Len(t, listed.Items, 1)
	require.Equal(t, session.ID.String(), listed.Items[0].ID)

	// The target's policy must not grant management rights to the caller.
	readOnlyCtx := withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()),
	)
	err = ti.service.RevokeUserSession(readOnlyCtx, &gen.RevokeUserSessionPayload{ID: session.ID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	require.False(t, jtiRevoked(t, ctx, ti.redis, session.Jti))

	writeCtx := withExactAuthzGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()),
	)
	require.NoError(t, ti.service.RevokeUserSession(writeCtx, &gen.RevokeUserSessionPayload{ID: session.ID.String()}))
	require.True(t, jtiRevoked(t, ctx, ti.redis, session.Jti))

	_, err = r.GetUserSessionByID(ctx, lookup)
	require.ErrorIs(t, err, pgx.ErrNoRows, "revoked sessions must no longer be returned")
}
