package assistants

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/executionprofiles"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

type mcpAuthNotRevoked struct{}

func (mcpAuthNotRevoked) IsTokenRevoked(context.Context, string) (bool, error) { return false, nil }

func TestMCPAuthCallbackAdoptsSelectionBeforeResume(t *testing.T) {
	for _, tc := range []struct {
		name                                                            string
		badBearer, resourceAudience, wrongAudience, failSignal, gateway bool
	}{
		{name: "published alias issuer audience"},
		{name: "published alias exact resource audience", resourceAudience: true},
		{name: "forged session", badBearer: true},
		{name: "wrong audience", wrongAudience: true},
		{name: "signal failure then retry", failSignal: true},
		{name: "gateway preserves legacy continuation", gateway: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := assistantsInfra.CloneTestDatabase(t, "oauth_selection")
			require.NoError(t, err)
			q := executionprofiles.New(db)
			org, project, server, remote := "org_oauth_fixture", uuid.New(), uuid.New(), uuid.New()
			require.NoError(t, q.SeedOrganizationFixture(ctx, org))
			require.NoError(t, q.SeedProjectFixture(ctx, executionprofiles.SeedProjectFixtureParams{ID: project, OrganizationID: org, Slug: "oauth-fixture"}))
			assistant, err := q.SeedAssistantFixture(ctx, executionprofiles.SeedAssistantFixtureParams{ProjectID: project, OrganizationID: org, Name: "OAuth fixture"})
			require.NoError(t, err)
			trigger, err := q.SeedTriggerFixture(ctx, executionprofiles.SeedTriggerFixtureParams{ProjectID: project, OrganizationID: org, Name: "OAuth trigger"})
			require.NoError(t, err)
			require.NoError(t, q.SeedRemoteMCPServerFixture(ctx, executionprofiles.SeedRemoteMCPServerFixtureParams{ID: remote, ProjectID: project, Url: "https://upstream.example.com"}))
			require.NoError(t, q.SeedMCPServerFixture(ctx, executionprofiles.SeedMCPServerFixtureParams{ID: server, ProjectID: project, Slug: pgtype.Text{String: "internal-fixture", Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: remote, Valid: true}, RequiresUpstreamAuth: pgtype.Bool{Bool: true, Valid: true}}))
			_, err = mcpendpointsrepo.New(db).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: project, McpServerID: uuid.NullUUID{UUID: server, Valid: true}, Slug: "published-fixture"})
			require.NoError(t, err)
			require.NoError(t, q.SeedAssistantServerFixture(ctx, executionprofiles.SeedAssistantServerFixtureParams{ProjectID: project, AssistantID: assistant, McpServerID: server}))
			selected := uuid.Nil
			for range 2 {
				selected, err = q.SeedAuthClientFixture(ctx, executionprofiles.SeedAuthClientFixtureParams{ProjectID: uuid.NullUUID{UUID: project, Valid: true}, OrganizationID: org, ClientType: "static_credential"})
				require.NoError(t, err)
				require.NoError(t, q.LinkAuthClientFixture(ctx, executionprofiles.LinkAuthClientFixtureParams{ProjectID: project, OrganizationID: org, McpServerID: server, AuthClientID: selected}))
			}
			target := executionprofiles.Target{OrganizationID: org, ProjectID: project, AssistantID: assistant, TriggerID: trigger, Context: executionprofiles.ContextUserBacked}
			old, err := executionprofiles.Bind(ctx, db, target, executionprofiles.SourceIssuance)
			require.NoError(t, err)
			require.Len(t, old.Gaps, 1)
			signer := sessiontokens.NewSigner("oauth-test-secret")
			mintSigner := signer
			if tc.badBearer {
				mintSigner = sessiontokens.NewSigner("untrusted-signing-secret")
			}
			raw, jti, err := mintSigner.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("oauth-user"), Issuer: "https://auth.example.com", JTI: base64.RawURLEncoding.EncodeToString([]byte(uuid.NewString())), Audience: urn.NewMcpServer(server).String(), ClientID: "oauth-fixture", Lifetime: time.Hour})
			require.NoError(t, err)
			seeded, err := q.SeedOAuthResumeFixture(ctx, executionprofiles.SeedOAuthResumeFixtureParams{ProjectID: project, OrganizationID: org, AssistantID: assistant, McpServerID: server, AuthClientID: selected, Jti: jti})
			require.NoError(t, err)
			mcpURL := "https://mcp.example.com/mcp/published-fixture"
			if tc.gateway {
				gateway, err := metamcprepo.New(db).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{OrganizationID: org, ProjectID: project, Name: "OAuth gateway fixture", UserSessionIssuerID: uuid.NullUUID{UUID: seeded.IssuerID, Valid: true}, Visibility: "private"})
				require.NoError(t, err)
				_, err = mcpendpointsrepo.New(db).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{ProjectID: project, MetaMcpServerID: uuid.NullUUID{UUID: gateway.ID, Valid: true}, Slug: "gateway-fixture"})
				require.NoError(t, err)
				mcpURL = "https://mcp.example.com/mcp/gateway-fixture"
			}
			audience := urn.NewUserSessionIssuer(seeded.IssuerID).String()
			if tc.resourceAudience {
				audience = mcpURL
			}
			if tc.wrongAudience {
				audience = "https://mcp.example.com/mcp/another-resource"
			}
			// Match the actual per-server serving audience, and keep its JTI bound
			// to the durable OAuth session seeded above.
			raw, _, err = mintSigner.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject("oauth-user"), Issuer: "https://auth.example.com", Audience: audience, ClientID: "oauth-fixture", Lifetime: time.Hour, JTI: jti})
			require.NoError(t, err)
			service := newMCPAuthTestService(t, db)
			service.core.SetMCPAuthSessionVerifier(signer, mcpAuthNotRevoked{})
			service.core.assistantTokens = assistanttokens.New("state-secret", db, nil, nil, nil)
			signaler := &mcpAuthRetrySignaler{failFirst: tc.failSignal}
			service.signaler = signaler
			verifier, err := service.core.encryptionClient.Encrypt([]byte("verifier"))
			require.NoError(t, err)
			var exchanges atomic.Int32
			tokenEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if exchanges.Add(1) != 1 {
					http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"access_token": raw, "token_type": "Bearer"})
			}))
			t.Cleanup(tokenEndpoint.Close)
			state, err := service.core.assistantTokens.GenerateMCPAuthFlow(assistanttokens.MCPAuthFlowInput{SelectionProfileVersionID: old.VersionID.String(), OrgID: org, ProjectID: project, AssistantID: assistant, ThreadID: seeded.ThreadID, UserID: "oauth-user", FlowID: assistant.String(), AttemptID: uuid.NewString(), ServerID: uuid.NewString(), McpURL: mcpURL, ClientID: "oauth-fixture", CodeVerifier: verifier, TokenEndpoint: tokenEndpoint.URL, OAuthServerIssuer: "https://auth.example.com", TTL: time.Minute})
			require.NoError(t, err)
			mux := goahttp.NewMuxer()
			Attach(mux, service)
			path := "/rpc/assistantMcpAuth/" + assistant.String() + "/oauth/callback?state=" + url.QueryEscape(state) + "&code=code"
			// Two callbacks for one state must exchange once and enqueue only the
			// winning result, never a losing invalid_grant ahead of a success.
			responses := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
			if tc.failSignal {
				mux.ServeHTTP(responses[0], httptest.NewRequest(http.MethodGet, path, nil))
				require.GreaterOrEqual(t, responses[0].Code, 500)
				mux.ServeHTTP(responses[1], httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, responses[1].Code)
				require.Equal(t, int32(2), signaler.calls.Load())
			} else {
				var wg sync.WaitGroup
				for _, response := range responses {
					wg.Go(func() { mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil)) })
				}
				wg.Wait()
			}

			require.Equal(t, int32(1), exchanges.Load())
			event, err := assistantrepo.New(db).GetLatestAssistantThreadEventByThreadID(ctx, assistantrepo.GetLatestAssistantThreadEventByThreadIDParams{ProjectID: project, AssistantThreadID: seeded.ThreadID})
			require.NoError(t, err)
			var payload mcpAuthEventPayload
			require.NoError(t, json.Unmarshal(event.NormalizedPayloadJson, &payload))
			next, err := executionprofiles.Bind(ctx, db, target, executionprofiles.SourceIssuance)
			require.NoError(t, err)
			if tc.badBearer || tc.wrongAudience {
				require.Equal(t, mcpAuthStatusFailed, payload.Status)
				require.Equal(t, old.VersionID, next.VersionID)
			} else if tc.gateway {
				require.Equal(t, mcpAuthStatusSuccess, payload.Status)
				require.Equal(t, old.VersionID, next.VersionID, "gateway success must not claim a direct selection repair")
				require.False(t, event.TriggerInstanceID.Valid)
			} else {
				require.Equal(t, mcpAuthStatusSuccess, payload.Status)
				require.Equal(t, trigger, event.TriggerInstanceID.UUID, "resume must retain its originating trigger")
				require.Empty(t, next.Gaps)
				choices, err := q.ListVersionChoicesFixture(ctx, next.VersionID)
				require.NoError(t, err)
				require.Len(t, choices, 1)
				require.Equal(t, selected, choices[0].AuthClientID.UUID)
			}
			oldChoices, err := q.ListVersionChoicesFixture(ctx, old.VersionID)
			require.NoError(t, err)
			require.Empty(t, oldChoices)
		})
	}
}

type mcpAuthRetrySignaler struct {
	stubWorkflowSignaler
	calls     atomic.Int32
	failFirst bool
}

func (s *mcpAuthRetrySignaler) SignalCoordinator(context.Context, uuid.UUID) error {
	if s.calls.Add(1) == 1 && s.failFirst {
		return errors.New("coordinator temporarily unavailable")
	}
	return nil
}
