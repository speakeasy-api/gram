package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	ateapiv1 "go.ate.dev/substrate/api/ateapi/v1"
	credproviderv1 "go.ate.dev/substrate/api/credprovider/v1"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/atecred"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errFakeATEEgressDenied = errors.New("ATE egress proxy denied request")

type fakeATEEgressIDTokenValidator struct {
	expectedToken string
	email         string
}

func (v fakeATEEgressIDTokenValidator) Validate(_ context.Context, token, audience string) (*idtoken.Payload, error) {
	if token != v.expectedToken {
		return nil, errors.New("invalid google id token")
	}
	return &idtoken.Payload{
		Issuer:   "https://accounts.google.com",
		Audience: audience,
		Claims: map[string]any{
			"email":          v.email,
			"email_verified": true,
		},
	}, nil
}

// fakeATEEgressProxy implements a policy-enforcing egress proxy over an ATE
// EgressPolicy rule list using ATE's custom CredentialProvider contract:
//   - refuses IP literals (mirroring ATE's hostname-only DNS relay and nftables
//     blocking of link-local/metadata/private IP destinations);
//   - applies ATE's most-specific-hostname-wins precedence across concrete
//     HTTPS rules and wildcard passthrough/HTTP rules;
//   - replaces Authorization by calling FetchSecret(credential_uri, actor_spiffe_id)
//     strictly on matched concrete HTTPS rules with replace_headers;
//   - dispatches matched requests through to the target backend handler.
type fakeATEEgressProxy struct {
	rules         []*ateapiv1.EgressRule
	credProvider  credproviderv1.CredentialProviderServer
	actorSPIFFEID string
	gateway       organizationGatewayFixture
	ti            *testInstance
	chatService   *chat.Service
	publicServer  *httptest.Server
}

type fakeATEEgressDecision struct {
	allowed        bool
	passthrough    bool
	injectedHeader string
	injectedURI    string
}

func (p *fakeATEEgressProxy) evaluate(t *testing.T, ctx context.Context, rawURL string, headers http.Header) (fakeATEEgressDecision, error) {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	host := strings.ToLower(u.Hostname())
	// ATE hostname rules never match IP literals, and ateworker nftables +
	// DNS relay refuse link-local/metadata and direct IP destinations.
	if net.ParseIP(host) != nil {
		return fakeATEEgressDecision{allowed: false}, errFakeATEEgressDenied
	}
	port := int32(443)
	if u.Scheme == "http" {
		port = 80
	}
	if rawPort := u.Port(); rawPort != "" {
		parsed, parseErr := strconv.ParseInt(rawPort, 10, 32)
		require.NoError(t, parseErr)
		port = int32(parsed)
	}

	var matchedConcrete *ateapiv1.EgressRule
	var matchedWildcard *ateapiv1.EgressRule
	for _, rule := range p.rules {
		var ruleHosts []string
		var rulePorts *ateapiv1.Ports
		switch {
		case u.Scheme == "https" && rule.GetHttps() != nil:
			ruleHosts = rule.GetHttps().GetHostnames()
			rulePorts = rule.GetHttps().GetPorts()
		case u.Scheme == "https" && rule.GetTlsPassthrough() != nil:
			ruleHosts = rule.GetTlsPassthrough().GetHostnames()
			rulePorts = rule.GetTlsPassthrough().GetPorts()
		case u.Scheme == "http" && rule.GetHttp() != nil:
			ruleHosts = rule.GetHttp().GetHostnames()
			rulePorts = rule.GetHttp().GetPorts()
		default:
			continue
		}
		if !egressPortMatches(rulePorts, port) {
			continue
		}
		for _, candidate := range ruleHosts {
			candidate = strings.ToLower(candidate)
			if candidate == host {
				matchedConcrete = rule
				break
			}
			if candidate == "*" && matchedWildcard == nil {
				matchedWildcard = rule
			}
		}
	}

	chosen := matchedConcrete
	if chosen == nil {
		chosen = matchedWildcard
	}
	if chosen == nil {
		return fakeATEEgressDecision{allowed: false}, errFakeATEEgressDenied
	}
	if chosen.GetTlsPassthrough() != nil {
		return fakeATEEgressDecision{allowed: true, passthrough: true}, nil
	}

	var effects *ateapiv1.HttpRuleEffects
	if chosen.GetHttps() != nil {
		effects = chosen.GetHttps().GetEffects()
	} else if chosen.GetHttp() != nil {
		effects = chosen.GetHttp().GetEffects()
	}

	decision := fakeATEEgressDecision{allowed: true}
	for _, repl := range effects.GetReplaceHeaders() {
		if headers.Get(repl.GetHeader()) == "" {
			continue
		}
		require.Nil(t, repl.GetActorJwt(), "Plan B egress rules must use credentialUri, not actorJwt")
		uri := repl.GetCredentialUri()
		require.NotEmpty(t, uri)
		resp, fetchErr := p.credProvider.FetchSecret(ctx, &credproviderv1.FetchSecretRequest{
			Uri:           uri,
			ActorSpiffeId: p.actorSPIFFEID,
		})
		if fetchErr != nil {
			return decision, fmt.Errorf("fetch secret from ATE credential provider: %w", fetchErr)
		}
		headers.Set(repl.GetHeader(), repl.GetPrefix()+string(resp.GetOpaqueBytes()))
		decision.injectedHeader = repl.GetHeader()
		decision.injectedURI = uri
	}
	return decision, nil
}

