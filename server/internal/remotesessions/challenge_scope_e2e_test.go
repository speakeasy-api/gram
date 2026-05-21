// challenge_scope_e2e_test.go drives ListClients → BuildAuthorizationUrl
// against a real ChallengeManager + database to assert that the upstream
// `scope` query parameter is sourced from the remote_session_client's
// stored scope when present and otherwise falls back to the remote
// issuer's scopes_supported.

package remotesessions_test

import (
	"net/url"
	"strings"
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

func TestBuildAuthorizationUrl_ScopeResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		clientScope   []string
		issuerScopes  []string
		expectedScope string
	}{
		{
			name:          "client scope override wins over issuer scopes_supported",
			clientScope:   []string{"read:tools", "write:tools"},
			issuerScopes:  []string{"openid", "profile", "email"},
			expectedScope: "read:tools write:tools",
		},
		{
			name:          "absent client scope falls back to issuer scopes_supported",
			clientScope:   nil,
			issuerScopes:  []string{"openid", "profile"},
			expectedScope: "openid profile",
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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
			slugSuffix := strings.ReplaceAll(tc.name, " ", "-")
			issuer, err := q.CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
				ProjectID:                         *authCtx.ProjectID,
				Slug:                              "auth-scope-" + slugSuffix,
				Issuer:                            "https://idp.example.com",
				AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
				TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
				RegistrationEndpoint:              pgtype.Text{String: "", Valid: false},
				JwksUri:                           pgtype.Text{String: "", Valid: false},
				ScopesSupported:                   tc.issuerScopes,
				GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
				ResponseTypesSupported:            []string{"code"},
				TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
				Oidc:                              false,
				Passthrough:                       false,
			})
			require.NoError(t, err)

			userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-scope-"+slugSuffix)

			client, err := q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
				ProjectID:               *authCtx.ProjectID,
				RemoteSessionIssuerID:   issuer.ID,
				UserSessionIssuerID:     userIssuer,
				ClientID:                "scope-cid",
				ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
				ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
				Scope:                   tc.clientScope,
			})
			require.NoError(t, err)
			_ = client

			clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, userIssuer)
			require.NoError(t, err)
			require.Len(t, clients, 1)

			subject := urn.NewUserSubject("scope-subject")
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
			require.Equal(t, tc.expectedScope, parsed.Query().Get("scope"), "case %d", i)
		})
	}
}
