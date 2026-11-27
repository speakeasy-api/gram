package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthtest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/staff"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (f workloadGrantFixture) directAssertion(t *testing.T, subject string, audiences ...string) string {
	t.Helper()
	now := time.Now()
	claims := jwt.Claims{
		Issuer:    f.issuer.OAuth21URL,
		Subject:   subject,
		Audience:  jwt.Audience(audiences),
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}
	return oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", claims)
}

func seedDirectWorkloadProjectGateway(t *testing.T, f workloadGrantFixture) (organizationGatewayFixture, uuid.UUID, *recordingUpstream) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	gateway := seedOrganizationGateway(t, f.ctx, f.ti)
	upstream := newRecordingUpstream(t, "ping")
	remote := remotemcptest.SeedServer(t, f.ctx, f.ti.conn, remotemcprepo.CreateServerParams{
		ProjectID: projectID, TransportType: "streamable-http", Url: upstream.url,
	})
	grantedID := mcpServerBySlug(t, f.ctx, f.ti, projectID, gateway.granted)
	memberIssuerID := createUserSessionIssuer(t, f.ctx, f.ti.conn, projectID)
	_, err := mcpserversrepo.New(f.ti.conn).UpdateMCPServer(f.ctx, mcpserversrepo.UpdateMCPServerParams{
		ID: grantedID, ProjectID: projectID, Name: conv.ToPGText(gateway.granted), Slug: conv.ToPGText(gateway.granted),
		RemoteMcpServerID: conv.ToNullUUID(remote.ID), UserSessionIssuerID: conv.ToNullUUID(memberIssuerID), Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)

	seedUserMCPConnectGrant(t, f.ctx, f.ti.conn, authCtx.ActiveOrganizationID, authCtx.UserID, grantedID.String())
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, authCtx.ActiveOrganizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), grantedID)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, f.fx.orgID, true)
	return gateway, grantedID, upstream
}

func newDirectWorkloadChatMux(t *testing.T, f workloadGrantFixture, upstream openrouter.CompletionClient) goahttp.Muxer {
	t.Helper()
	billingRepo := &workloadInferenceBilling{Repository: billing.NewStubClient(f.ti.logger, f.ti.tracerProvider)}
	svc := chat.NewService(
		f.ti.logger, f.ti.tracerProvider, f.ti.conn, f.ti.sessionManager, f.ti.chatSessionsManager,
		nil, upstream, nil, posthog.New(t.Context(), f.ti.logger, "", "", ""), nil, nil,
		f.ti.authzEngine,
		assistanttokens.New("direct-workload-test-secret", f.ti.conn, f.ti.authzEngine, f.ti.principalCredentials, f.ti.cacheAdapter),
		billingRepo, f.ti.audit, staff.Organization{},
	).WithWorkloadInference(f.ti.service)
	router := goahttp.NewMuxer()
	chat.Attach(router, svc)
	return router
}

func postCompletionRequest(router goahttp.Muxer, path, bearer string, headers map[string]string, body string) *httptest.ResponseRecorder {
	if body == "" {
		body = `{"model":"test-model","messages":[{"role":"user","content":"hello"}],"stream":false}`
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestDirectWorkloadBearer_ProjectGatewayListsAndCallsTools(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	gateway, _, upstream := seedDirectWorkloadProjectGateway(t, f)

	token := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience)

	// tools/list works with the workload JWT presented directly as a bearer.
	toolsList, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, makeMetaRPCBody(t, mcpversions.MethodToolsList, map[string]any{}))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, toolsList.Code, toolsList.Body.String())
	require.Contains(t, toolsList.Body.String(), "list_servers")
	require.Contains(t, toolsList.Body.String(), "execute_tool")

	// list_servers enforces the agent's live mcp:connect grants.
	listed, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, listed.Code, listed.Body.String())
	require.Contains(t, listed.Body.String(), gateway.granted)
	require.Contains(t, listed.Body.String(), gateway.open)
	require.NotContains(t, listed.Body.String(), gateway.denied)

	// execute_tool calls the upstream MCP server and reuses the exact same token across calls.
	callBody := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
		"name":      "execute_tool",
		"arguments": map[string]any{"name": gateway.granted + "--ping", "arguments": map[string]any{}},
	})
	for attempt := 1; attempt <= 2; attempt++ {
		resp, callErr := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, callBody)
		require.NoError(t, callErr)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		text, isError := metaToolResultText(t, decodeRPCResponse(t, resp))
		require.False(t, isError, text)
		require.Equal(t, attempt, upstream.toolCalls(), "reused direct-bearer JWT must execute upstream tools on every call")
	}
	require.Empty(t, f.workloadSessions(t), "direct-bearer authentication must not persist user_sessions rows")
}

