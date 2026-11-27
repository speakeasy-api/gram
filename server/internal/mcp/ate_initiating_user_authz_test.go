package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	authclientfixtures "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/userblock"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type testATEAuthorizerAdapter struct {
	resolver *assistants.ATEWorkloadAuthorizer
}

func (a *testATEAuthorizerAdapter) ResolveWorkloadAuthorizer(ctx context.Context, req mcp.WorkloadAuthorizerRequest) (mcp.WorkloadAuthorizerResolution, error) {
	res, err := a.resolver.Resolve(ctx, assistants.ATEWorkloadAuthorizerRequest{
		OrganizationID: req.OrganizationID,
		ProjectID:      req.ProjectID,
		IssuerID:       req.IssuerID,
		Subject:        req.Subject,
		Claims:         req.Claims,
	})
	if err != nil {
		if errors.Is(err, assistants.ErrATEWorkloadAuthorizerRejected) {
			return mcp.WorkloadAuthorizerResolution{}, fmt.Errorf("%w: %w", mcp.ErrWorkloadAuthorizerRejected, err)
		}
		return mcp.WorkloadAuthorizerResolution{}, fmt.Errorf("resolve ATE workload authorizer: %w", err)
	}
	return mcp.WorkloadAuthorizerResolution{
		Bound:            res.Bound,
		AuthorizerUserID: res.AuthorizerUserID,
		AssistantID:      res.AssistantID,
		ThreadID:         res.ThreadID,
		ChatID:           res.ChatID,
	}, nil
}

type ateActorTestFixture struct {
	f           workloadGrantFixture
	projectID   uuid.UUID
	orgID       string
	ownerUserID string
	assistantID uuid.UUID
	runtimeID   uuid.UUID
	identity    assistantidentity.RuntimeIdentity
	gateway     organizationGatewayFixture
	atespace    string
}

type seededATEThreadActor struct {
	threadID     uuid.UUID
	chatID       uuid.UUID
	actorRowID   uuid.UUID
	actorName    string
	actorUID     string
	subject      string
	acpSessionID string
}

func newATEActorAuthzFixture(t *testing.T) *ateActorTestFixture {
	t.Helper()
	ctx := t.Context()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	f.ti.service.SetWorkloadAuthorizerResolver(&testATEAuthorizerAdapter{
		resolver: assistants.NewATEWorkloadAuthorizer(f.ti.conn),
	})

	projectID := f.fx.target.ProjectID
	orgID := f.fx.orgID
	ownerUserID := f.fx.userID
	q := assistantrepo.New(f.ti.conn)

	asst, err := q.CreateAssistant(ctx, assistantrepo.CreateAssistantParams{
		ProjectID:       projectID,
		OrganizationID:  orgID,
		CreatedByUserID: conv.ToPGText(ownerUserID),
		Name:            "ATE initiating-user authz assistant",
		Model:           "test/model",
		WarmTtlSeconds:  300,
		MaxConcurrency:  4,
		Status:          "active",
	})
	require.NoError(t, err)
	_, err = triggerrepo.New(f.ti.conn).CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		DefinitionSlug: "manual",
		Name:           "ATE manual root",
		TargetKind:     "assistant",
		TargetRef:      asst.ID.String(),
		TargetDisplay:  asst.Name,
		ConfigJson:     []byte(`{}`),
		Status:         "active",
	})
	require.NoError(t, err)

	identities := assistantidentity.New(f.ti.serverURL.String(), f.ti.audit)
	tx := testenv.BeginTx(t, ctx, f.ti.conn)
	require.NoError(t, identities.Provision(ctx, tx, assistantidentity.ProvisionParams{
		OrganizationID: orgID,
		ProjectID:      projectID,
		AssistantID:    asst.ID,
		ActorUserID:    ownerUserID,
	}))
	require.NoError(t, tx.Commit(ctx))
	identity, err := identities.ResolveRuntime(ctx, f.ti.conn, orgID, projectID, asst.ID)
	require.NoError(t, err)

	// Re-point the fixture's agent to the assistant's backing agent.
	unassignWorkloadAgent(t, ctx, f.ti, f.assignmentID)
	f.agentID = identity.AgentID

	require.NoError(t, q.SetAssistantDesiredRuntimeVersionForTest(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionForTestParams{
		ProjectID:      projectID,
		AssistantID:    asst.ID,
		RuntimeVersion: 3,
	}))

	// Seed an initial placeholder thread to reserve the live ATE runtime row.
	seedChatID := uuid.New()
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID: seedChatID, ProjectID: projectID, OrganizationID: orgID, UserID: conv.ToPGText(ownerUserID),
	}))
	seedThreadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID: asst.ID, ProjectID: projectID, CorrelationID: "ate-seed-" + uuid.NewString()[:8],
		ChatID: seedChatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)
	require.NoError(t, q.ReserveAssistantRuntimeV2(ctx, assistantrepo.ReserveAssistantRuntimeV2Params{
		ProjectID: projectID, AssistantID: asst.ID, AssistantThreadID: seedThreadID, Backend: "ate", State: "active",
	}))
	rt, err := q.GetAssistantRuntimeV2(ctx, assistantrepo.GetAssistantRuntimeV2Params{
		ProjectID: projectID, AssistantID: asst.ID,
	})
	require.NoError(t, err)

	gateway := seedOrganizationGateway(t, f.ctx, f.ti)
	f.ti.features.SetFlag(feature.FlagProjectGatewayMachineAccess, orgID, true)
	authCtx, ok := contextvalues.GetAuthContext(f.ctx)
	require.True(t, ok)
	grantProjectRead(t, f.ti, authCtx)

	return &ateActorTestFixture{
		f:           f,
		projectID:   projectID,
		orgID:       orgID,
		ownerUserID: ownerUserID,
		assistantID: asst.ID,
		runtimeID:   rt.ID,
		identity:    identity,
		gateway:     gateway,
		atespace:    "prod-assistants",
	}
}

