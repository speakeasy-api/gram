package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestDirectUpstreamChoiceGitHubAppRejectsLegacyFallback(t *testing.T) {
	t.Parallel()

	pool, err := TestInfra.CloneTestDatabase(t, "mcp_github_direct_fallback")
	require.NoError(t, err)

	for _, mapped := range []bool{false, true} {
		name := "unmapped OAuth choice"
		if mapped {
			name = "mapped OAuth choice with stale headers"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
			f.service.metrics = mcpmetrics.NewMetrics(provider.Meter("test"), f.service.logger)
			ctx := t.Context()
			f.service.features = new(feature.InMemory)
			f.service.authClientPermitter = &recordingPermitter{asked: nil}
			f.attach()

			// Keep a usable historical OAuth attachment alongside the App.
			// Neither a legacy selection nor stale typed header copies may
			// reactivate that attachment for an App-required server.
			appID := f.client(authClientTypeServiceAccount, uuid.NullUUID{}, true)
			require.NoError(t, authclientsrepo.New(pool).SetGitHubAppCredential(ctx, authclientsrepo.SetGitHubAppCredentialParams{
				AuthClientID: appID, OrganizationID: f.orgID,
				InstanceUrl: "https://github.example.com", AppID: 1, InstallationID: 2,
				PrivateKeyEncrypted: "unused-test-key", UpstreamOrigin: "https://mcp.example.com",
			}))
			required, err := f.service.serverRequiresGitHubApp(ctx, f.orgID, f.serverID)
			require.NoError(t, err)
			require.True(t, required)

			issuerID, credentialID := uuid.New(), uuid.New()
			require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{
				ID: issuerID, ProjectID: validUUID(f.projectID), Slug: "usi-" + issuerID.String()[:8],
			}))
			require.NoError(t, f.q.BindClientIssuerFixture(ctx, authclientbackfill.BindClientIssuerFixtureParams{
				RemoteSessionClientID: f.registration, UserSessionIssuerID: issuerID,
			}))
			require.NoError(t, f.q.SeedOAuthSelectionFixture(ctx, authclientbackfill.SeedOAuthSelectionFixtureParams{
				ProjectID: f.projectID, OrganizationID: f.orgID, CredentialID: credentialID,
				McpServerID: f.serverID, RemoteSessionClientID: f.registration,
			}))
			if mapped {
				clientID := f.client(authClientTypeRemoteSession, validUUID(f.registration), true)
				f.sourceHeader("X-Version", false, text("current"), pgtype.Text{})
				f.copyHeader(clientID, "X-Version", false, false, text("stale"), pgtype.Text{})
				require.NoError(t, f.q.SwapUpstreamAuthClientFixture(ctx, authclientbackfill.SwapUpstreamAuthClientFixtureParams{
					AuthClientID: validUUID(clientID), CredentialID: credentialID, McpServerID: f.serverID,
				}))
			}

			subject := urn.NewUserSubject("user_direct_fallback")
			proof := validatedSessionProof(t, subject)
			require.NoError(t, f.q.SeedUserSessionFixture(ctx, authclientbackfill.SeedUserSessionFixtureParams{
				ProjectID: validUUID(f.projectID), OrganizationID: text(f.orgID), UserSessionIssuerID: issuerID,
				SubjectUrn: subject, Jti: proof.JTI(), CredentialID: validUUID(credentialID),
			}))
			ctx = mcpidentity.NewValidatorBoundary().StampValidatedSession(ctx, proof)
			ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: subject.ID, ActiveOrganizationID: f.orgID})
			ctx = context.WithValue(ctx, liveSessionPolicyKey{}, true)
			authentication := &issuerGateAuthentication{
				endpoint: &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: issuerID, McpServerID: validUUID(f.serverID)},
				subject:  subject, refreshableUserSession: true,
				protectedResourceURL: "https://gateway.example.com/.well-known/oauth-protected-resource/mcp/test",
			}
			selector, err := f.service.upstreamSelectorFor(ctx, authentication.endpoint)
			require.NoError(t, err)
			require.NotNil(t, selector)
			legacy, err := selector.forServer(ctx, f.serverID)
			require.NoError(t, err)
			require.Equal(t, validUUID(f.registration), legacy.clientID, "the legacy path really can serve an OAuth credential")
			res, err := f.service.resolveEnforcedAuthClient(ctx, selector, f.serverID, f.projectID, subject)
			require.NoError(t, err)
			if mapped {
				require.Equal(t, authClientLegacy, res.outcome, "stale mapped headers hand back to the legacy selector")
				require.Equal(t, authClientReasonHeadersStale, res.reason)
			} else {
				require.Equal(t, authClientGap, res.outcome, "enforcement already upgrades an unmapped choice to a gap")
			}

			w := httptest.NewRecorder()
			choice, handled, err := f.service.directUpstreamChoice(ctx, w, authentication)
			require.True(t, handled)
			require.ErrorContains(t, err, remoteSessionReconnectDescription)
			require.Contains(t, w.Header().Get("WWW-Authenticate"), `error="invalid_token"`)
			require.False(t, choice.clientID.Valid, "an App-required server must not return the legacy OAuth credential")
			require.False(t, authentication.chosenClient.Valid)
			if mapped {
				var collected metricdata.ResourceMetrics
				require.NoError(t, reader.Collect(ctx, &collected))
				var rejections int64
				for _, scope := range collected.ScopeMetrics {
					for _, metric := range scope.Metrics {
						if metric.Name != mcpmetrics.InstrumentMCPRequestRejected {
							continue
						}
						sum, ok := metric.Data.(metricdata.Sum[int64])
						require.True(t, ok)
						for _, point := range sum.DataPoints {
							reason, ok := point.Attributes.Value(attr.OAuthFailureReasonKey)
							require.True(t, ok)
							require.Equal(t, issuerGateReasonUpstreamSelectionUnavailable, reason.AsString())
							rejections += point.Value
						}
					}
				}
				require.EqualValues(t, 1, rejections, "App legacy handback records exactly one issuer-gate rejection")
			}
		})
	}
}