func TestDirectWorkloadBearer_InferenceCompletesAndReusesToken(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	enableWorkloadInference(t, f)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	token := f.directAssertion(t, f.subject, mcp.DirectWorkloadInferenceAudience)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	for attempt := 1; attempt <= 2; attempt++ {
		w := postCompletionRequest(router, projectPath, token, nil, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), "workload reply")
		require.Len(t, upstream.requests, attempt)
		req := upstream.requests[attempt-1]
		require.Equal(t, f.fx.orgID, req.OrgID)
		require.Equal(t, f.fx.target.ProjectID.String(), req.ProjectID)
		require.Equal(t, billing.ModelUsageSourcePlayground, req.UsageSource)
		require.Equal(t, billing.ModelUsageSourcePlayground, req.KeySlot)
		require.Equal(t, openrouter.KeyTypeChat, req.KeyType)
		require.Equal(t, uuid.Nil, req.ChatID)
	}

	// Conflicting Gram-Project header is refused even with a valid direct-bearer JWT.
	w := postCompletionRequest(router, projectPath, token, map[string]string{"Gram-Project": uuid.NewString()}, "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Len(t, upstream.requests, 2)
}

func TestDirectWorkloadBearer_ManagedInferenceAttributionAndRetirement(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	ctx := t.Context()
	q := assistantrepo.New(f.ti.conn)
	project := f.fx.target.ProjectID

	a, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID: project, OrganizationID: f.fx.orgID, CreatedByUserID: conv.ToPGText(f.fx.userID),
		Name: "Direct bearer assistant", Model: "test/model", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	_, err = triggerrepo.New(f.ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: f.fx.orgID, ProjectID: project, DefinitionSlug: "manual", Name: "Managed root",
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

	unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
	f.agentID = identity.AgentID
	f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, f.agentID)
	enableWorkloadInference(t, f)

	chatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID: chatID, ProjectID: project, OrganizationID: f.fx.orgID, UserID: conv.ToPGText(f.fx.userID),
	}))
	thread, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID: a.ID, ProjectID: project, CorrelationID: "direct-managed-inference", ChatID: chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)
	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{ProjectID: project, AssistantID: a.ID, RuntimeVersion: 3}))
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID, AssistantThreadID: thread, Backend: "local", State: "active"}))
	runtime, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{ProjectID: project, AssistantID: a.ID})
	require.NoError(t, err)
	metadata, err := json.Marshal(map[string]any{"workload": map[string]any{"issuer_id": f.issuerID, "subject": f.subject, "identity": identity}})
	require.NoError(t, err)
	require.NoError(t, q.UpdateAssistantRuntimeMetadata(ctx, assistantrepo.UpdateAssistantRuntimeMetadataParams{ProjectID: project, RuntimeID: runtime.ID, BackendMetadataJson: metadata}))
	const sessionID = "direct-managed-acp-session"
	_, err = q.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{
		SessionID: sessionID, RuntimeID: runtime.ID, ProjectID: project, AssistantID: a.ID, ThreadID: thread,
	})
	require.NoError(t, err)

	var calls atomic.Int32
	client := &managedInferenceCompletion{complete: func(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
		calls.Add(1)
		require.Equal(t, billing.ModelUsageSourceAssistants, request.UsageSource)
		require.Equal(t, billing.ModelUsageSourceAssistants, request.KeySlot)
		require.Equal(t, chatID, request.ChatID)
		principal, ok := contextvalues.GetAssistantPrincipal(ctx)
		require.True(t, ok)
		require.Equal(t, a.ID, principal.AssistantID)
		require.Equal(t, thread, principal.ThreadID)
		require.True(t, assistanttokens.InferenceTranscriptOwned(ctx))
		message := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
		return &openrouter.CompletionResponse{Message: &message, Model: "test/model"}, nil
	}}
	router := newDirectWorkloadChatMux(t, f, client)
	token := f.directAssertion(t, f.subject, mcp.DirectWorkloadInferenceAudience)
	projectPath := "/projects/" + project.String() + "/chat/completions"
	body, err := json.Marshal(map[string]any{"model": "test/model", "messages": []any{}, "user": sessionID})
	require.NoError(t, err)

	w := postCompletionRequest(router, projectPath, token, map[string]string{"X-Gram-Source": "gram"}, string(body))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, int32(1), calls.Load())

	// Retiring the runtime (withdrawing assignment and admission) immediately rejects the warm token.
	unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
	_, err = testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(ctx, testrepo.SoftDeleteWorkloadAdmissionFixtureParams{
		OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject,
	})
	require.NoError(t, err)
	w = postCompletionRequest(router, projectPath, token, nil, string(body))
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, int32(1), calls.Load())
}