func (fx *ateActorTestFixture) seedOrgMember(t *testing.T, label string, grantProjectAccess, grantInference bool) string {
	t.Helper()
	ctx := t.Context()
	userID := "user_" + label + "_" + uuid.NewString()[:8]
	tq := testrepo.New(fx.f.ti.conn)
	require.NoError(t, tq.InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID: userID, Email: userID + "@example.com", DisplayName: label,
	}))
	require.NoError(t, tq.CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: fx.orgID, UserID: pgtype.Text{String: userID, Valid: true},
	}))
	if grantProjectAccess {
		sel, err := authz.NewSelector(authz.ScopeProjectRead, fx.projectID.String()).MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(fx.f.ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: fx.orgID,
			PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, userID),
			Scope:          string(authz.ScopeProjectRead),
			Selectors:      sel,
		})
		require.NoError(t, err)
	}
	if grantInference {
		seedWorkloadInferenceGrant(t, fx.f, urn.NewPrincipal(urn.PrincipalTypeUser, userID), fx.projectID)
	}
	return userID
}

func (fx *ateActorTestFixture) seedRemoteMemberServer(t *testing.T, slug string, toolName string) (uuid.UUID, *recordingUpstream) {
	t.Helper()
	ctx := t.Context()
	upstream := newRecordingUpstream(t, toolName)
	remote := remotemcptest.SeedServer(t, ctx, fx.f.ti.conn, remotemcp_repo.CreateServerParams{
		ProjectID: fx.projectID, TransportType: "streamable-http", Url: upstream.url,
	})
	memberIssuerID := createUserSessionIssuer(t, ctx, fx.f.ti.conn, fx.projectID)
	serverID := uuid.New()
	require.NoError(t, authclientfixtures.New(fx.f.ti.conn).SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID:                serverID,
		ProjectID:         fx.projectID,
		Slug:              conv.ToPGText(slug),
		RemoteMcpServerID: conv.ToNullUUID(remote.ID),
	}))
	_, err := mcpserversrepo.New(fx.f.ti.conn).UpdateMCPServer(ctx, mcpserversrepo.UpdateMCPServerParams{
		ID:                  serverID,
		ProjectID:           fx.projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		RemoteMcpServerID:   conv.ToNullUUID(remote.ID),
		UserSessionIssuerID: conv.ToNullUUID(memberIssuerID),
		Visibility:          mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return serverID, upstream
}

func (fx *ateActorTestFixture) seedThreadActor(t *testing.T, label, initiatingUserID, resolution string) seededATEThreadActor {
	t.Helper()
	ctx := t.Context()
	q := assistantrepo.New(fx.f.ti.conn)

	chatID := uuid.New()
	chatOwner := initiatingUserID
	if chatOwner == "" {
		chatOwner = fx.ownerUserID
	}
	require.NoError(t, q.UpsertAssistantChat(ctx, assistantrepo.UpsertAssistantChatParams{
		ChatID: chatID, ProjectID: fx.projectID, OrganizationID: fx.orgID, UserID: conv.ToPGText(chatOwner),
	}))
	threadID, err := q.UpsertAssistantThread(ctx, assistantrepo.UpsertAssistantThreadParams{
		AssistantID: fx.assistantID, ProjectID: fx.projectID,
		CorrelationID: "ate-" + label + "-" + uuid.NewString()[:8],
		ChatID:        chatID, SourceKind: "dashboard", SourceRefJson: []byte(`{}`),
	})
	require.NoError(t, err)

	actorName := "spk-kit-" + label + "-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	actorUID := "uid-" + label + "-" + uuid.NewString()
	subject := "actor/" + fx.atespace + "/" + actorName

	require.NoError(t, testrepo.New(fx.f.ti.conn).CreateWorkloadIdentityAdmissionFixture(ctx, testrepo.CreateWorkloadIdentityAdmissionFixtureParams{
		OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true},
		WorkloadIssuerID: fx.f.issuerID, Subject: subject,
	}))
	assignAgentToWorkload(t, ctx, fx.f.ti, fx.orgID, fx.f.issuerID, subject, fx.f.agentID)

	bindingJSON, err := json.Marshal(map[string]any{
		"issuer_id":           fx.f.issuerID,
		"subject":             subject,
		"identity":            fx.identity,
		"mcp_project_gateway": true,
	})
	require.NoError(t, err)

	row, err := q.InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
		ActorName:                actorName,
		ActorUid:                 actorUID,
		State:                    "running",
		Configured:               true,
		WorkloadBindingJson:      bindingJSON,
		WarmUntil:                pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
		InitiatingUserID:         conv.ToPGTextEmpty(initiatingUserID),
		InitiatingUserResolution: resolution,
		RuntimeID:                fx.runtimeID,
		ProjectID:                fx.projectID,
		AssistantID:              fx.assistantID,
		ThreadID:                 threadID,
	})
	require.NoError(t, err)

	acpSessionID := "ate-acp-" + label + "-" + uuid.NewString()[:8]
	_, err = q.InsertKitRuntimeSession(ctx, assistantrepo.InsertKitRuntimeSessionParams{
		SessionID: acpSessionID, RuntimeID: fx.runtimeID, ProjectID: fx.projectID,
		AssistantID: fx.assistantID, ThreadID: threadID,
	})
	require.NoError(t, err)

	return seededATEThreadActor{
		threadID:     threadID,
		chatID:       chatID,
		actorRowID:   row.ID,
		actorName:    actorName,
		actorUID:     actorUID,
		subject:      subject,
		acpSessionID: acpSessionID,
	}
}

func (fx *ateActorTestFixture) mintATEJWT(t *testing.T, subject string, audiences []string, extraClaims map[string]any) string {
	t.Helper()
	now := time.Now()
	std := jwt.Claims{
		Issuer:    fx.f.issuer.OAuth21URL,
		Subject:   subject,
		Audience:  jwt.Audience(audiences),
		ID:        uuid.NewString(),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		Expiry:    jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: fx.f.issuer.SigningKey()},
		(&jose.SignerOptions{}).WithType(jose.ContentType("JWT")).WithHeader(jose.HeaderKey("kid"), fx.f.issuer.KeyID()),
	)
	require.NoError(t, err)
	builder := jwt.Signed(signer).Claims(std)
	if len(extraClaims) > 0 {
		builder = builder.Claims(extraClaims)
	}
	raw, err := builder.Serialize()
	require.NoError(t, err)
	return raw
}

func (fx *ateActorTestFixture) mintActorToken(t *testing.T, actor seededATEThreadActor, audiences ...string) string {
	t.Helper()
	return fx.mintATEJWT(t, actor.subject, audiences, map[string]any{
		"actor.ate.dev": map[string]string{
			"atespace":  fx.atespace,
			"actorName": actor.actorName,
			"actorUID":  actor.actorUID,
		},
	})
}

