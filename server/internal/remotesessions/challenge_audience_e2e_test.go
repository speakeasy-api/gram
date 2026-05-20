// challenge_audience_e2e_test.go drives ListClients → BuildAuthorizationUrl
// against a real ChallengeManager + database to assert that the upstream
// `audience` query parameter is attached to the authorize redirect when the
// remote_session_client has audience configured and omitted otherwise.

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

func TestBuildAuthorizationUrl_AudienceResolution(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		audience         pgtype.Text
		expectedAudience string
		expectAudParam   bool
	}{
		{
			name:             "configured audience is attached to authorize URL",
			audience:         conv.ToPGText("https://api.example.com"),
			expectedAudience: "https://api.example.com",
			expectAudParam:   true,
		},
		{
			name:             "missing audience omits the parameter",
			audience:         pgtype.Text{String: "", Valid: false},
			expectedAudience: "",
			expectAudParam:   false,
		},
	}

	for _, tc := range cases {
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
				Slug:                              "auth-aud-" + slugSuffix,
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

			userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "usi-aud-"+slugSuffix)

			_, err = q.CreateRemoteSessionClient(ctx, repo.CreateRemoteSessionClientParams{
				ProjectID:               *authCtx.ProjectID,
				RemoteSessionIssuerID:   issuer.ID,
				UserSessionIssuerID:     userIssuer,
				ClientID:                "aud-cid",
				ClientSecretEncrypted:   pgtype.Text{String: "", Valid: false},
				ClientIDIssuedAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				ClientSecretExpiresAt:   pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
				TokenEndpointAuthMethod: pgtype.Text{String: "", Valid: false},
				Scope:                   nil,
				Audience:                tc.audience,
			})
			require.NoError(t, err)

			clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, userIssuer)
			require.NoError(t, err)
			require.Len(t, clients, 1)

			subject := urn.NewUserSubject("aud-subject")
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
			if tc.expectAudParam {
				require.Equal(t, tc.expectedAudience, parsed.Query().Get("audience"))
			} else {
				require.False(t, parsed.Query().Has("audience"), "audience param should be absent when client.audience is unset")
			}
		})
	}
}