func TestDirectWorkloadBearer_AudienceIsolationAndMultiAudience(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	gateway, _, _ := seedDirectWorkloadProjectGateway(t, f)
	enableWorkloadInference(t, f)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	gatewayOnly := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience)
	inferenceOnly := f.directAssertion(t, f.subject, mcp.DirectWorkloadInferenceAudience)
	bothAudiences := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)
	neitherAudience := f.directAssertion(t, f.subject, f.advertisedIssuer)
	sessionAudience := f.directAssertion(t, f.subject, mcp.InferenceResource)

	// Gateway-only token is rejected at inference.
	w := postCompletionRequest(router, projectPath, gatewayOnly, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// Inference-only token is rejected at Project Gateway with standard 401 + WWW-Authenticate challenge.
	gwResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, inferenceOnly, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, gwResp.Header().Get("WWW-Authenticate"), "Bearer resource_metadata=")

	// Tokens carrying neither endpoint audience (including the Speakeasy session audience) fail on both endpoints.
	for _, bad := range []string{neitherAudience, sessionAudience} {
		gwResp, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, bad, listServersBody(t))
		requireOopsCode(t, err, oops.CodeUnauthorized)
		require.Contains(t, gwResp.Header().Get("WWW-Authenticate"), "Bearer resource_metadata=")

		w = postCompletionRequest(router, projectPath, bad, nil, "")
		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	}

	// Unscoped /chat/completions rejects direct-bearer workload JWTs without falling back.
	w = postCompletionRequest(router, "/chat/completions", inferenceOnly, map[string]string{"Gram-Key": "unused-fallback-key"}, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Empty(t, upstream.requests)

	// A multi-audience token carrying both audiences succeeds at both endpoints.
	gwResp, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, bothAudiences, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, gwResp.Code, gwResp.Body.String())

	w = postCompletionRequest(router, projectPath, bothAudiences, nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, upstream.requests, 1)
}

