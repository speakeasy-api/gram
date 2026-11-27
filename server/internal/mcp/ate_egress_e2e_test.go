package mcp_test

import (
	"context"
	"encoding/json"
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

// fakeATEEgressProxy evaluates an ATE EgressPolicy rule list using ATE's
// most-specific-hostname-wins precedence and applies HTTPS replace_headers
// actorJwt injection only when a concrete HTTPS rule matches.
type fakeATEEgressProxy struct {
	rules   []*ateapiv1.EgressRule
	mintJWT func(audiences []string) string
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
	port := int32(443)
	if u.Scheme == "http" {
		port = 80
	}
	if rawPort := u.Port(); rawPort != "" {
		parsed, parseErr := strconv.ParseInt(rawPort, 10, 32)
		require.NoError(t, parseErr)
		port = int32(parsed)
	}

	// Concrete hostname rules win over wildcard "*" rules, mirroring ATE egress matching.
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
			}

			// 1. Project Gateway tool call: Kit sends only a non-secret placeholder Authorization header;
			//    the egress proxy replaces it with the actor JWT, and the real Project Gateway executes the tool.
			const placeholder = "ate-egress-injected-placeholder"
			gwHeaders := http.Header{"Authorization": []string{"Bearer " + placeholder}}
			gwDecision := proxy.evaluate(t, gatewayURL, gwHeaders)
			require.True(t, gwDecision.allowed)
			require.False(t, gwDecision.passthrough)
			require.Equal(t, "Authorization", gwDecision.injectedHeader)
			require.Equal(t, tc.wantGatewayAuds, gwDecision.injectedAudience)
			gwInjectedToken := strings.TrimPrefix(gwHeaders.Get("Authorization"), "Bearer ")
			require.NotEqual(t, placeholder, gwInjectedToken)

			callBody := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
				"name":      "execute_tool",
				"arguments": map[string]any{"name": gateway.granted + "--ping", "arguments": map[string]any{}},
			})
			gwResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, gwInjectedToken, callBody)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, gwResp.Code, gwResp.Body.String())
			text, isErr := metaToolResultText(t, decodeRPCResponse(t, gwResp))
			require.False(t, isErr, text)
			require.Equal(t, 1, mcpUpstream.toolCalls())

			// 2. Inference call: Kit (provider="openrouter" + OPENROUTER_BASE_URL + OPENROUTER_API_KEY placeholder)
			//    sends only the placeholder Authorization header; egress injects the actor JWT; the real
			//    /projects/{projectID}/chat/completions handler attributes it via assistant_ate_actors.
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

			infHeaders := http.Header{"Authorization": []string{"Bearer " + placeholder}}
			infDecision := proxy.evaluate(t, inferenceURL, infHeaders)
			require.True(t, infDecision.allowed)
			require.False(t, infDecision.passthrough)
			require.Equal(t, "Authorization", infDecision.injectedHeader)
			require.Equal(t, tc.wantInferenceAuds, infDecision.injectedAudience)
			infInjectedToken := strings.TrimPrefix(infHeaders.Get("Authorization"), "Bearer ")
			require.NotEqual(t, placeholder, infInjectedToken)

			projectPath := "/projects/" + project.String() + "/chat/completions"
			infBody, err := json.Marshal(map[string]any{"model": "test/model", "messages": []any{}, "user": acpSessionID})
			require.NoError(t, err)
			infResp := postCompletionRequest(chatRouter, projectPath, infInjectedToken, nil, string(infBody))
			require.Equal(t, http.StatusOK, infResp.Code, infResp.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())

			// 3. Public HTTPS web access: arbitrary public host traverses "*" tls_passthrough with NO header injection.
			var sawPublicAuth string
			publicServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sawPublicAuth = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("public-docs-ok"))
			}))
			t.Cleanup(publicServer.Close)

			pubHeaders := http.Header{}
			pubDecision := proxy.evaluate(t, "https://public-docs.example.org/guide", pubHeaders)
			require.True(t, pubDecision.allowed)
			require.True(t, pubDecision.passthrough)
			require.Empty(t, pubDecision.injectedHeader)
			require.Empty(t, pubDecision.injectedAudience)
			require.Empty(t, pubHeaders.Get("Authorization"))

			pubReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicServer.URL+"/guide", nil)
			require.NoError(t, err)
			pubReq.Header = pubHeaders
			pubResp, err := publicServer.Client().Do(pubReq)
			require.NoError(t, err)
			require.NoError(t, pubResp.Body.Close())
			require.Equal(t, http.StatusOK, pubResp.StatusCode)
			require.Empty(t, sawPublicAuth, "public web traffic must never receive an injected actor JWT")

			// Even if the sandbox explicitly sends an Authorization header to a public host,
			// the passthrough rule does not inject an actor JWT.
			pubWithAuth := http.Header{"Authorization": []string{"Bearer user-third-party-token"}}
			pubAuthDecision := proxy.evaluate(t, "https://api.github.com/repos/example/repo", pubWithAuth)
			require.True(t, pubAuthDecision.allowed)
			require.True(t, pubAuthDecision.passthrough)
			require.Equal(t, "Bearer user-third-party-token", pubWithAuth.Get("Authorization"))

			// 4. Non-admitted actor subject is rejected by Speakeasy at both Project Gateway and inference.
			unadmittedToken := f.directAssertion(t, "actor/prod-assistants/unadmitted-actor",
				mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)
			_, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, unadmittedToken, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)
			unadmittedInf := postCompletionRequest(chatRouter, projectPath, unadmittedToken, nil, string(infBody))
			require.Equal(t, http.StatusUnauthorized, unadmittedInf.Code, unadmittedInf.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())

			// 5. Retiring the ATE actor (withdrawing assignment, admission, and soft-deleting the actor row)
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

			_, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, gwInjectedToken, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)
			retiredInf := postCompletionRequest(chatRouter, projectPath, infInjectedToken, nil, string(infBody))
			require.Equal(t, http.StatusUnauthorized, retiredInf.Code, retiredInf.Body.String())
			require.Equal(t, int32(1), inferenceCalls.Load())
		})
	}
}