func (p *fakeATEEgressProxy) doProjectGateway(t *testing.T, ctx context.Context, rawURL string, headers http.Header, body []byte) (*httptest.ResponseRecorder, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision, err := p.evaluate(t, ctx, rawURL, cloned)
	if err != nil {
		return nil, decision, err
	}
	bearer := strings.TrimPrefix(cloned.Get("Authorization"), "Bearer ")
	resp, err := serveProjectGatewayHTTP(t, p.ti, p.gateway, p.gateway.projectSlug, bearer, body)
	return resp, decision, err
}

func (p *fakeATEEgressProxy) doInference(t *testing.T, ctx context.Context, rawURL string, headers http.Header, body string) (*httptest.ResponseRecorder, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision, err := p.evaluate(t, ctx, rawURL, cloned)
	if err != nil {
		return nil, decision, err
	}
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, u.Path, strings.NewReader(body))
	req.Header = cloned
	rec := httptest.NewRecorder()
	if err := p.chatService.HandleCompletion(rec, req); err != nil {
		return rec, decision, fmt.Errorf("handle completion: %w", err)
	}
	return rec, decision, nil
}

func (p *fakeATEEgressProxy) doPublicHTTP(t *testing.T, ctx context.Context, rawURL string, headers http.Header) (int, []byte, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision, err := p.evaluate(t, ctx, rawURL, cloned)
	if err != nil {
		return 0, nil, decision, err
	}
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.publicServer.URL+u.RequestURI(), nil)
	require.NoError(t, err)
	req.Host = u.Host
	req.Header = cloned
	resp, err := p.publicServer.Client().Do(req)
	if err != nil {
		return 0, nil, decision, fmt.Errorf("public egress request: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	require.NoError(t, readErr)
	require.NoError(t, closeErr)
	return resp.StatusCode, body, decision, nil
}

func egressPortMatches(ports *ateapiv1.Ports, port int32) bool {
	if ports == nil {
		return false
	}
	return ports.GetAll() != nil || slices.Contains(ports.GetNumbers(), port)
}

func assertNoWildcardRuleHasEffects(t *testing.T, rules []*ateapiv1.EgressRule) {
	t.Helper()
	for _, rule := range rules {
		if hs := rule.GetHttps(); hs != nil {
			for _, host := range hs.GetHostnames() {
				require.NotContains(t, host, "*", "HTTPS credential-injection rule must never match wildcards")
			}
		}
		if h := rule.GetHttp(); h != nil {
			for _, host := range h.GetHostnames() {
				if strings.Contains(host, "*") {
					require.Nil(t, h.GetEffects(), "wildcard HTTP rule must never carry header effects")
				}
			}
		}
	}
}

func TestATEEgress_EndToEndCredentialInjectionAndWebAccess(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		gatewayOrigin   string
		inferenceOrigin string
		wantHTTPSRules  int
	}{
		{
			name:            "same host deduplicates to one HTTPS credential_uri rule",
			gatewayOrigin:   "https://app.speakeasy.example.com",
			inferenceOrigin: "https://app.speakeasy.example.com",
			wantHTTPSRules:  1,
		},
		{
			name:            "split hosts emit separate HTTPS credential_uri rules",
			gatewayOrigin:   "https://mcp.speakeasy.example.com",
			inferenceOrigin: "https://api.speakeasy.example.com",
			wantHTTPSRules:  2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorkloadGrantFixture(t)
			ctx := f.ctx
			ti := f.ti
			q := assistantrepo.New(ti.conn)
			project := f.fx.target.ProjectID
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			const (
				atespace          = "prod-assistants"
				providerSAEmail   = "ate-cred-provider@speakeasy-test.iam.gserviceaccount.com"
				providerGoogleJWT = "valid-google-oidc-id-token"
			)

			// Seed Project Gateway with one granted private MCP server.
			gateway := seedOrganizationGateway(t, ctx, ti)
			grantProjectRead(t, ti, authCtx)
			ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
			grantedServerID := mcpServerBySlug(t, ctx, ti, project, gateway.granted)

			// Provision a real assistant + dashboard trigger + identity binding.
			a, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
				ProjectID: project, OrganizationID: f.fx.orgID, CreatedByUserID: conv.ToPGText(f.fx.userID),
				Name: "ATE egress assistant", Model: "test/model", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
			})
			require.NoError(t, err)
			_, err = triggerrepo.New(ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
				OrganizationID: f.fx.orgID, ProjectID: project, DefinitionSlug: "dashboard", Name: "ATE dashboard root",
				TargetKind: "assistant", TargetRef: fmt.Sprintf("project:%s:assistant:%s", project, a.ID), TargetDisplay: a.Name, ConfigJson: []byte(`{}`), Status: "active",
			})
			require.NoError(t, err)

			identities := assistantidentity.New(ti.serverURL.String(), ti.audit)
			tx := testenv.BeginTx(t, ctx, ti.conn)
			require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
				OrganizationID: f.fx.orgID, ProjectID: project, AssistantID: a.ID, ActorUserID: f.fx.userID,
			}))
			require.NoError(t, tx.Commit(ctx))
			identity, err := identities.ResolveRuntime(ctx, ti.conn, f.fx.orgID, project, a.ID)
			require.NoError(t, err)

			// Grant mcp:connect on the Project Gateway member and inference:execute
			// to both the assistant's agent and its owner (4-way / 2-way policy).
			agentPrincipal := urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID.String())
			ownerPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID)
			mcpSel, err := (authz.Selector{
				authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
				authz.SelectorKeyResourceID:   grantedServerID.String(),
				authz.SelectorKeyProjectID:    project.String(),
			}).MarshalJSON()
			require.NoError(t, err)
			for _, p := range []urn.Principal{agentPrincipal, ownerPrincipal} {
				_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
					OrganizationID: f.fx.orgID, PrincipalUrn: p, Scope: string(authz.ScopeMCPConnect), Selectors: mcpSel,
				})
				require.NoError(t, err)
				seedWorkloadInferenceGrant(t, f, p, project)
			}

			// Seed thread, chat, ATE v3 runtime, and live running ATE thread actor.
			chatID := uuid.New()
			require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
				ChatID: chatID, ProjectID: project, OrganizationID: f.fx.orgID, UserID: conv.ToPGText(f.fx.userID),
			}))
			threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
				AssistantID: a.ID, ProjectID: project, CorrelationID: "ate-egress-thread", ChatID: chatID,
				SourceKind: "dashboard", SourceRefJson: []byte(`{}`),
			})
			require.NoError(t, err)
			require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
				ProjectID: project, AssistantID: a.ID, RuntimeVersion: 3,
			}))
			require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
				ProjectID: project, AssistantID: a.ID, AssistantThreadID: threadID, Backend: "ate", State: "active",
			}))
			runtimeRec, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
				ProjectID: project, AssistantID: a.ID,
			})
			require.NoError(t, err)
			runtimeMeta, err := json.Marshal(map[string]any{
				"runtime_id":   runtimeRec.ID,
				"assistant_id": a.ID,
				"project_id":   project,
				"image_digest": "sha256:" + strings.Repeat("a", 64),
				"atespace":     atespace,
				"template":     "kit-v1",
			})
			require.NoError(t, err)
			require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{
				ProjectID: project, RuntimeID: runtimeRec.ID, BackendMetadataJson: runtimeMeta,
			}))

			actorName := "spk-kit-" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
			actorUID := "ate-uid-" + uuid.NewString()
			actorRow, err := q.InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
				ActorName:   actorName,
				ActorUid:    actorUID,
				State:       "running",
				Configured:  true,
				WarmUntil:   pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				RuntimeID:   runtimeRec.ID,
				ProjectID:   project,
				AssistantID: a.ID,
				ThreadID:    threadID,
			})
			require.NoError(t, err)

			// Wire real assistants.ServiceCore -> real atecred mint HTTP handler -> real atecred.Server (FetchSecret).
			logger := testenv.NewLogger(t)
			tracer := testenv.NewTracerProvider(t)
			core := assistants.NewServiceCore(
				logger, tracer, testenv.NewMeterProvider(t), ti.conn, guardian.NewDefaultPolicy(tracer), ti.enc,
				nil, nil, ti.assistantTokens, ti.serverURL, nil, nil, ti.audit, identities, ti.authzEngine,
			)
			core.SetATEAtespace(atespace)

			mintMux := http.NewServeMux()
			mintServer := httptest.NewServer(mintMux)
			t.Cleanup(mintServer.Close)
			mintServerURL, err := url.Parse(mintServer.URL)
			require.NoError(t, err)
			mintURL, err := atecred.MintEndpointURL(mintServerURL)
			require.NoError(t, err)

			auth := atecred.NewAuthenticator(atecred.AuthenticatorConfig{
				Environment:      "production",
				ExpectedAudience: mintURL,
				ServiceAccount:   providerSAEmail,
				LocalToken:       "",
				Validator:        fakeATEEgressIDTokenValidator{expectedToken: providerGoogleJWT, email: providerSAEmail},
			})
			mintMux.Handle(atecred.MintEndpointPath, atecred.NewMintHandler(logger, auth, core))

			var clockOffset atomic.Int64
			mintClient := atecred.NewHTTPMintClient(atecred.HTTPMintClientConfig{
				EndpointURL:    mintURL,
				TokenSource:    oauth2.StaticTokenSource(&oauth2.Token{AccessToken: providerGoogleJWT}),
				HTTPClient:     mintServer.Client(),
				RequestTimeout: 3 * time.Second,
			})
			credProvider := atecred.NewServer(atecred.ServerConfig{
				Logger:   logger,
				Atespace: atespace,
				CacheTTL: time.Minute,
				Client:   mintClient,
				Now: func() time.Time {
					return time.Now().Add(time.Duration(clockOffset.Load()))
				},
			})

			// Wire real chat.Service with real TurnStream and stubbed OpenRouter model transport.
			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)
			turnStream := chat.NewTurnStream(redisClient)
			var inferenceCalls atomic.Int32
			completionClient := &managedInferenceCompletion{complete: func(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
				inferenceCalls.Add(1)
				require.Equal(t, billing.ModelUsageSourceAssistants, req.UsageSource)
				require.Equal(t, billing.ModelUsageSourceAssistants, req.KeySlot)
				require.Equal(t, chatID, req.ChatID)
				principal, hasPrincipal := contextvalues.GetAssistantPrincipal(ctx)
				require.True(t, hasPrincipal)
				require.Equal(t, a.ID, principal.AssistantID)
				require.Equal(t, threadID, principal.ThreadID)
				gotChat, hasChat := assistanttokens.RuntimeChatID(ctx)
				require.True(t, hasChat)
				require.Equal(t, chatID, gotChat)
				require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
				// Prove ChatMessageCaptureStrategy skips persistence for Kit-owned turn credentials.
				capture := new(chat.ChatMessageCaptureStrategy)
				session, capErr := capture.StartOrResumeChat(ctx, req)
				require.NoError(t, capErr)
				require.Nil(t, session)
				msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
				return &openrouter.CompletionResponse{Message: &msg, Model: "test/model"}, nil
			}}
			chatSvc := chat.NewService(
				logger, tracer, ti.conn, ti.sessionManager, ti.chatSessionsManager,
				nil, completionClient, nil, posthog.New(ctx, logger, "", "", ""),
				nil, nil, ti.authzEngine, ti.assistantTokens, billing.NewStubClient(logger, tracer),
				ti.audit, staff.Organization{},
			).WithWorkloadInference(ti.service).WithTurnStream(turnStream)

			var sawPublicAuth atomic.Value
			publicServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawPublicAuth.Store(r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("public-docs-ok"))
			}))
			t.Cleanup(publicServer.Close)

			// Build the real EgressPolicy rules from exact actor coordinates and URLs.
			gatewayURL := tc.gatewayOrigin + "/mcp/org/" + gateway.organizationSlug + "/" + gateway.projectSlug
			inferenceURL := tc.inferenceOrigin + "/chat/completions"
			rules, err := assistants.BuildKitATEEgressPolicyRules(atespace, actorName, actorUID, gatewayURL, inferenceURL)
			require.NoError(t, err)
			assertNoWildcardRuleHasEffects(t, rules)
			require.Len(t, rules, tc.wantHTTPSRules+2)

			wantURI, err := atecred.BuildCredentialURI(atespace, actorName, actorUID)
			require.NoError(t, err)
			actorSPIFFEID := fmt.Sprintf("spiffe://substrate-actor.local/actor/%s/%s", atespace, actorName)

			proxy := &fakeATEEgressProxy{
				rules:         rules,
				credProvider:  credProvider,
				actorSPIFFEID: actorSPIFFEID,
				gateway:       gateway,
				ti:            ti,
				chatService:   chatSvc,
				publicServer:  publicServer,
			}

			// 1. Project Gateway list_servers flows through egress proxy with ONLY non-secret placeholder.
			const placeholder = "ate-egress-injected-placeholder"
			placeholderHeaders := http.Header{"Authorization": []string{"Bearer " + placeholder}}
			gwResp, gwDecision, err := proxy.doProjectGateway(t, ctx, gatewayURL, placeholderHeaders, listServersBody(t))
			require.NoError(t, err)
			require.True(t, gwDecision.allowed)
			require.False(t, gwDecision.passthrough)
			require.Equal(t, "Authorization", gwDecision.injectedHeader)
			require.Equal(t, wantURI, gwDecision.injectedURI)
			require.Equal(t, http.StatusOK, gwResp.Code, gwResp.Body.String())
			text, isErr := metaToolResultText(t, decodeRPCResponse(t, gwResp))
			require.False(t, isErr, text)
			require.Contains(t, text, gateway.granted)

			// 2. Inference call flows through egress proxy with ONLY placeholder and hits real /chat/completions
			//    with Kit-owned transcript flag (zero chat_messages written, zero TurnStream events).
			infBody, err := json.Marshal(map[string]any{"model": "test/model", "messages": []any{}})
			require.NoError(t, err)
			infResp, infDecision, err := proxy.doInference(t, ctx, inferenceURL, placeholderHeaders, string(infBody))
			require.NoError(t, err)
			require.True(t, infDecision.allowed)
			require.False(t, infDecision.passthrough)
			require.Equal(t, "Authorization", infDecision.injectedHeader)
			require.Equal(t, wantURI, infDecision.injectedURI)
			require.Equal(t, http.StatusOK, infResp.Code, infResp.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())

			msgCount, err := chatrepo.New(ti.conn).CountChatMessages(ctx, chatrepo.CountChatMessagesParams{ProjectID: project, ChatID: chatID})
			require.NoError(t, err)
			require.Zero(t, msgCount, "Kit-owned inference must not duplicate transcript rows")

			// 3. Public HTTPS web access flows through "*" tls_passthrough with NO credential injection.
			pubStatus, pubBody, pubDecision, err := proxy.doPublicHTTP(t, ctx, "https://public-docs.example.org/guide", http.Header{})
			require.NoError(t, err)
			require.True(t, pubDecision.allowed)
			require.True(t, pubDecision.passthrough)
			require.Empty(t, pubDecision.injectedHeader)
			require.Empty(t, pubDecision.injectedURI)
			require.Equal(t, http.StatusOK, pubStatus)
			require.Equal(t, "public-docs-ok", string(bytes.TrimSpace(pubBody)))
			require.Empty(t, sawPublicAuth.Load(), "public web traffic must never receive a Speakeasy turn credential")

			// Third-party Authorization header passes through untouched.
			pubAuthStatus, _, pubAuthDecision, err := proxy.doPublicHTTP(t, ctx, "https://api.github.com/repos/example/repo", http.Header{
				"Authorization": []string{"Bearer user-third-party-token"},
			})
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, pubAuthStatus)
			require.True(t, pubAuthDecision.passthrough)
			require.Equal(t, "Bearer user-third-party-token", sawPublicAuth.Load())

			// 4. Direct IP / metadata / private-IP destinations are refused by the egress proxy.
			for _, blockedURL := range []string{
				"http://169.254.169.254/computeMetadata/v1/",
				"https://10.0.0.1/internal",
				"https://127.0.0.1/local",
			} {
				_, _, blockedDecision, blockedErr := proxy.doPublicHTTP(t, ctx, blockedURL, http.Header{})
				require.ErrorIs(t, blockedErr, errFakeATEEgressDenied, "expected %s to be denied", blockedURL)
				require.False(t, blockedDecision.allowed)
			}

			// 5. Spoofed actor SPIFFE ID is refused by FetchSecret with PermissionDenied.
			spoofedProxy := *proxy
			spoofedProxy.actorSPIFFEID = fmt.Sprintf("spiffe://substrate-actor.local/actor/%s/other-actor", atespace)
			_, _, err = spoofedProxy.doProjectGateway(t, ctx, gatewayURL, placeholderHeaders, listServersBody(t))
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, _, err = spoofedProxy.doInference(t, ctx, inferenceURL, placeholderHeaders, string(infBody))
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			require.Equal(t, int32(1), inferenceCalls.Load())

			// Capture a still-unexpired minted credential before retiring the actor/thread.
			fetched, err := credProvider.FetchSecret(ctx, &credproviderv1.FetchSecretRequest{
				Uri:           wantURI,
				ActorSpiffeId: actorSPIFFEID,
			})
			require.NoError(t, err)
			cachedToken := string(fetched.GetOpaqueBytes())
			require.NotEmpty(t, cachedToken)

			// 6. Retiring the ATE actor immediately rejects new mint requests (PermissionDenied) once cache expires.
			deletedRows, err := q.SoftDeleteAssistantATEActor(ctx, assistantrepo.SoftDeleteAssistantATEActorParams{
				RetiredState: "retired", ProjectID: project, ID: actorRow.ID,
			})
			require.NoError(t, err)
			require.Equal(t, int64(1), deletedRows)
			clockOffset.Store(int64(2 * time.Minute))

			_, _, err = proxy.doProjectGateway(t, ctx, gatewayURL, placeholderHeaders, listServersBody(t))
			require.Equal(t, codes.PermissionDenied, status.Code(err))
			_, _, err = proxy.doInference(t, ctx, inferenceURL, placeholderHeaders, string(infBody))
			require.Equal(t, codes.PermissionDenied, status.Code(err))

			// 7. Soft-deleting the thread immediately rejects even a previously-minted cached credential.
			require.NoError(t, q.SoftDeleteAssistantThread(ctx, assistantrepo.SoftDeleteAssistantThreadParams{
				ID: threadID, ProjectID: project,
			}))
			_, err = serveProjectGatewayHTTP(t, ti, gateway, gateway.projectSlug, cachedToken, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)

			infReq := httptest.NewRequestWithContext(ctx, http.MethodPost, "/chat/completions", strings.NewReader(string(infBody)))
			infReq.Header.Set("Authorization", "Bearer "+cachedToken)
			err = chatSvc.HandleCompletion(httptest.NewRecorder(), infReq)
			requireOopsCode(t, err, oops.CodeUnauthorized)
			require.Equal(t, int32(1), inferenceCalls.Load())
		})
	}
}