func TestDirectWorkloadBearer_CrossProjectRejection(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	ctx := t.Context()

	// Replace the default organization-tier fixture issuer/admission with project-X-scoped ones
	// so project Y in the same organization does not admit this workload.
	projectX := f.fx.target.ProjectID
	projectY, err := projectsrepo.New(f.ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-project-" + uuid.NewString()[:8], OrganizationID: f.fx.orgID,
	})
	require.NoError(t, err)

	softDeleteWorkloadIssuer(t, ctx, f.ti, f.fx.orgID, f.issuerID)
	unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)

	fixtures := testrepo.New(f.ti.conn)
	projectIssuerID, err := fixtures.CreateWorkloadIssuerFixture(ctx, testrepo.CreateWorkloadIssuerFixtureParams{
		OrganizationID: f.fx.orgID,
		ProjectID:      uuid.NullUUID{UUID: projectX, Valid: true},
		Name:           "project-x-issuer",
		Issuer:         f.issuer.OAuth21URL,
		JwksUri:        oauthtest.DiscoverWorkloadJWKSURI(t, f.issuer),
	})
	require.NoError(t, err)
	f.issuerID = projectIssuerID
	f.setAssertionReuse(t, true)
	require.NoError(t, fixtures.CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{UUID: projectX, Valid: true},
		WorkloadIssuerID: projectIssuerID, Subject: f.subject,
	}))
	f.assignmentID = assignAgentToWorkload(t, ctx, f.ti, f.fx.orgID, projectIssuerID, f.subject, f.agentID)

	gateway, _, _ := seedDirectWorkloadProjectGateway(t, f)
	enableWorkloadInference(t, f)
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), projectY.ID)
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), projectY.ID)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)

	token := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)

	// Valid on project X gateway and project X inference.
	gwX, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, gwX.Code, gwX.Body.String())

	infX := postCompletionRequest(router, "/projects/"+projectX.String()+"/chat/completions", token, nil, "")
	require.Equal(t, http.StatusOK, infX.Code, infX.Body.String())
	require.Len(t, upstream.requests, 1)

	// Rejected on project Y gateway and project Y inference.
	_, err = serveProjectGatewayHTTP(t, f.ti, gateway, projectY.Slug, token, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	infY := postCompletionRequest(router, "/projects/"+projectY.ID.String()+"/chat/completions", token, nil, "")
	require.Equal(t, http.StatusUnauthorized, infY.Code, infY.Body.String())
	require.Len(t, upstream.requests, 1)
}

func TestDirectWorkloadBearer_NonAdmittedSubjectRejected(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	gateway, _, _ := seedDirectWorkloadProjectGateway(t, f)
	enableWorkloadInference(t, f)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	// Non-admitted subjects (including worker-for-actor/*) are rejected naturally by exact-subject admission.
	for _, subject := range []string{"worker-for-actor/actor-123", "sandbox/other-runtime"} {
		t.Run(subject, func(t *testing.T) {
			t.Parallel()
			token := f.directAssertion(t, subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)
			gwResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
			requireOopsCode(t, err, oops.CodeUnauthorized)
			require.Contains(t, gwResp.Header().Get("WWW-Authenticate"), "Bearer resource_metadata=")

			w := postCompletionRequest(router, projectPath, token, nil, "")
			require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		})
	}
	require.Empty(t, upstream.requests)
}

func TestDirectWorkloadBearer_SingleUseIssuerRefused(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	// Fixture issuers default to allow_assertion_reuse = false.
	gateway, _, _ := seedDirectWorkloadProjectGateway(t, f)
	enableWorkloadInference(t, f)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	now := time.Now()
	withoutJTI := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", jwt.Claims{
		Issuer:    f.issuer.OAuth21URL,
		Subject:   f.subject,
		Audience:  jwt.Audience{mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(10 * time.Minute)),
	})
	withJTI := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)

	for _, token := range []string{withJTI, withoutJTI} {
		_, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
		requireOopsCode(t, err, oops.CodeUnauthorized)

		w := postCompletionRequest(router, projectPath, token, nil, "")
		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	}
	require.Empty(t, upstream.requests)
}

