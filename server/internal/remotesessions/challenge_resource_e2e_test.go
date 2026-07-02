// challenge_resource_e2e_test.go drives ListClients → BuildAuthorizationUrl
// against a real ChallengeManager + database to assert that the RFC 8707
// `resource` query parameter is attached to the authorize redirect when the
// remote_session_client has resource configured and omitted otherwise.

package remotesessions_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// buildAuthorizeURLWithResource seeds an issuer + client with the given
// resource value and returns the parsed authorize redirect URL.
func buildAuthorizeURLWithResource(t *testing.T, resource pgtype.Text, slugSuffix string) *url.URL {
	t.Helper()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	enc := testenv.NewEncryptionClient(t)
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	mgr := remotesessions.NewChallengeManager(
		logger,
		ti.conn,
		enc,
		policy,
		cache.NoopCache,
		mustURL(t, "http://localhost"),
	)

	q := repo.New(ti.conn)
	issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Slug:                              "auth-res-" + slugSuffix,
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		Oidc:                              false,
		Passthrough:                       false,
	})
	require.NoError(t, err)

	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-res-"+slugSuffix)

	client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
		ProjectID:               conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:          conv.ToPGTextEmpty(authCtx.ActiveOrganizationID),
		RemoteSessionIssuerID:   issuer.ID,
		ClientID:                "res-cid",
		ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
		ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
		Scope:                   nil,
		Audience:                pgtype.Text{String: "", Valid: false},
		Resource:                resource,
	})
	require.NoError(t, err)

	err = q.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   userIssuer,
	})
	require.NoError(t, err)

	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)

	subject := urn.NewUserSubject("res-subject")
	authURL, err := mgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  uuid.NewString(),
		ProjectID:           *authCtx.ProjectID,
		UserSessionIssuerID: userIssuer,
		Subject:             &subject,
		McpSlug:             "",
		FinalRedirectURI:    "",
	}, clients[0])
	require.NoError(t, err)

	parsed, err := url.Parse(authURL)
	require.NoError(t, err)
	return parsed
}

func TestBuildAuthorizationUrl_ResourceConfigured(t *testing.T) {
	t.Parallel()

	parsed := buildAuthorizeURLWithResource(t, conv.ToPGText("https://mcp.example.com/mcp"), "set")
	require.Equal(t, "https://mcp.example.com/mcp", parsed.Query().Get("resource"), "authorize URL must carry the RFC 8707 resource indicator when client.resource is set")
}

func TestBuildAuthorizationUrl_ResourceOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	parsed := buildAuthorizeURLWithResource(t, pgtype.Text{String: "", Valid: false}, "unset")
	require.False(t, parsed.Query().Has("resource"), "resource param should be absent when client.resource is unset")
}
