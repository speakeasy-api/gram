package mcp_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// revokeHistorySession is one live session and the access token naming it.
type revokeHistorySession struct {
	id     uuid.UUID
	issuer uuid.UUID
	jti    string
	token  string
}

func seedRevokeHistorySession(t *testing.T, ctx context.Context, ti *testInstance, issuerID, clientID uuid.UUID, refreshHash pgtype.Text, identity string) revokeHistorySession {
	t.Helper()

	signer := sessiontokens.NewSigner("test-jwt-secret")
	subject := urn.NewUserSubject("revoke-history-" + uuid.NewString())
	token, jti, err := signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: "revoke-history", Issuer: "revoke-history", Lifetime: time.Hour, ExpiresAt: nil, ClientID: "", JTI: ""})
	require.NoError(t, err)
	row, err := usersessions_repo.New(ti.conn).CreateUserSession(ctx, usersessions_repo.CreateUserSessionParams{
		UserSessionIssuerID:    issuerID,
		UserSessionClientID:    uuid.NullUUID{UUID: clientID, Valid: true},
		SubjectUrn:             subject,
		AuthorizerUserID:       pgtype.Text{String: "", Valid: false},
		DelegatedGrants:        nil,
		DelegatedGrantsVersion: pgtype.Int4{Int32: 0, Valid: false},
		Jti:                    jti,
		RefreshTokenHash:       refreshHash,
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: 0, Valid: true},
		RefreshExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), InfinityModifier: 0, Valid: true},
		ToolSelection:          nil,
		Resource:               pgtype.Text{String: "", Valid: false},
		ResourceIdentity:       conv.ToPGTextEmpty(identity),
		CredentialID:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SignInID:               uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SignInUserID:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return revokeHistorySession{id: row.ID, issuer: issuerID, jti: jti, token: token}
}