func TestDirectWorkloadBearer_Stage1CacheAndRevocationBounds(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	gateway, grantedID, _ := seedDirectWorkloadProjectGateway(t, f)
	agentGrant, ownerGrant := enableWorkloadInference(t, f)

	baseTime := time.Now()
	var currentTime atomic.Int64
	currentTime.Store(baseTime.UnixNano())
	f.ti.service.SetDirectWorkloadClock(func() time.Time { return time.Unix(0, currentTime.Load()) })

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	token := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)

	// Populate Stage-1 cache for both endpoints.
	gwResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, gwResp.Code)
	w := postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusOK, w.Code)

	// Stage-2 revocations take effect immediately (0 s) even while Stage-1 cache is warm:
	// 1. Revoking inference:execute grant blocks inference immediately.
	_, err = accessrepo.New(f.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: agentGrant, OrganizationID: f.fx.orgID})
	require.NoError(t, err)
	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), f.fx.target.ProjectID)

	_, err = accessrepo.New(f.ti.conn).DeletePrincipalGrant(t.Context(), accessrepo.DeletePrincipalGrantParams{ID: ownerGrant, OrganizationID: f.fx.orgID})
	require.NoError(t, err)
	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID), f.fx.target.ProjectID)

	// 2. Suspending the agent blocks both endpoints immediately.
	_, err = agentsrepo.New(f.ti.conn).SuspendAgent(t.Context(), agentsrepo.SuspendAgentParams{OrganizationID: f.fx.orgID, ID: f.agentID})
	require.NoError(t, err)
	_, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// Re-activate agent via fresh agent assignment so we can test Stage-1 admission withdrawal + TTL expiry.
	newAgent := createConsentAgent(t, f.ctx, f.ti, f.fx, "Replacement direct workload agent")
	seedPrincipalMCPConnectGrant(t, f.ctx, f.ti, f.fx.orgID, urn.NewPrincipal(urn.PrincipalTypeAgent, newAgent.ID.String()), grantedID)
	seedWorkloadInferenceGrant(t, f, urn.NewPrincipal(urn.PrincipalTypeAgent, newAgent.ID.String()), f.fx.target.ProjectID)
	unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
	f.assignmentID = assignAgentToWorkload(t, f.ctx, f.ti, f.fx.orgID, f.issuerID, f.subject, newAgent.ID)

	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// 3. Stage-1 cache hit within 5 s, then rejected once clock advances past the 5 s cache TTL.
	rows, err := testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(t.Context(), testrepo.SoftDeleteWorkloadAdmissionFixtureParams{
		OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)

	// Still cached at +3 s.
	currentTime.Store(baseTime.Add(3 * time.Second).UnixNano())
	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusOK, w.Code, "Stage-1 cache must serve warm hits within the 5 s window")

	// Evicted at +6 s -> re-checks admission -> rejected.
	currentTime.Store(baseTime.Add(6 * time.Second).UnixNano())
	w = postCompletionRequest(router, projectPath, token, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, "withdrawn admission must be rejected after Stage-1 cache TTL expires")
	_, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, token, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)

	// 4. Cache entry is bounded by token expiry when exp < 5 s cache TTL.
	require.NoError(t, testrepo.New(f.ti.conn).CreateWorkloadIdentityAdmissionFixture(t.Context(), testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID: f.fx.orgID, ProjectID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, WorkloadIssuerID: f.issuerID, Subject: f.subject,
	}))
	shortNow := time.Now()
	currentTime.Store(shortNow.UnixNano())
	shortExpToken := oauthtest.MintWorkloadAssertion(t, f.issuer, "JWT", jwt.Claims{
		Issuer:    f.issuer.OAuth21URL,
		Subject:   f.subject,
		Audience:  jwt.Audience{mcp.DirectWorkloadInferenceAudience},
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(shortNow),
		NotBefore: jwt.NewNumericDate(shortNow),
		Expiry:    jwt.NewNumericDate(shortNow.Add(2 * time.Second)),
	})
	w = postCompletionRequest(router, projectPath, shortExpToken, nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	// Withdraw admission and advance past the 2 s token expiry (still under the 5 s cache TTL):
	// cache must evict at exp rather than keeping the entry alive for 5 s.
	_, err = testrepo.New(f.ti.conn).SoftDeleteWorkloadAdmissionFixture(t.Context(), testrepo.SoftDeleteWorkloadAdmissionFixtureParams{
		OrganizationID: f.fx.orgID, WorkloadIssuerID: f.issuerID, Subject: f.subject,
	})
	require.NoError(t, err)
	currentTime.Store(shortNow.Add(3 * time.Second).UnixNano())
	w = postCompletionRequest(router, projectPath, shortExpToken, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, "Stage-1 cache entry must not outlive token exp")
}

