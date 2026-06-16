package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	gen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListUserSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "list-sessions-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	for _, principal := range []urn.SessionSubject{urn.NewUserSubject("p1"), urn.NewUserSubject("p2"), urn.NewUserSubject("p3")} {
		_, err := seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), principal)
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 3)
	require.Nil(t, got.NextCursor, "non-paged result must not return a cursor")
}

func TestListUserSessions_FilterByPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "sessions-by-principal",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("keep"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("skip"))
	require.NoError(t, err)

	target := "user:keep"
	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          &target,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, target, got.Items[0].SubjectUrn)
}

func TestListUserSessions_FilterByIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerA, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "sessions-by-issuer-A",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerB, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "sessions-by-issuer-B",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuerA.ID), urn.NewUserSubject("a"))
	require.NoError(t, err)
	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuerB.ID), urn.NewUserSubject("b"))
	require.NoError(t, err)

	filter := issuerA.ID
	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: &filter,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, issuerA.ID, got.Items[0].UserSessionIssuerID)
}

func TestListUserSessions_BadCursor(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	bad := "not-a-timestamp"
	_, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              &bad,
		Limit:               nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListUserSessions_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Empty grant set under enterprise enforcement — list must be denied.
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListUserSessions_RefreshTokenHashNotReturned(t *testing.T) {
	t.Parallel()

	// The repo projection that backs ListUserSessions omits refresh_token_hash
	// from the SELECT list, so even if a future view builder regression added
	// the field to types.UserSession, the column would not be available to
	// populate. This test exists to flag that contract — anyone considering
	// widening the projection should treat that as a deliberate decision.

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "no-refresh-token",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("no-leak"))
	require.NoError(t, err)

	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	// Surface contract: jti is OK to return, refresh_token_hash is not
	// representable on the API type at all.
	require.NotEmpty(t, got.Items[0].Jti)
}

func TestListUserSessions_ReturnsEnrichment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "enrichment-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("nobody"))
	require.NoError(t, err)

	got, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, "enrichment-issuer", got.Items[0].IssuerSlug)
	require.Equal(t, "user", got.Items[0].SubjectType)
	require.Nil(t, got.Items[0].SubjectDisplayName) // "nobody" is not a real users.id
	require.Nil(t, got.Items[0].RevokedAt)
}

func TestListUserSessions_StatusRevokedFilter(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "status-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	live, err := seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("live"))
	require.NoError(t, err)
	toRevoke, err := seedUserSession(t, ctx, ti.conn, uuid.MustParse(issuer.ID), urn.NewUserSubject("dead"))
	require.NoError(t, err)

	require.NoError(t, ti.service.RevokeUserSession(ctx, &gen.RevokeUserSessionPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               toRevoke.ID.String(),
	}))

	active := "active"
	gotActive, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              &active,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, gotActive.Items, 1)
	require.Equal(t, live.ID.String(), gotActive.Items[0].ID)

	revoked := "revoked"
	gotRevoked, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              &revoked,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, gotRevoked.Items, 1)
	require.Equal(t, toRevoke.ID.String(), gotRevoked.Items[0].ID)
	require.NotNil(t, gotRevoked.Items[0].RevokedAt)

	all := "all"
	gotAll, err := ti.service.ListUserSessions(ctx, &gen.ListUserSessionsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              &all,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, gotAll.Items, 2)
}