func revokeAccessToken(t *testing.T, ti *testInstance, slug, clientID, token string) {
	t.Helper()

	form := url.Values{}
	form.Set(oauthwire.ParamToken, token)
	form.Set(oauthwire.ParamTokenTypeHint, "access_token")
	form.Set("client_id", clientID)
	w := postForm(t, ti, slug, "revoke", form)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func sessionDeletedAt(t *testing.T, ctx context.Context, ti *testInstance, session revokeHistorySession) pgtype.Timestamptz {
	t.Helper()

	deletedAt, err := usersessions_repo.New(ti.conn).GetUserSessionDeletedAtFixture(ctx, usersessions_repo.GetUserSessionDeletedAtFixtureParams{ID: session.id, UserSessionIssuerID: session.issuer})
	require.NoError(t, err)
	return deletedAt
}

// Revoking an access token ends a session that cannot refresh, since the token
// was all it had, and records that end where its history is read. A
// refreshable session keeps its grant. Another client's token, a token from
// another issuer, and a repeated revoke change nothing.
func TestHandleRevoke_AccessTokenRecordsAccessOnlyEnd(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	otherToolset, otherIssuer, otherIssuerClient := seedPrivateToolsetWithIssuer(t, ctx, ti)
	foreign, err := usersessions_repo.New(ti.conn).CreateUserSessionClient(ctx, usersessions_repo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                "client_" + uuid.NewString(),
		ClientSecretHash:        pgtype.Text{String: "", Valid: false},
		ClientName:              "another client",
		RedirectUris:            []string{"http://127.0.0.1:51423/callback"},
		ClientSecretExpiresAt:   pgtype.Timestamptz{},
		TokenEndpointAuthMethod: "none",
		ClientJwks:              nil,
		ClientJwksUri:           pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	require.NoError(t, err)
	refreshable := conv.ToPGText(base64.RawURLEncoding.EncodeToString(raw))
	gateway := "meta_mcp_server:" + uuid.NewString()
	accessOnly := pgtype.Text{String: "", Valid: false}

	grant := seedRevokeHistorySession(t, ctx, ti, issuer.ID, client.ID, refreshable, gateway)
	resourceScoped := seedRevokeHistorySession(t, ctx, ti, issuer.ID, client.ID, conv.ToPGText("id-jag:"+uuid.NewString()), gateway)
	unrefreshable := seedRevokeHistorySession(t, ctx, ti, issuer.ID, client.ID, accessOnly, gateway)
	notOurs := seedRevokeHistorySession(t, ctx, ti, issuer.ID, foreign.ID, accessOnly, gateway)
	elsewhere := seedRevokeHistorySession(t, ctx, ti, otherIssuer.ID, otherIssuerClient.ID, accessOnly, gateway)

	slug := toolset.McpSlug.String
	revokeAccessToken(t, ti, slug, client.ClientID, grant.token)
	revokeAccessToken(t, ti, slug, client.ClientID, resourceScoped.token)
	revokeAccessToken(t, ti, slug, client.ClientID, unrefreshable.token)
	revokeAccessToken(t, ti, slug, client.ClientID, notOurs.token)
	revokeAccessToken(t, ti, slug, client.ClientID, elsewhere.token)

	require.False(t, sessionDeletedAt(t, ctx, ti, grant).Valid, "a refreshable grant outlives one revoked access token")
	ended := sessionDeletedAt(t, ctx, ti, resourceScoped)
	require.True(t, ended.Valid, "an access-only session ends with its token")
	require.True(t, sessionDeletedAt(t, ctx, ti, unrefreshable).Valid)
	require.False(t, sessionDeletedAt(t, ctx, ti, notOurs).Valid, "another client's token is not revoked")
	require.False(t, sessionDeletedAt(t, ctx, ti, elsewhere).Valid, "a token of another issuer is not revoked here")
	// The revocation cache is unchanged: every owned token is revoked, the
	// refreshable grant's included, and nobody else's is.
	cached := func(session revokeHistorySession) bool {
		t.Helper()
		revoked, err := ti.chatSessionsManager.IsTokenRevoked(ctx, session.jti)
		require.NoError(t, err)
		return revoked
	}
	require.Equal(t, []bool{true, true, true, false, false},
		[]bool{cached(grant), cached(resourceScoped), cached(unrefreshable), cached(notOurs), cached(elsewhere)})

	// Revoking again keeps the recorded end.
	revokeAccessToken(t, ti, slug, client.ClientID, resourceScoped.token)
	require.Equal(t, ended.Time, sessionDeletedAt(t, ctx, ti, resourceScoped).Time)
	// The other issuer's own client still ends its own session.
	revokeAccessToken(t, ti, otherToolset.McpSlug.String, otherIssuerClient.ClientID, elsewhere.token)
	require.True(t, sessionDeletedAt(t, ctx, ti, elsewhere).Valid)
	require.True(t, cached(elsewhere))

	// The recorded ends are what the gateway history reads as revocations:
	// the two access-only sessions this client revoked, and the other
	// issuer's, while the refreshable grant and another client's session
	// stay usable.
	events, err := usersessions_repo.New(ti.conn).GetGatewaySessionTrendEvents(ctx, usersessions_repo.GetGatewaySessionTrendEventsParams{
		ResourceIdentity: gateway,
		OrganizationID:   toolset.OrganizationID,
		ProjectID:        toolset.ProjectID,
		ToTime:           pgtype.Timestamptz{Time: time.Now().Add(time.Minute), InfinityModifier: 0, Valid: true},
		FromTime:         pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: 0, Valid: true},
		Stride:           pgtype.Interval{Microseconds: (24 * time.Hour).Microseconds(), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	var revoked, ended3 int64
	for _, event := range events {
		if event.Kind == "end" {
			ended3 += event.Total
			revoked += event.Flagged
		}
	}
	require.EqualValues(t, 3, revoked)
	require.EqualValues(t, 3, ended3)
}
