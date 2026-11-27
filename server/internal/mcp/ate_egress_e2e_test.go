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
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

var errFakeATEEgressDenied = errors.New("ATE egress proxy denied request")

// fakeATEEgressProxy implements a policy-enforcing egress proxy over an ATE
// EgressPolicy rule list:
//   - refuses IP literals (mirroring ATE's hostname-only DNS relay and nftables
//     blocking of link-local/metadata/private IP destinations);
//   - applies ATE's most-specific-hostname-wins precedence across concrete
//     HTTPS rules and wildcard passthrough/HTTP rules;
//   - replaces Authorization with a freshly signed actor JWT strictly on
//     matched concrete HTTPS rules with replace_headers;
//   - dispatches matched requests through to the target backend handler.
type fakeATEEgressProxy struct {
	rules        []*ateapiv1.EgressRule
	mintJWT      func(audiences []string) string
	gatewayHost  string
	gateway      organizationGatewayFixture
	ti           *testInstance
	chatHost     string
	chatRouter   goahttp.Muxer
	publicServer *httptest.Server
}

type fakeATEEgressDecision struct {
	allowed          bool
	passthrough      bool
	injectedHeader   string
	injectedAudience []string
}

func (p *fakeATEEgressProxy) evaluate(t *testing.T, rawURL string, headers http.Header) fakeATEEgressDecision {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	host := strings.ToLower(u.Hostname())
	// ATE hostname rules never match IP literals, and ateworker nftables +
	// DNS relay refuse link-local/metadata and direct IP destinations.
	if net.ParseIP(host) != nil {
		return fakeATEEgressDecision{allowed: false}
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
		return fakeATEEgressDecision{allowed: false}
	}
	if chosen.GetTlsPassthrough() != nil {
		return fakeATEEgressDecision{allowed: true, passthrough: true}
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
		actorJWT := repl.GetActorJwt()
		require.NotNil(t, actorJWT)
		token := p.mintJWT(actorJWT.GetAudiences())
		headers.Set(repl.GetHeader(), repl.GetPrefix()+token)
		decision.injectedHeader = repl.GetHeader()
		decision.injectedAudience = slices.Clone(actorJWT.GetAudiences())
	}
	return decision
}

func (p *fakeATEEgressProxy) doProjectGateway(t *testing.T, rawURL string, headers http.Header, body []byte) (*httptest.ResponseRecorder, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision := p.evaluate(t, rawURL, cloned)
	if !decision.allowed {
		return nil, decision, errFakeATEEgressDenied
	}
	bearer := strings.TrimPrefix(cloned.Get("Authorization"), "Bearer ")
	resp, err := serveProjectGatewayHTTP(t, p.ti, p.gateway, p.gateway.projectSlug, bearer, body)
	return resp, decision, err
}

func (p *fakeATEEgressProxy) doInference(t *testing.T, rawURL string, headers http.Header, body string) (*httptest.ResponseRecorder, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision := p.evaluate(t, rawURL, cloned)
	if !decision.allowed {
		return nil, decision, errFakeATEEgressDenied
	}
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	bearer := strings.TrimPrefix(cloned.Get("Authorization"), "Bearer ")
	resp := postCompletionRequest(p.chatRouter, u.Path, bearer, nil, body)
	return resp, decision, nil
}

