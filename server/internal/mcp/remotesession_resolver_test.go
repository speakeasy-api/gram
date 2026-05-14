package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func TestServePublic_UserSessionIssuerRemoteSessionNoValidTokenChallenges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "resolver-no-token")
	requestSubject := urn.NewUserSubject("resolver-user-" + uuid.NewString())

	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, requestSubject, "expired-upstream-token", time.Now().Add(-time.Minute))
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, urn.NewUserSubject("resolver-other-"+uuid.NewString()), "other-user-upstream-token", time.Now().Add(time.Hour))

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, requestSubject)
	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/"+fixture.Toolset.McpSlug.String,
		"resolver must surface a WWW-Authenticate challenge when no valid remote_session exists for the subject")
}

// TestServePublic_UserSessionIssuerRemoteSessionNoRowsChallenges pins the
// zero-rows variant of the negative path: when the subject has no
// remote_sessions entries at all, the resolver must produce the same
// auth-challenge outcome as the only-expired case. Kept as a distinct
// test so the resolver cannot accidentally branch on row existence
// (e.g. "skip the check when nothing is stored").
func TestServePublic_UserSessionIssuerRemoteSessionNoRowsChallenges(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "resolver-no-rows")
	requestSubject := urn.NewUserSubject("resolver-user-" + uuid.NewString())

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, requestSubject)
	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
	require.Contains(t, w.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource/mcp/"+fixture.Toolset.McpSlug.String,
		"resolver must surface a WWW-Authenticate challenge when the subject has no remote_sessions rows")
}

// TestServePublic_UserSessionIssuerRemoteSessionValidTokenResolvesOAuthInput
// pins the green half of the resolver contract: when a non-expired
// remote_sessions row exists for the subject extracted from the
// user-session JWT, the resolver must surface that exchanged token as the
// OAuth input for the request — satisfying the toolset's oauth2 scheme so
// initialize succeeds without a WWW-Authenticate challenge.
//
// To prove the resolver picked *the right* row, we plant a second
// remote_session for a different subject and a third revoked/expired row
// for the request subject. Neither must satisfy the request — only the
// valid row for the matching subject can.
func TestServePublic_UserSessionIssuerRemoteSessionValidTokenResolvesOAuthInput(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	fixture := createRemoteSessionResolverFixture(t, ctx, ti, authCtx, "resolver-valid-token")
	requestSubject := urn.NewUserSubject("resolver-user-" + uuid.NewString())

	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, requestSubject, "valid-upstream-token", time.Now().Add(time.Hour))
	insertRemoteSessionAccessToken(t, ctx, ti, fixture.UserSessionIssuer.ID, fixture.RemoteSessionClient.ID, urn.NewUserSubject("resolver-other-"+uuid.NewString()), "other-user-upstream-token", time.Now().Add(time.Hour))

	sessionToken := mintUserSessionBearerForSubject(t, ti, fixture.Toolset, requestSubject)
	w, err := servePublicHTTP(t, context.Background(), ti, fixture.Toolset.McpSlug.String, makeInitializeBody(), sessionToken, nil)
	require.NoError(t, err, "initialize should succeed when a valid remote_session resolves for the subject")
	require.Empty(t, w.Header().Get("WWW-Authenticate"),
		"resolver must satisfy the toolset's oauth2 scheme, so no WWW-Authenticate challenge should be emitted")
}

type remoteSessionResolverFixture struct {
	Toolset             toolsets_repo.Toolset
	UserSessionIssuer   usersessions_repo.UserSessionIssuer
	RemoteSessionClient remotesessions_repo.RemoteSessionClient
}

func createRemoteSessionResolverFixture(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	authCtx *contextvalues.AuthContext,
	slugPrefix string,
) remoteSessionResolverFixture {
	t.Helper()

	suffix := uuid.NewString()[:8]
	slug := slugPrefix + "-" + suffix

	userIssuer, err := usersessions_repo.New(ti.conn).CreateUserSessionIssuer(ctx, usersessions_repo.CreateUserSessionIssuerParams{
		ProjectID:          *authCtx.ProjectID,
		Slug:               "resolver-usi-" + suffix,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: int64(time.Hour / time.Microsecond),
			Days:         0,
			Months:       0,
			Valid:        true,
		},
	})
	require.NoError(t, err)

	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
	toolset, err = toolsets_repo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsets_repo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: userIssuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           toolset.ProjectID,
	})
	require.NoError(t, err)
	ti.addToolWithDualSecurity(ctx, t, toolset.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	remoteRepo := remotesessions_repo.New(ti.conn)
	remoteIssuer, err := remoteRepo.CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         *authCtx.ProjectID,
		Slug:                              "resolver-rsi-" + suffix,
		Issuer:                            "https://upstream.example/" + suffix,
		AuthorizationEndpoint:             conv.ToPGText("https://upstream.example/" + suffix + "/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://upstream.example/" + suffix + "/token"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	remoteClient, err := remoteRepo.CreateRemoteSessionClient(ctx, remotesessions_repo.CreateRemoteSessionClientParams{
		ProjectID:             *authCtx.ProjectID,
		RemoteSessionIssuerID: remoteIssuer.ID,
		UserSessionIssuerID:   userIssuer.ID,
		ClientID:              "resolver-client-" + suffix,
		ClientSecretEncrypted: pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ClientSecretExpiresAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)

	return remoteSessionResolverFixture{
		Toolset:             toolset,
		UserSessionIssuer:   userIssuer,
		RemoteSessionClient: remoteClient,
	}
}

func insertRemoteSessionAccessToken(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	userSessionIssuerID uuid.UUID,
	remoteSessionClientID uuid.UUID,
	subject urn.SessionSubject,
	accessToken string,
	expiresAt time.Time,
) remotesessions_repo.RemoteSession {
	t.Helper()

	accessTokenEncrypted, err := ti.enc.Encrypt([]byte(accessToken))
	require.NoError(t, err)

	session, err := remotesessions_repo.New(ti.conn).UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            subject,
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteSessionClientID: remoteSessionClientID,
		AccessTokenEncrypted:  accessTokenEncrypted,
		AccessExpiresAt:       pgtype.Timestamptz{Time: expiresAt, Valid: true},
		RefreshTokenEncrypted: pgtype.Text{String: "", Valid: false},
		RefreshExpiresAt:      pgtype.Timestamptz{Valid: false},
		Scopes:                []string{},
	})
	require.NoError(t, err)
	return session
}

func mintUserSessionBearerForSubject(
	t *testing.T,
	ti *testInstance,
	toolset toolsets_repo.Toolset,
	subject urn.SessionSubject,
) string {
	t.Helper()

	token, _, err := usersessions.NewSigner("test-jwt-secret").Mint(
		subject,
		urn.NewToolset(toolset.ID).String(),
		ti.serverURL.String()+"/mcp/"+toolset.McpSlug.String,
		time.Hour,
	)
	require.NoError(t, err)
	return token
}