func serveProjectGatewayHTTPWithOptions(
	t *testing.T,
	ti *testInstance,
	gateway organizationGatewayFixture,
	token string,
	rawBody []byte,
	query string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	path := "/mcp/gateway/" + gateway.organizationSlug + "/projects/" + gateway.projectSlug
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(rawBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rc := chi.NewRouteContext()
	rc.URLParams.Add("mcpSlug", gateway.organizationSlug)
	rc.URLParams.Add("projectSlug", gateway.projectSlug)
	req = req.WithContext(context.WithValue(t.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	oops.ErrHandle(ti.logger, ti.service.ServeOrganizationGateway).ServeHTTP(rec, req)
	return rec
}

func TestATEInitiatingUserAuthz_FourWayIntersectionPerThreadIsolationAndLiveRecheck(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fx := newATEActorAuthzFixture(t)
	fx.f.ti.service.SetAuthClientPermitted(true)

	// Freeze the Stage-1 cache clock so every revocation test proves Stage-2
	// re-evaluates fresh inside the 5-second Stage-1 cache window.
	frozenNow := time.Now()
	fx.f.ti.service.SetDirectWorkloadClock(func() time.Time { return frozenNow })

	// Create two distinct users under the same organization and project.
	userA := fx.seedOrgMember(t, "alice", true, true)
	userB := fx.seedOrgMember(t, "bob", true, true)

	// Seed 3 private MCP servers:
	//  - serverA: granted to agent + owner + userA (NOT userB)
	//  - serverB: granted to agent + owner + userB (NOT userA)
	//  - serverOwnerOnly: granted to agent + owner (NEITHER userA NOR userB)
	slugA := "svc-alice-" + uuid.NewString()[:6]
	slugB := "svc-bob-" + uuid.NewString()[:6]
	slugOwnerOnly := "svc-owner-" + uuid.NewString()[:6]
	serverA, _ := fx.seedRemoteMemberServer(t, slugA, "ping_a")
	serverB, _ := fx.seedRemoteMemberServer(t, slugB, "ping_b")
	serverOwnerOnly, _ := fx.seedRemoteMemberServer(t, slugOwnerOnly, "ping_owner")

	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, fx.f.agentID.String())
	for _, srvID := range []uuid.UUID{serverA, serverB, serverOwnerOnly} {
		seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, fx.ownerUserID, srvID.String())
		seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, agentURN, srvID)
	}
	grantA := seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, urn.NewPrincipal(urn.PrincipalTypeUser, userA), serverA)
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userB, serverB.String())

	// Enable inference on agent + owner too.
	enableWorkloadInference(t, fx.f)

	// Seed 4 threads on the SAME assistant + SAME runtime:
	//  - threadA: human User A
	//  - threadB: human User B
	//  - threadNoUser: explicit "no_user"
	//  - threadUnrecorded: "not_recorded"
	actorA := fx.seedThreadActor(t, "alice", userA, assistants.ATEInitiatorDashboardSender)
	actorB := fx.seedThreadActor(t, "bob", userB, assistants.ATEInitiatorSlackMapping)
	actorNoUser := fx.seedThreadActor(t, "cron", "", assistants.ATEInitiatorNoUser)
	actorUnrecorded := fx.seedThreadActor(t, "unrecorded", "", assistants.ATEInitiatorNotRecorded)

	tokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadProjectGatewayAudience)
	tokenB := fx.mintActorToken(t, actorB, mcp.DirectWorkloadProjectGatewayAudience)
	tokenNoUser := fx.mintActorToken(t, actorNoUser, mcp.DirectWorkloadProjectGatewayAudience)
	tokenUnrecorded := fx.mintActorToken(t, actorUnrecorded, mcp.DirectWorkloadProjectGatewayAudience)

	// 1. Thread A sees serverA ONLY (denied serverB and serverOwnerOnly even though agent+owner have them).
	listedA := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), "", nil)
	require.Equal(t, http.StatusOK, listedA.Code, listedA.Body.String())
	require.Contains(t, listedA.Body.String(), slugA)
	require.NotContains(t, listedA.Body.String(), slugB)
	require.NotContains(t, listedA.Body.String(), slugOwnerOnly)

	// Direct execute_tool call to serverOwnerOnly as Thread A must be denied.
	callOwnerOnly := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": slugOwnerOnly + "--ping_owner", "arguments": map[string]any{}},
	})
	respOwnerOnly := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, callOwnerOnly, "", nil)
	require.Equal(t, http.StatusOK, respOwnerOnly.Code, respOwnerOnly.Body.String())
	ownerOnlyRPC := decodeRPCResponse(t, respOwnerOnly)
	require.Nil(t, ownerOnlyRPC["result"], "initiating user without server grant must be denied even when agent and owner have grants")
	require.NotNil(t, ownerOnlyRPC["error"], "expected JSON-RPC error denying access to owner-only server")

	// Direct execute_tool call to serverA as Thread A succeeds.
	callA := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": slugA + "--ping_a", "arguments": map[string]any{}},
	})
	respA := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, callA, "", nil)
	require.Equal(t, http.StatusOK, respA.Code, respA.Body.String())
	textA, isErrA := metaToolResultText(t, decodeRPCResponse(t, respA))
	require.False(t, isErrA, textA)
	require.Contains(t, textA, "pong from ping_a")

	// 2. Thread B on the SAME assistant sees serverB ONLY (not serverA, not serverOwnerOnly).
	listedB := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenB, listServersBody(t), "", nil)
	require.Equal(t, http.StatusOK, listedB.Code, listedB.Body.String())
	require.Contains(t, listedB.Body.String(), slugB)
	require.NotContains(t, listedB.Body.String(), slugA)
	require.NotContains(t, listedB.Body.String(), slugOwnerOnly)

	// 3. Explicit "no_user" thread keeps userless workload authority (sees all 3 agent+owner servers).
	listedNoUser := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenNoUser, listServersBody(t), "", nil)
	require.Equal(t, http.StatusOK, listedNoUser.Code, listedNoUser.Body.String())
	require.Contains(t, listedNoUser.Body.String(), slugA)
	require.Contains(t, listedNoUser.Body.String(), slugB)
	require.Contains(t, listedNoUser.Body.String(), slugOwnerOnly)

	// 4. Unrecorded "not_recorded" row fails closed (401 Unauthorized) on BOTH gateway and inference.
	listedUnrecorded := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenUnrecorded, listServersBody(t), "", nil)
	require.Equal(t, http.StatusUnauthorized, listedUnrecorded.Code, listedUnrecorded.Body.String())

	// 5. Managed Kit inference succeeds for User A and records canonical Thread A / Chat A.
	var inferenceCalls atomic.Int32
	completionClient := &managedInferenceCompletion{complete: func(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
		inferenceCalls.Add(1)
		require.Equal(t, billing.ModelUsageSourceAssistants, req.UsageSource)
		require.Equal(t, actorA.chatID, req.ChatID)
		p, ok := contextvalues.GetAssistantPrincipal(ctx)
		require.True(t, ok)
		require.Equal(t, fx.assistantID, p.AssistantID)
		require.Equal(t, actorA.threadID, p.ThreadID)
		cred, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
		require.True(t, ok)
		require.Equal(t, userA, cred.AuthorizerUserID)
		// Verify telemetry actor attribution records agent as actor and initiating user as authorizer.
		attrs := tm.HTTPLogAttributes{}
		attrs.RecordAuthenticatedActor(ctx)
		require.Equal(t, "agent", attrs["gram.authorization.actor.type"])
		require.Equal(t, fx.f.agentID.String(), attrs["gram.authorization.actor.id"])
		require.Equal(t, userA, attrs["gram.authorization.authorizer_user_id"])
		msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
		return &openrouter.CompletionResponse{Message: &msg, Model: "test/model"}, nil
	}}
	chatRouter := newDirectWorkloadChatMux(t, fx.f, completionClient)
	infPath := "/projects/" + fx.projectID.String() + "/chat/completions"
	infTokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadInferenceAudience)
	infTokenUnrecorded := fx.mintActorToken(t, actorUnrecorded, mcp.DirectWorkloadInferenceAudience)

	infBodyA := fmt.Sprintf(`{"model":"test-model","user":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, actorA.acpSessionID)
	infResp := postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA)
	require.Equal(t, http.StatusOK, infResp.Code, infResp.Body.String())
	require.EqualValues(t, 1, inferenceCalls.Load())

	infRespUnrecorded := postCompletionRequest(chatRouter, infPath, infTokenUnrecorded, nil, infBodyA)
	require.Equal(t, http.StatusUnauthorized, infRespUnrecorded.Code, infRespUnrecorded.Body.String())

	// 6. Live per-request recheck INSIDE the 5s Stage-1 cache window (clock still frozen):
	//    (a) Revoking User A's mcp:connect grant on serverA takes effect immediately.
	_, err := accessrepo.New(fx.f.ti.conn).DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
		OrganizationID: fx.orgID, ID: grantA,
	})
	require.NoError(t, err)
	listedAfterRevoke := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), "", nil)
	require.Equal(t, http.StatusOK, listedAfterRevoke.Code, listedAfterRevoke.Body.String())
	require.NotContains(t, listedAfterRevoke.Body.String(), slugA, "revoked user grant must take effect immediately even inside the 5s Stage-1 cache window")

	//    (b) Removing User A from the organization takes effect immediately on BOTH
	//        Project Gateway AND inference (401 refusal, never falling back to userless!).
	require.NoError(t, testrepo.New(fx.f.ti.conn).DeleteOrganizationUserRelationshipFixture(ctx, testrepo.DeleteOrganizationUserRelationshipFixtureParams{
		OrganizationID: fx.orgID, UserID: conv.ToPGText(userA),
	}))
	listedAfterRemove := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), "", nil)
	require.Equal(t, http.StatusUnauthorized, listedAfterRemove.Code, "removed initiating user must be refused immediately without falling back to userless")

	infAfterRemove := postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA)
	require.Equal(t, http.StatusUnauthorized, infAfterRemove.Code, "removed initiating user must be refused on inference immediately")
}

func TestATEInitiatingUserAuthz_PersonalUpstreamConnectionsAndRecoveryLinkBoundToInitiator(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fx := newATEActorAuthzFixture(t)
	fx.f.ti.service.SetAuthClientPermitted(true)

	userA := fx.seedOrgMember(t, "alice-personal", true, false)
	userB := fx.seedOrgMember(t, "bob-personal", true, false)

	// Upstream MCP server records the Authorization header it receives and answers
	// 401 when no valid personal token is presented, proxying authorized calls to a
	// real streamable-HTTP MCP server.
	mockMCP := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{Tools: []testmcp.Tool{{
		Name:        "list_issues",
		Description: "lists issues",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Response: testmcp.ToolResponse{
			Content: []map[string]any{{"type": "text", "text": "alice-issues-ok"}},
		},
	}}})
	t.Cleanup(mockMCP.Close)
	mockTarget, err := url.Parse(mockMCP.URL)
	require.NoError(t, err)
	mockProxy := httputil.NewSingleHostReverseProxy(mockTarget)

	var receivedAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		receivedAuth.Store(auth)
		if auth != "Bearer alice-personal-secret-token" {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mockProxy.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)

	asBase := "https://ate-personal-" + uuid.NewString()[:6] + ".example.com"
	registration := createConsentRemoteClientWithCallback(t, ctx, fx.f.ti.conn, fx.projectID, fx.orgID, "ate-pers-"+uuid.NewString()[:6], asBase, "")

	fixtures := authclientfixtures.New(fx.f.ti.conn)
	remoteID, serverID := uuid.New(), uuid.New()
	memberSlug := "linear-pers-" + serverID.String()[:6]
	require.NoError(t, fixtures.SeedRemoteMCPServerFixture(ctx, authclientfixtures.SeedRemoteMCPServerFixtureParams{
		ID: remoteID, ProjectID: fx.projectID, Url: upstream.URL,
	}))
	require.NoError(t, fixtures.SeedMCPServerFixture(ctx, authclientfixtures.SeedMCPServerFixtureParams{
		ID: serverID, ProjectID: fx.projectID, Slug: pgtype.Text{String: memberSlug, Valid: true},
		RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true},
	}))
	authClientID, err := fixtures.SeedAuthClientFixture(ctx, authclientfixtures.SeedAuthClientFixtureParams{
		OrganizationID: fx.orgID, ProjectID: uuid.NullUUID{UUID: fx.projectID, Valid: true},
		ClientType: "remote_session", Source: "admin", RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
	})
	require.NoError(t, err)
	require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientfixtures.SeedServerAuthClientLinkFixtureParams{
		ProjectID: fx.projectID, OrganizationID: fx.orgID, McpServerID: serverID, AuthClientID: authClientID, Source: "admin",
	}))
	associateServerRegistration(t, ctx, fx.f.ti.conn, fx.orgID, fx.projectID, serverID, registration)

	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, fx.f.agentID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, fx.ownerUserID, serverID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userA, serverID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userB, serverID.String())
	seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, agentURN, serverID)

	// Give ONLY userB (and the owner!) a stored resource-qualified personal remote session — NOT userA.
	for _, entry := range []struct {
		userID string
		tok    string
	}{
		{userID: userB, tok: "bob-personal-secret-token"},
		{userID: fx.ownerUserID, tok: "owner-personal-secret-token"},
	} {
		insertQualifiedRemoteSessionToken(
			t, ctx, fx.f.ti, fx.gateway.issuerID, registration,
			urn.NewUserSubject(entry.userID), entry.tok, upstream.URL,
		)
	}

	actorA := fx.seedThreadActor(t, "alice-pers", userA, assistants.ATEInitiatorDashboardSender)
	tokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadProjectGatewayAudience)

	// Configure browser session auth for verifying the OAuth recovery link binding.
	fx.f.ti.service.SetBrowserSession(func(ctx context.Context, cookie string) (context.Context, error) {
		if cookie == "" {
			return ctx, oops.C(oops.CodeUnauthorized)
		}
		return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: "", UserID: cookie}), nil
	})
	router := chi.NewRouter()
	router.Get(mcp.ConnectionRecoveryRoute, oops.BrowserErrHandle(fx.f.ti.logger, fx.f.ti.service.HandleConnectionRecovery).ServeHTTP)
	openRecovery := func(path, browserUser string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if browserUser != "" {
			req.AddCookie(&http.Cookie{Name: constants.SessionCookie, Value: browserUser})
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// 1. Thread A calls server without User A having a personal connection:
	//    MUST NOT use User B's or the owner's personal token; MUST return a
	//    connection recovery link bound strictly to User A.
	callBody := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": memberSlug + "--list_issues", "arguments": map[string]any{}},
	})
	resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, callBody, "", nil)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	text, isErr := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.True(t, isErr)
	require.Contains(t, text, "Connection required: the person this turn acts for needs to connect their own account")
	gotAuth, _ := receivedAuth.Load().(string)
	require.Empty(t, gotAuth, "upstream must not receive owner or User B credentials")

	recoveryPath := extractRecoveryPath(t, fx.f.ti.siteURL, text)

	// User B (and owner) opening User A's recovery link MUST be rejected 403.
	otherOpen := openRecovery(recoveryPath, userB)
	require.Equal(t, http.StatusForbidden, otherOpen.Code, otherOpen.Body.String())
	require.Contains(t, otherOpen.Body.String(), "for someone else")

	ownerOpen := openRecovery(recoveryPath, fx.ownerUserID)
	require.Equal(t, http.StatusForbidden, ownerOpen.Code, ownerOpen.Body.String())

	// User A opening the recovery link succeeds (302 redirect to upstream AS).
	aliceOpen := openRecovery(recoveryPath, userA)
	require.Equal(t, http.StatusFound, aliceOpen.Code, aliceOpen.Body.String())
	loc, err := url.Parse(aliceOpen.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, asBase+"/authorize", loc.Scheme+"://"+loc.Host+loc.Path)
	remoteState, err := fx.f.ti.service.RemoteLoginState(ctx, loc.Query().Get("state"))
	require.NoError(t, err)
	require.Equal(t, userA, remoteState.Subject.ID, "recovery OAuth login must bind strictly to initiating user A")

	// 2. Once User A has their own personal session stored, Thread A sends
	//    User A's personal Bearer token upstream and succeeds.
	insertQualifiedRemoteSessionToken(
		t, ctx, fx.f.ti, fx.gateway.issuerID, registration,
		urn.NewUserSubject(userA), "alice-personal-secret-token", upstream.URL,
	)

	// Re-run call as Thread A: upstream receives Alice's personal token and succeeds.
	respOK := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, callBody, "", nil)
	require.Equal(t, http.StatusOK, respOK.Code, respOK.Body.String())
	okText, okErr := metaToolResultText(t, decodeRPCResponse(t, respOK))
	require.False(t, okErr, okText)
	require.Contains(t, okText, "alice-issues-ok")
	require.Equal(t, "Bearer alice-personal-secret-token", receivedAuth.Load())
}

func TestATEInitiatingUserAuthz_AuditLogRecordsAgentActorAndInitiatingUserAuthorizer(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fx := newATEActorAuthzFixture(t)
	fx.f.ti.service.SetAuthClientPermitted(true)

	authCtx, ok := contextvalues.GetAuthContext(fx.f.ctx)
	require.True(t, ok)

	userA := fx.seedOrgMember(t, "alice-audit", true, false)
	member, upstream := seedExecutableHostedProjectMember(t, ctx, fx.f.ti, authCtx, "audit_ping", `{"ok":true}`)
	t.Cleanup(upstream.Close)

	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, fx.f.agentID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, fx.ownerUserID, member.toolsetID.String())
	seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, agentURN, member.toolsetID)
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userA, member.toolsetID.String())

	actorA := fx.seedThreadActor(t, "alice-audit", userA, assistants.ATEInitiatorDashboardSender)
	tokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadProjectGatewayAudience)

	// Drive a real execute_tool request through Project Gateway so the
	// production audit logger and telemetry pipeline emit the persisted records.
	callBody := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
		"name":      metamcp.ToolExecuteTool,
		"arguments": map[string]any{"name": member.slug + "--audit_ping", "arguments": map[string]any{}},
	})
	resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, callBody, "", nil)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	text, isErr := metaToolResultText(t, decodeRPCResponse(t, resp))
	require.False(t, isErr, text)
	require.Contains(t, text, `"ok":true`)

	// Verify the production audit_logs row records agent as actor and initiating user as authorizer.
	authorizerURN := urn.NewPrincipal(urn.PrincipalTypeUser, userA)
	logs, err := auditrepo.New(fx.f.ti.conn).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{
		OrganizationID:         fx.orgID,
		ProjectID:              uuid.NullUUID{UUID: fx.projectID, Valid: true},
		IncludeAssistantEvents: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	require.Equal(t, fx.f.agentID.String(), logs[0].ActorID)
	require.Equal(t, string(urn.PrincipalTypeAgent), logs[0].ActorType)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(logs[0].Metadata, &meta))
	require.Equal(t, authorizerURN.String(), meta["authorizer_urn"])
	require.Equal(t, actorA.threadID.String(), meta["thread_id"])
	require.Equal(t, actorA.chatID.String(), meta["chat_id"])

	// Verify the production ClickHouse telemetry tool_call row records agent actor,
	// initiating user authorizer, and canonical conversation chat_id.
	requireTelemetryRowCount(t, `gram_project_id = ?
		   AND event_source = 'tool_call'
		   AND tool_name = 'audit_ping'
		   AND toString(attributes.gram.authorization.actor.type) = 'agent'
		   AND toString(attributes.gram.authorization.actor.id) = ?
		   AND toString(attributes.gram.authorization.authorizer_user_id) = ?
		   AND toString(attributes.gen_ai.conversation.id) = ?`,
		1, fx.projectID.String(), fx.f.agentID.String(), userA, actorA.chatID.String())
}

func TestATEInitiatingUserAuthz_InjectionAndSpoofingVectorsRejected(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fx := newATEActorAuthzFixture(t)
	fx.f.ti.service.SetAuthClientPermitted(true)

	frozenNow := time.Now()
	fx.f.ti.service.SetDirectWorkloadClock(func() time.Time { return frozenNow })

	userA := fx.seedOrgMember(t, "alice-vec", true, true)
	userB := fx.seedOrgMember(t, "bob-vec", true, true)

	slugB := "svc-bob-only-" + uuid.NewString()[:6]
	serverB, _ := fx.seedRemoteMemberServer(t, slugB, "secret_bob_tool")
	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, fx.f.agentID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, fx.ownerUserID, serverB.String())
	seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, agentURN, serverB)
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userB, serverB.String())
	enableWorkloadInference(t, fx.f)

	actorA := fx.seedThreadActor(t, "alice-vec", userA, assistants.ATEInitiatorDashboardSender)
	actorB := fx.seedThreadActor(t, "bob-vec", userB, assistants.ATEInitiatorDashboardSender)
	tokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadProjectGatewayAudience)
	infTokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadInferenceAudience)

	// Vector 1: Spoofed user identity headers claiming User B on Thread A's token.
	t.Run("spoofed user headers cannot escalate to User B", func(t *testing.T) {
		t.Parallel()
		resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), "", map[string]string{
			"X-User-ID":            userB,
			"X-Gram-User-ID":       userB,
			"Gram-User-ID":         userB,
			"X-Authorizer-User-ID": userB,
		})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		require.NotContains(t, resp.Body.String(), slugB)
	})

	// Vector 2: Spoofed Gram-Chat-ID / Gram-Assistant-ID headers pointing to Thread B.
	t.Run("spoofed Gram-Chat-ID header pointing to Thread B is rejected 403", func(t *testing.T) {
		t.Parallel()
		resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), "", map[string]string{
			"Gram-Chat-ID": actorB.chatID.String(),
		})
		require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())

		chatRouter := newDirectWorkloadChatMux(t, fx.f, &managedInferenceCompletion{})
		infPath := "/projects/" + fx.projectID.String() + "/chat/completions"
		infBody := fmt.Sprintf(`{"model":"test-model","user":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, actorA.acpSessionID)
		for _, hdr := range []string{"Gram-Chat-ID", "Gram-Assistant-ID"} {
			r := postCompletionRequest(chatRouter, infPath, infTokenA, map[string]string{hdr: actorB.chatID.String()}, infBody)
			require.Equal(t, http.StatusForbidden, r.Code, "header %s must be refused on inference", hdr)
		}
	})

	// Vector 3: Spoofed MCP JSON-RPC _meta fields claiming User B / Thread B.
	t.Run("spoofed MCP JSON-RPC _meta fields cannot escalate to User B", func(t *testing.T) {
		t.Parallel()
		body := makeMetaRPCBody(t, mcpversions.MethodToolsCall, map[string]any{
			"name":      metamcp.ToolListServers,
			"arguments": map[string]any{},
			"_meta": map[string]any{
				"user_id":            userB,
				"authorizer_user_id": userB,
				"thread_id":          actorB.threadID.String(),
				"chat_id":            actorB.chatID.String(),
			},
		})
		resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, body, "", nil)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		require.NotContains(t, resp.Body.String(), slugB)
	})

	// Vector 4: Spoofed query parameters claiming User B / Thread B.
	t.Run("spoofed query parameters cannot escalate to User B", func(t *testing.T) {
		t.Parallel()
		q := url.Values{
			"user_id":            {userB},
			"authorizer_user_id": {userB},
			"thread_id":          {actorB.threadID.String()},
			"chat_id":            {actorB.chatID.String()},
		}.Encode()
		resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokenA, listServersBody(t), q, nil)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		require.NotContains(t, resp.Body.String(), slugB)
	})

	// Vector 5: Cross-thread session claim on inference (Token A claiming Thread B's ACP session ID).
	t.Run("cross-thread ACP session claim on inference is rejected 403", func(t *testing.T) {
		t.Parallel()
		chatRouter := newDirectWorkloadChatMux(t, fx.f, &managedInferenceCompletion{})
		infPath := "/projects/" + fx.projectID.String() + "/chat/completions"
		crossBody := fmt.Sprintf(`{"model":"test-model","user":%q,"messages":[{"role":"user","content":"steal"}],"stream":false}`, actorB.acpSessionID)
		r := postCompletionRequest(chatRouter, infPath, infTokenA, nil, crossBody)
		require.Equal(t, http.StatusForbidden, r.Code, r.Body.String())
	})

	// Vector 6: Replayed token from a retired/replaced actor (old actor_uid soft-deleted),
	// even inside the 5s Stage-1 cache window AND even if a same-name replacement exists.
	t.Run("replayed retired actor token is rejected immediately inside Stage-1 cache window", func(t *testing.T) {
		t.Parallel()
		actorRet := fx.seedThreadActor(t, "retire-me", userA, assistants.ATEInitiatorDashboardSender)
		tokRet := fx.mintActorToken(t, actorRet, mcp.DirectWorkloadProjectGatewayAudience)

		// Warm Stage-1 cache while live.
		okResp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokRet, listServersBody(t), "", nil)
		require.Equal(t, http.StatusOK, okResp.Code, okResp.Body.String())

		// Soft-delete the old actor row (leaving its workload admission intact to prove
		// actor_uid liveness is enforced independently of Stage-1 admission caching).
		_, err := assistantrepo.New(fx.f.ti.conn).SoftDeleteAssistantATEActor(ctx, assistantrepo.SoftDeleteAssistantATEActorParams{
			RetiredState: "retired", ProjectID: fx.projectID, ID: actorRet.actorRowID,
		})
		require.NoError(t, err)

		// Even inserting a new live row with the SAME actor_name + SAME subject but a
		// NEW actor_uid must NOT validate the replayed old-UID token!
		bindingJSON, err := json.Marshal(map[string]any{
			"issuer_id":           fx.f.issuerID,
			"subject":             actorRet.subject,
			"identity":            fx.identity,
			"mcp_project_gateway": true,
		})
		require.NoError(t, err)
		_, err = assistantrepo.New(fx.f.ti.conn).InsertAssistantATEActor(ctx, assistantrepo.InsertAssistantATEActorParams{
			ActorName:                actorRet.actorName,
			ActorUid:                 "uid-brand-new-replacement-" + uuid.NewString(),
			State:                    "running",
			Configured:               true,
			WorkloadBindingJson:      bindingJSON,
			WarmUntil:                pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
			InitiatingUserID:         conv.ToPGText(userB),
			InitiatingUserResolution: assistants.ATEInitiatorDashboardSender,
			RuntimeID:                fx.runtimeID,
			ProjectID:                fx.projectID,
			AssistantID:              fx.assistantID,
			ThreadID:                 actorRet.threadID,
		})
		require.NoError(t, err)

		replayed := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokRet, listServersBody(t), "", nil)
		require.Equal(t, http.StatusUnauthorized, replayed.Code, "replayed retired-actor token must be refused 401 even inside 5s cache window")
	})

	// Vector 7: Tampered / malformed / mismatched actor.ate.dev claims and worker-for-actor tokens.
	t.Run("tampered or mismatched actor.ate.dev claims and worker tokens fail closed 401", func(t *testing.T) {
		t.Parallel()
		badTokens := []struct {
			name  string
			token string
		}{
			{
				name:  "missing actor.ate.dev claim on actor/ subject",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, nil),
			},
			{
				name: "mismatched actorName vs sub",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorB.actorName, "actorUID": actorB.actorUID},
				}),
			},
			{
				name: "mismatched atespace vs sub",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": "other-space", "actorName": actorA.actorName, "actorUID": actorA.actorUID},
				}),
			},
			{
				name: "missing actorUID",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorA.actorName},
				}),
			},
			{
				name: "unknown actorUID",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorA.actorName, "actorUID": "uid-does-not-exist"},
				}),
			},
			{
				name: "non-empty node on actor.ate.dev claim",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorA.actorName, "actorUID": actorA.actorUID, "node": "worker-1"},
				}),
			},
			{
				name: "worker-for-actor.ate.dev claim rejected",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"worker-for-actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorA.actorName, "actorUID": actorA.actorUID, "node": "worker-1"},
				}),
			},
			{
				name: "actorA subject carrying actorB UID fails row identity cross-check",
				token: fx.mintATEJWT(t, actorA.subject, []string{mcp.DirectWorkloadProjectGatewayAudience}, map[string]any{
					"actor.ate.dev": map[string]string{"atespace": fx.atespace, "actorName": actorA.actorName, "actorUID": actorB.actorUID},
				}),
			},
		}
		for _, tc := range badTokens {
			resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tc.token, listServersBody(t), "", nil)
			require.Equal(t, http.StatusUnauthorized, resp.Code, "case %q must fail closed 401, got body: %s", tc.name, resp.Body.String())
		}
	})

	// Vector 8: Non-running actor states (suspended/crashed), stopped runtimes,
	// and soft-deleted chats fail closed 401 even inside the 5s Stage-1 cache window.
	t.Run("suspended or crashed actor, stopped runtime, and deleted chat fail closed 401", func(t *testing.T) {
		t.Parallel()
		for _, nonRunningState := range []string{"suspended", "crashed"} {
			actorNonRun := fx.seedThreadActor(t, "state-"+nonRunningState, userA, assistants.ATEInitiatorDashboardSender)
			tok := fx.mintActorToken(t, actorNonRun, mcp.DirectWorkloadProjectGatewayAudience)
			require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tok, listServersBody(t), "", nil).Code)

			_, err := assistantrepo.New(fx.f.ti.conn).UpdateAssistantATEActorState(ctx, assistantrepo.UpdateAssistantATEActorStateParams{
				State: nonRunningState, ProjectID: fx.projectID, ID: actorNonRun.actorRowID,
			})
			require.NoError(t, err)
			resp := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tok, listServersBody(t), "", nil)
			require.Equal(t, http.StatusUnauthorized, resp.Code, "actor in state %q must be rejected 401", nonRunningState)
		}

		// Soft-deleted chat must reject the actor immediately.
		actorDelChat := fx.seedThreadActor(t, "del-chat", userA, assistants.ATEInitiatorDashboardSender)
		tokDelChat := fx.mintActorToken(t, actorDelChat, mcp.DirectWorkloadProjectGatewayAudience)
		require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokDelChat, listServersBody(t), "", nil).Code)
		require.NoError(t, testrepo.New(fx.f.ti.conn).ForceSoftDeleteChat(ctx, actorDelChat.chatID))
		require.Equal(t, http.StatusUnauthorized, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, tokDelChat, listServersBody(t), "", nil).Code)

		// Stopped runtime (isolated fixture so sibling subtests are unaffected) must reject immediately.
		fxStopped := newATEActorAuthzFixture(t)
		fxStopped.f.ti.service.SetDirectWorkloadClock(func() time.Time { return frozenNow })
		uStopped := fxStopped.seedOrgMember(t, "alice-stopped", true, false)
		actorStopped := fxStopped.seedThreadActor(t, "stopped-rt", uStopped, assistants.ATEInitiatorDashboardSender)
		tokStopped := fxStopped.mintActorToken(t, actorStopped, mcp.DirectWorkloadProjectGatewayAudience)
		require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fxStopped.f.ti, fxStopped.gateway, tokStopped, listServersBody(t), "", nil).Code)
		require.NoError(t, assistantrepo.New(fxStopped.f.ti.conn).SetAssistantRuntimeStateForTest(ctx, assistantrepo.SetAssistantRuntimeStateForTestParams{
			ProjectID: fxStopped.projectID, RuntimeID: fxStopped.runtimeID, State: "stopped", UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		}))
		require.Equal(t, http.StatusUnauthorized, serveProjectGatewayHTTPWithOptions(t, fxStopped.f.ti, fxStopped.gateway, tokStopped, listServersBody(t), "", nil).Code)
	})
}