func (p *fakeATEEgressProxy) doPublicHTTP(t *testing.T, ctx context.Context, rawURL string, headers http.Header) (int, []byte, fakeATEEgressDecision, error) {
	t.Helper()
	cloned := headers.Clone()
	decision := p.evaluate(t, rawURL, cloned)
	if !decision.allowed {
		return 0, nil, decision, errFakeATEEgressDenied
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

func assertNoWildcardRuleHasJWT(t *testing.T, rules []*ateapiv1.EgressRule) {
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
		name              string
		gatewayOrigin     string
		inferenceOrigin   string
		wantGatewayAuds   []string
		wantInferenceAuds []string
	}{
		{
			name:            "same host combines both audiences on one HTTPS rule",
			gatewayOrigin:   "https://app.speakeasy.example.com",
			inferenceOrigin: "https://app.speakeasy.example.com",
			wantGatewayAuds: []string{
				mcp.DirectWorkloadInferenceAudience,
				mcp.DirectWorkloadProjectGatewayAudience,
			},
			wantInferenceAuds: []string{
				mcp.DirectWorkloadInferenceAudience,
				mcp.DirectWorkloadProjectGatewayAudience,
			},
		},
		{
			name:              "split hosts scope each HTTPS rule to its single audience",
			gatewayOrigin:     "https://mcp.speakeasy.example.com",
			inferenceOrigin:   "https://api.speakeasy.example.com",
			wantGatewayAuds:   []string{mcp.DirectWorkloadProjectGatewayAudience},
			wantInferenceAuds: []string{mcp.DirectWorkloadInferenceAudience},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newWorkloadGrantFixture(t)
			f.setAssertionReuse(t, true)
			ctx := t.Context()
			q := assistantrepo.New(f.ti.conn)
			project := f.fx.target.ProjectID

			// Provision an assistant + backing agent and assign an ATE thread actor subject to it.
			actorSubject := "actor/prod-assistants/spk-kit-" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
			f.subject = actorSubject

			a, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
				ProjectID: project, OrganizationID: f.fx.orgID, CreatedByUserID: conv.ToPGText(f.fx.userID),
				Name: "ATE egress assistant", Model: "test/model", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
			})
			require.NoError(t, err)
			_, err = triggerrepo.New(f.ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
				OrganizationID: f.fx.orgID, ProjectID: project, DefinitionSlug: "manual", Name: "ATE manual root",
				TargetKind: "assistant", TargetRef: a.ID.String(), TargetDisplay: a.Name, ConfigJson: []byte(`{}`), Status: "active",
			})
			require.NoError(t, err)

			identities := assistantidentity.New(f.ti.serverURL.String(), f.ti.audit)
			tx := testenv.BeginTx(t, ctx, f.ti.conn)
			require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
				OrganizationID: f.fx.orgID, ProjectID: project, AssistantID: a.ID, ActorUserID: f.fx.userID,
			}))
			require.NoError(t, tx.Commit(ctx))
			identity, err := identities.ResolveRuntime(ctx, f.ti.conn, f.fx.orgID, project, a.ID)
			require.NoError(t, err)

			// Re-point the workload fixture's agent, admission, and assignment to the ATE thread actor subject.
			unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
			f.agentID = identity.AgentID
			require.NoError(t, testrepo.New(f.ti.conn).CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
				OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{UUID: project, Valid: true},
				WorkloadIssuerID: f.issuerID, Subject: actorSubject,
			}))
			f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, actorSubject, f.agentID)

			gateway, _, mcpUpstream := seedDirectWorkloadProjectGateway(t, f)
			enableWorkloadInference(t, f)

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
			runtime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
				ProjectID: project, AssistantID: a.ID,
			})
			require.NoError(t, err)

			bindingJSON, err := json.Marshal(map[string]any{
				"issuer_id":           f.issuerID,
				"subject":             actorSubject,
				"identity":            identity,
				"mcp_project_gateway": true,
			})
			require.NoError(t, err)
			actorRow, err := q.InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
				ActorName:           strings.TrimPrefix(actorSubject, "actor/prod-assistants/"),
				ActorUid:            "ate-uid-" + uuid.NewString(),
				State:               "running",
				Configured:          true,
				WorkloadBindingJson: bindingJSON,
				WarmUntil:           pgtype.Timestamptz{Time: time.Now().Add(5 * time.Minute), Valid: true},
				RuntimeID:           runtime.ID,
				ProjectID:           project,
				AssistantID:         a.ID,
				ThreadID:            threadID,
			})
			require.NoError(t, err)

			const acpSessionID = "ate-acp-session-1"
			_, err = q.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{
				SessionID: acpSessionID, RuntimeID: runtime.ID, ProjectID: project, AssistantID: a.ID, ThreadID: threadID,
			})
			require.NoError(t, err)

			var inferenceCalls atomic.Int32
			completionClient := &managedInferenceCompletion{complete: func(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
				inferenceCalls.Add(1)
				require.Equal(t, billing.ModelUsageSourceAssistants, req.UsageSource)
				require.Equal(t, billing.ModelUsageSourceAssistants, req.KeySlot)
				require.Equal(t, chatID, req.ChatID)
				principal, ok := contextvalues.GetAssistantPrincipal(ctx)
				require.True(t, ok)
				require.Equal(t, a.ID, principal.AssistantID)
				require.Equal(t, threadID, principal.ThreadID)
				require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
				msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
				return &openrouter.CompletionResponse{Message: &msg, Model: "test/model"}, nil
			}}
			chatRouter := newDirectWorkloadChatMux(t, f, completionClient)

			var sawPublicAuth atomic.Value
			publicServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawPublicAuth.Store(r.Header.Get("Authorization"))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("public-docs-ok"))
			}))
			t.Cleanup(publicServer.Close)

			// Build the real EgressPolicy rules from the exact Project Gateway and inference URLs.
			gatewayURL := tc.gatewayOrigin + "/mcp/org/" + gateway.organizationSlug + "/" + gateway.projectSlug
			inferenceURL := tc.inferenceOrigin + "/projects/" + project.String() + "/chat/completions"
			rules, err := assistants.BuildKitATEEgressPolicyRules(gatewayURL, inferenceURL)
			require.NoError(t, err)
			assertNoWildcardRuleHasJWT(t, rules)

			proxy := &fakeATEEgressProxy{
				rules: rules,
				mintJWT: func(audiences []string) string {
					return f.directAssertion(t, actorSubject, audiences...)
				},
				gatewayHost:  tc.gatewayOrigin,
				gateway:      gateway,
				ti:           f.ti,
				chatHost:     tc.inferenceOrigin,
				chatRouter:   chatRouter,
				publicServer: publicServer,
			}

			// 1. Project Gateway tool call flows through the egress proxy with ONLY a non-secret placeholder.
			const placeholder = "ate-egress-injected-placeholder"
			placeholderHeaders := http.Header{"Authorization": []string{"Bearer " + placeholder}}
			callBody := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
				"name":      "execute_tool",
				"arguments": map[string]any{"name": gateway.granted + "--ping", "arguments": map[string]any{}},
			})
			gwResp, gwDecision, err := proxy.doProjectGateway(t, gatewayURL, placeholderHeaders, callBody)
			require.NoError(t, err)
			require.True(t, gwDecision.allowed)
			require.False(t, gwDecision.passthrough)
			require.Equal(t, "Authorization", gwDecision.injectedHeader)
			require.Equal(t, tc.wantGatewayAuds, gwDecision.injectedAudience)
			require.Equal(t, http.StatusOK, gwResp.Code, gwResp.Body.String())
			text, isErr := metaToolResultText(t, decodeRPCResponse(t, gwResp))
			require.False(t, isErr, text)
			require.Equal(t, 1, mcpUpstream.toolCalls())

			// 2. Inference call flows through the egress proxy with ONLY the placeholder and hits the
			//    real /projects/{projectID}/chat/completions handler with assistant_ate_actors attribution.
			infBody, err := json.Marshal(map[string]any{"model": "test/model", "messages": []any{}, "user": acpSessionID})
			require.NoError(t, err)
			infResp, infDecision, err := proxy.doInference(t, inferenceURL, placeholderHeaders, string(infBody))
			require.NoError(t, err)
			require.True(t, infDecision.allowed)
			require.False(t, infDecision.passthrough)
			require.Equal(t, "Authorization", infDecision.injectedHeader)
			require.Equal(t, tc.wantInferenceAuds, infDecision.injectedAudience)
			require.Equal(t, http.StatusOK, infResp.Code, infResp.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())

			// 3. Public HTTPS web access flows through "*" tls_passthrough with NO header injection.
			pubStatus, pubBody, pubDecision, err := proxy.doPublicHTTP(t, ctx, "https://public-docs.example.org/guide", http.Header{})
			require.NoError(t, err)
			require.True(t, pubDecision.allowed)
			require.True(t, pubDecision.passthrough)
			require.Empty(t, pubDecision.injectedHeader)
			require.Empty(t, pubDecision.injectedAudience)
			require.Equal(t, http.StatusOK, pubStatus)
			require.Equal(t, "public-docs-ok", string(bytes.TrimSpace(pubBody)))
			require.Empty(t, sawPublicAuth.Load(), "public web traffic must never receive an injected actor JWT")

			// Even if the sandbox sends its own Authorization header to a public host,
			// the passthrough rule preserves it untouched without injecting an actor JWT.
			pubAuthStatus, _, pubAuthDecision, err := proxy.doPublicHTTP(t, ctx, "https://api.github.com/repos/example/repo", http.Header{
				"Authorization": []string{"Bearer user-third-party-token"},
			})
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, pubAuthStatus)
			require.True(t, pubAuthDecision.allowed)
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

			// 5. Non-admitted actor subject is rejected by Speakeasy at both Project Gateway and inference.
			unadmittedProxy := *proxy
			unadmittedProxy.mintJWT = func(audiences []string) string {
				return f.directAssertion(t, "actor/prod-assistants/unadmitted-actor", audiences...)
			}
			_, _, err = unadmittedProxy.doProjectGateway(t, gatewayURL, placeholderHeaders, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)
			unadmittedInf, _, err := unadmittedProxy.doInference(t, inferenceURL, placeholderHeaders, string(infBody))
			require.NoError(t, err)
			require.Equal(t, http.StatusUnauthorized, unadmittedInf.Code, unadmittedInf.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())

			// 6. Retiring the ATE actor (withdrawing assignment, admission, and soft-deleting the actor row)
			//    immediately rejects its token at both Project Gateway and inference.
			unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
			_, err = testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(ctx, testrepo.SoftDeleteWorkloadAdmissionFixtureParams{
				OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: actorSubject,
			})
			require.NoError(t, err)
			deletedRows, err := q.SoftDeleteAssistantATEActor(ctx, assistantrepo.SoftDeleteAssistantATEActorParams{
				RetiredState: "retired", ProjectID: project, ID: actorRow.ID,
			})
			require.NoError(t, err)
			require.Equal(t, int64(1), deletedRows)

			_, _, err = proxy.doProjectGateway(t, gatewayURL, placeholderHeaders, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)
			retiredInf, _, err := proxy.doInference(t, inferenceURL, placeholderHeaders, string(infBody))
			require.NoError(t, err)
			require.Equal(t, http.StatusUnauthorized, retiredInf.Code, retiredInf.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())
		})
	}
}
