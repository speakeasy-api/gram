package mcp_test

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// Revocation soft-deletes the session row before it pushes the jti to the
// Redis denylist, and that entry can be evicted or never written. The first
// request caches the session's policy in Redis; the row is then revoked with
// no denylist entry, and the next request must still be refused.
func TestServePublic_RevokedUserSessionWithoutDenylistEntryIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)

	var capturedAuth atomic.Value
	upstream := newAuthorizationRecorder(t, &capturedAuth)
	fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, "revoked-row", upstream.URL)
	slug := fixture.Toolset.McpSlug.String
	issuerID := fixture.Toolset.UserSessionIssuerID.UUID

	subject := urn.NewUserSubject("revoked-row-user-" + uuid.NewString())
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
	token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
		Subject:  subject,
		Audience: urn.NewUserSessionIssuer(issuerID).String(),
		Issuer:   ti.serverURL.String() + "/mcp/" + slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, ti, issuerID, subject, jti)

	resp := serveMCPResponse(t, ti, slug, makeInitializeBody(), token)
	require.Equal(t, http.StatusOK, resp.Code, "the session must be admitted first, or the refusal that follows proves nothing: body=%s", resp.Body.String())

	queries := usersessionsrepo.New(ti.conn)
	session, err := queries.GetUserSessionByJTI(ctx, usersessionsrepo.GetUserSessionByJTIParams{UserSessionIssuerID: issuerID, Jti: jti})
	require.NoError(t, err)
	_, err = queries.RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{
		ID:             session.ID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	revoked, err := ti.chatSessionsManager.IsTokenRevoked(ctx, jti)
	require.NoError(t, err)
	require.False(t, revoked, "the denylist must not know the jti, or this test exercises the denylist instead of the row")

	refused := serveMCPResponse(t, ti, slug, makeInitializeBody(), token)
	require.Equal(t, http.StatusUnauthorized, refused.Code, "body=%s", refused.Body.String())
	require.Equal(t,
		`Bearer resource_metadata="`+ti.serverURL.String()+`/.well-known/oauth-protected-resource/mcp/`+slug+`"`,
		refused.Header().Get("WWW-Authenticate"),
		"a user session is answered with the same challenge as any other rejected bearer",
	)
}

func TestServePublic_ExpiredUserSessionIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx := requireProjectAuthContext(t, ctx)
	now := time.Now()

	tests := []struct {
		name              string
		createdAt         time.Time
		lastUsedAt        time.Time
		inactivityTimeout time.Duration
		absoluteExpiresAt time.Time
	}{
		{name: "inactive", createdAt: now.Add(-2 * time.Hour), lastUsedAt: now.Add(-31 * time.Minute), inactivityTimeout: 30 * time.Minute, absoluteExpiresAt: now.Add(time.Hour)},
		{name: "maximum duration elapsed", createdAt: now.Add(-2 * time.Hour), lastUsedAt: now.Add(-time.Minute), inactivityTimeout: 30 * time.Minute, absoluteExpiresAt: now.Add(-time.Second)},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var capturedAuth atomic.Value
			upstream := newAuthorizationRecorder(t, &capturedAuth)
			fixture := createIssuerGatedHTTPToolFixture(t, ctx, ti, authCtx, fmt.Sprintf("expired-%d", i), upstream.URL)
			slug := fixture.Toolset.McpSlug.String
			issuerID := fixture.Toolset.UserSessionIssuerID.UUID
			subject := urn.NewUserSubject("expired-row-user-" + uuid.NewString())

			token, jti, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{
				Subject: subject, Audience: urn.NewUserSessionIssuer(issuerID).String(),
				Issuer: ti.serverURL.String() + "/mcp/" + slug, Lifetime: time.Hour,
			})
			require.NoError(t, err)
			persistTestUserSession(t, ti, issuerID, subject, jti)
			insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, subject, "valid-upstream-token", time.Now().Add(time.Hour))
			control := serveMCPResponse(t, ti, slug, makeInitializeBody(), token)
			require.Equal(t, http.StatusOK, control.Code, "unexpired control body=%s", control.Body.String())

			err = usersessionsrepo.New(ti.conn).SetUserSessionLifetimeFixture(ctx, usersessionsrepo.SetUserSessionLifetimeFixtureParams{
				CreatedAt:                pgtype.Timestamptz{Time: tt.createdAt, Valid: true},
				LastUsedAt:               pgtype.Timestamptz{Time: tt.lastUsedAt, Valid: true},
				SessionInactivityTimeout: pgtype.Interval{Microseconds: tt.inactivityTimeout.Microseconds(), Valid: true},
				SessionAbsoluteExpiresAt: pgtype.Timestamptz{Time: tt.absoluteExpiresAt, Valid: true},
				Jti:                      jti,
				ProjectID:                uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			})
			require.NoError(t, err)

			resp := serveMCPResponse(t, ti, slug, makeInitializeBody(), token)
			require.Equal(t, http.StatusUnauthorized, resp.Code, "body=%s", resp.Body.String())
		})
	}
}