func TestDirectWorkloadBearer_TokenTypeConfusionAndNeighbouringCredentials(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	gateway, grantedID, _ := seedDirectWorkloadProjectGateway(t, f)
	enableWorkloadInference(t, f)

	upstream := &workloadInferenceCompletionClient{}
	router := newDirectWorkloadChatMux(t, f, upstream)
	projectPath := "/projects/" + f.fx.target.ProjectID.String() + "/chat/completions"

	directToken := f.directAssertion(t, f.subject, mcp.DirectWorkloadProjectGatewayAudience, mcp.DirectWorkloadInferenceAudience)

	// 1. Direct workload JWT is rejected on org-wide gateway and direct MCP server (with standard 401 + challenge).
	orgResp, err := serveOrganizationGatewayHTTP(t, t.Context(), f.ti, gateway.organizationSlug, directToken, makeInitializeBody())
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, orgResp.Header().Get("WWW-Authenticate"), "Bearer resource_metadata=")

	directEndpointResp := httptest.NewRecorder()
	_, _, _, err = f.ti.service.ApplyIssuerGate(t.Context(), directEndpointResp, directToken, f.ti.serverURL.String(), workloadSessionEndpoint(f.fx))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	require.Contains(t, directEndpointResp.Header().Get("WWW-Authenticate"), "Bearer resource_metadata=")

	// 2. Speakeasy device-agent RS256 token cannot be confused with a direct workload bearer.
	deviceIssuer := deviceagenttoken.New(mcpauthztest.NewIssuer(t), f.ti.conn)
	deviceToken, _, err := deviceIssuer.Mint(f.fx.orgID, urn.NewWorkloadPrincipal(f.issuerID, f.subject), time.Now())
	require.NoError(t, err)
	_, err = serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, deviceToken, listServersBody(t))
	requireOopsCode(t, err, oops.CodeUnauthorized)
	w := postCompletionRequest(router, projectPath, deviceToken, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// 3. Neighbouring credential types stay unaffected at the Project Gateway:
	//    - user session JWT (HS256)
	userToken := mintProjectGatewayBearer(t, f.ti, gateway, gateway.projectSlug)
	userResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, userToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, userResp.Code, userResp.Body.String())

	//    - agent API key (gram_...)
	agentKey := mintAgentGatewayKey(t, f.ctx, f.ti, f.agentID, grantedID)
	agentKeyResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, agentKey, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, agentKeyResp.Code, agentKeyResp.Body.String())

	//    - principal credential (RS256 with typ: gram-principal-credential+jwt)
	projectRead, err := authz.NewSelector(authz.ScopeProjectRead, f.fx.target.ProjectID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.fx.orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, f.fx.userID),
		Scope: string(authz.ScopeProjectRead), Selectors: projectRead,
	})
	require.NoError(t, err)
	principalToken, _, err := f.ti.principalCredentials.Mint(principalcredential.Credential{
		OrganizationID: f.fx.orgID, ProjectID: f.fx.target.ProjectID,
		Principal: urn.NewPrincipal(urn.PrincipalTypeAgent, f.agentID.String()), AuthorizerUserID: f.fx.userID,
		SelectionProfileVersionID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	principalResp, err := serveProjectGatewayHTTP(t, f.ti, gateway, gateway.projectSlug, principalToken, listServersBody(t))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, principalResp.Code, principalResp.Body.String())

	// 4. Invalid project UUID on project-scoped inference route returns 401 without fallback.
	w = postCompletionRequest(router, "/projects/not-a-uuid/chat/completions", directToken, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())

	// 5. Exchanged Speakeasy inference session works on its own project-scoped route and is rejected on another project's route.
	sharedIssuer := f.sharedIssuer(t)
	exchangedToken := requireWorkloadInferenceToken(t, f, sharedIssuer)
	w = postCompletionRequest(router, projectPath, exchangedToken, nil, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = postCompletionRequest(router, "/projects/"+uuid.NewString()+"/chat/completions", exchangedToken, nil, "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}