func TestATEInitiatingUserAuthz_BlockedUserRefusedOnGatewayAndInference(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	fx := newATEActorAuthzFixture(t)
	fx.f.ti.service.SetAuthClientPermitted(true)

	// Freeze the Stage-1 cache clock to prove blocked-user refusal runs fresh on
	// every request inside the 5-second Stage-1 cache window.
	frozenNow := time.Now()
	fx.f.ti.service.SetDirectWorkloadClock(func() time.Time { return frozenNow })

	userA := fx.seedOrgMember(t, "alice-blocked", true, true)

	slugA := "svc-blocked-" + uuid.NewString()[:6]
	serverA, _ := fx.seedRemoteMemberServer(t, slugA, "ping_blocked")
	agentURN := urn.NewPrincipal(urn.PrincipalTypeAgent, fx.f.agentID.String())
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, fx.ownerUserID, serverA.String())
	seedPrincipalMCPConnectGrant(t, ctx, fx.f.ti, fx.orgID, agentURN, serverA)
	seedUserMCPConnectGrant(t, ctx, fx.f.ti.conn, fx.orgID, userA, serverA.String())
	enableWorkloadInference(t, fx.f)

	actorA := fx.seedThreadActor(t, "alice-blocked", userA, assistants.ATEInitiatorDashboardSender)
	actorNoUser := fx.seedThreadActor(t, "cron-blocked", "", assistants.ATEInitiatorNoUser)

	gwTokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadProjectGatewayAudience)
	infTokenA := fx.mintActorToken(t, actorA, mcp.DirectWorkloadInferenceAudience)
	gwTokenNoUser := fx.mintActorToken(t, actorNoUser, mcp.DirectWorkloadProjectGatewayAudience)
	infTokenNoUser := fx.mintActorToken(t, actorNoUser, mcp.DirectWorkloadInferenceAudience)

	var inferenceCalls atomic.Int32
	completionClient := &managedInferenceCompletion{complete: func(ctx context.Context, req openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
		inferenceCalls.Add(1)
		msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{})
		return &openrouter.CompletionResponse{Message: &msg, Model: "test/model"}, nil
	}}
	chatRouter := newDirectWorkloadChatMux(t, fx.f, completionClient)
	infPath := "/projects/" + fx.projectID.String() + "/chat/completions"
	infBodyA := fmt.Sprintf(`{"model":"test-model","user":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, actorA.acpSessionID)
	infBodyNoUser := fmt.Sprintf(`{"model":"test-model","user":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, actorNoUser.acpSessionID)

	// Warm Stage-1 cache for both threads while neither user is blocked.
	require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenA, listServersBody(t), "", nil).Code)
	require.Equal(t, http.StatusOK, postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA).Code)
	require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenNoUser, listServersBody(t), "", nil).Code)
	require.Equal(t, http.StatusOK, postCompletionRequest(chatRouter, infPath, infTokenNoUser, nil, infBodyNoUser).Code)
	require.EqualValues(t, 2, inferenceCalls.Load())

	setBlocked := func(userID string, blocked bool) {
		require.NoError(t, usersrepo.New(fx.f.ti.conn).SetUserBlacklisted(ctx, usersrepo.SetUserBlacklistedParams{
			Blacklisted: blocked,
			ID:          userID,
		}))
		userblock.Forget(fx.f.ti.conn)
	}

	// 1. Blocking the initiating user immediately refuses Thread A on BOTH Project
	//    Gateway and inference with 401 (never falling back to userless authority),
	//    while leaving the unaffected userless thread working.
	setBlocked(userA, true)
	gwBlocked := serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenA, listServersBody(t), "", nil)
	require.Equal(t, http.StatusUnauthorized, gwBlocked.Code, gwBlocked.Body.String())
	infBlocked := postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA)
	require.Equal(t, http.StatusUnauthorized, infBlocked.Code, infBlocked.Body.String())
	require.EqualValues(t, 2, inferenceCalls.Load(), "blocked initiating user must not reach inference")
	require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenNoUser, listServersBody(t), "", nil).Code)

	// Unblocking the initiating user restores both endpoints.
	setBlocked(userA, false)
	require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenA, listServersBody(t), "", nil).Code)
	require.Equal(t, http.StatusOK, postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA).Code)
	require.EqualValues(t, 3, inferenceCalls.Load())

	// 2. Blocking the assistant's owner refuses BOTH human-initiated and userless
	//    direct-bearer workload requests on Project Gateway and inference.
	setBlocked(fx.ownerUserID, true)
	require.Equal(t, http.StatusUnauthorized, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenA, listServersBody(t), "", nil).Code)
	require.Equal(t, http.StatusUnauthorized, postCompletionRequest(chatRouter, infPath, infTokenA, nil, infBodyA).Code)
	require.Equal(t, http.StatusUnauthorized, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenNoUser, listServersBody(t), "", nil).Code)
	require.Equal(t, http.StatusUnauthorized, postCompletionRequest(chatRouter, infPath, infTokenNoUser, nil, infBodyNoUser).Code)
	require.EqualValues(t, 3, inferenceCalls.Load(), "blocked owner must not reach inference")

	// Unblocking the owner restores access.
	setBlocked(fx.ownerUserID, false)
	require.Equal(t, http.StatusOK, serveProjectGatewayHTTPWithOptions(t, fx.f.ti, fx.gateway, gwTokenNoUser, listServersBody(t), "", nil).Code)
}
