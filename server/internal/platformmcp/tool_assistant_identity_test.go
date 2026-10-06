package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	genassistants "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type assistantIdentityManagementFunc func(context.Context, *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error)

func (f assistantIdentityManagementFunc) UpgradeAssistantIdentity(ctx context.Context, payload *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error) {
	return f(ctx, payload)
}

func TestAssistantIdentityToolContract(t *testing.T) {
	t.Parallel()
	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "live", Version: "1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "unavailable", Version: "1"}, nil))
	registerAssistantIdentityTool(live, &AssistantIdentityService{})
	registerAssistantIdentityTool(unavailable, nil)
	a := descriptorByName(t, live, upgradeAssistantIdentityToolName)
	b := descriptorByName(t, unavailable, upgradeAssistantIdentityToolName)
	require.JSONEq(t, string(a.InputSchema), string(b.InputSchema))
	require.Equal(t, a.Meta, b.Meta)
	require.Equal(t, ExternalAuthorizationOrgAdmin, a.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, a.Meta.ProjectScope)
	require.Equal(t, externalOnly, a.Meta.Audiences)
	require.Empty(t, live.For(AudienceAssistant))
	for _, field := range []string{"project_id", "assistant_id", "confirmed"} {
		require.Contains(t, string(a.InputSchema), field)
	}
	require.Contains(t, a.Description, "project:write")
	require.Contains(t, a.Description, "Repeating the upgrade is safe")
	require.Contains(t, a.Description, "ACTIVE does not prove")
	ctx := contextWithPrincipal(t.Context(), Principal{OrganizationID: "test-org", UserID: "test-user"})
	_, err := b.Invoke(ctx, []byte(`{"project_id":"`+uuid.NewString()+`","assistant_id":"`+uuid.NewString()+`","confirmed":true}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, unavailableCode)
}

func TestAssistantIdentityUpgradeUsesAuthorizedEndpointAndSafeProjection(t *testing.T) {
	t.Parallel()
	projectID, assistantID := uuid.New(), uuid.New()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	sessionID := "test-session"
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, SessionID: &sessionID}, actor)
	grants := []authz.Grant{authz.NewGrant(authz.ScopeProjectWrite, projectID.String())}
	ctx = authz.GrantsToContext(ctx, grants)
	state, agentID := "ACTIVE", uuid.NewString()
	calls := 0
	service := &AssistantIdentityService{
		resolveProject: func(_ context.Context, org string, input FindMCPInput) (ResolvedProject, error) {
			require.Equal(t, principal.OrganizationID, org)
			require.Equal(t, projectID.String(), input.ProjectID)
			require.Empty(t, input.ProjectSlug)
			return ResolvedProject{ID: projectID, Slug: "explicit-project"}, nil
		},
		management: assistantIdentityManagementFunc(func(ctx context.Context, payload *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error) {
			calls++
			require.Equal(t, assistantID.String(), payload.ID)
			require.Equal(t, &agentID, payload.AgentID, "the agent choice reaches the shared endpoint unchanged")
			scoped, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.Equal(t, projectID, *scoped.ProjectID)
			require.Equal(t, &sessionID, scoped.SessionID)
			gotActor, ok := contextvalues.AuthenticatedActor(ctx)
			require.True(t, ok)
			require.Equal(t, actor, gotActor)
			gotGrants, ok := authz.GrantsFromContext(ctx)
			require.True(t, ok)
			require.Equal(t, grants, gotGrants)
			return &types.Assistant{ID: assistantID.String(), ProjectID: projectID.String(), IdentityState: &state, AgentID: &agentID, Instructions: "private instructions", CreatedByUserID: new("private creator")}, nil
		}),
	}
	input := UpgradeAssistantIdentityInput{ProjectID: projectID.String(), AssistantID: assistantID.String(), AgentID: &agentID, AgentName: nil, Confirmed: true}
	for range 2 {
		output, err := service.upgrade(ctx, principal, input)
		require.NoError(t, err)
		require.Equal(t, &state, output.IdentityState)
		require.Equal(t, &agentID, output.AgentID)
		encoded, err := json.Marshal(output)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private")
		require.NotContains(t, string(encoded), "instructions")
	}
	require.Equal(t, 2, calls, "retries use the same idempotent application endpoint")
	original, _ := contextvalues.GetAuthContext(ctx)
	require.Nil(t, original.ProjectID, "project scoping must not mutate the incoming context")
	denied := oops.C(oops.CodeForbidden)
	service.management = assistantIdentityManagementFunc(func(context.Context, *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error) {
		return nil, denied
	})
	_, err := service.upgrade(ctx, principal, input)
	require.ErrorIs(t, err, denied, "endpoint authorization failures are never bypassed")
}

func TestAssistantIdentityUpgradeRejectsAmbiguousUnconfirmedAndHiddenTargets(t *testing.T) {
	t.Parallel()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	projectID, assistantID := uuid.NewString(), uuid.NewString()
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	resolutions := 0
	service := &AssistantIdentityService{
		resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
			resolutions++
			return ResolvedProject{}, ErrForbidden
		},
		management: assistantIdentityManagementFunc(func(context.Context, *genassistants.UpgradeAssistantIdentityPayload) (*types.Assistant, error) {
			t.Fatal("must not upgrade a hidden or invalid target")
			return nil, nil
		}),
	}
	for _, input := range []UpgradeAssistantIdentityInput{
		{ProjectID: projectID, AssistantID: assistantID},
		{AssistantID: assistantID, Confirmed: true},
		{ProjectID: projectID, AssistantID: "not-an-id", Confirmed: true},
		{ProjectID: uuid.Nil.String(), AssistantID: assistantID, Confirmed: true},
	} {
		_, err := service.upgrade(ctx, principal, input)
		var shared *oops.ShareableError
		require.ErrorAs(t, err, &shared)
		require.Equal(t, oops.CodeBadRequest, shared.Code)
	}
	require.Zero(t, resolutions)
	valid := UpgradeAssistantIdentityInput{ProjectID: projectID, AssistantID: assistantID, AgentID: nil, AgentName: nil, Confirmed: true}
	_, err := service.upgrade(t.Context(), principal, valid)
	var shared *oops.ShareableError
	require.ErrorAs(t, err, &shared)
	require.Equal(t, oops.CodeUnauthorized, shared.Code)
	require.Zero(t, resolutions)
	_, err = service.upgrade(ctx, principal, valid)
	require.ErrorIs(t, err, ErrForbidden)
	require.Equal(t, 1, resolutions)
}

func TestAssistantIdentityRefusalsDoNotLeakBackendDetails(t *testing.T) {
	t.Parallel()
	for _, err := range []error{errors.New("private policy and credential"), oops.E(oops.CodeConflict, nil, "private binding"), oops.E(oops.CodeNotFound, nil, "private assistant"), oops.E(oops.CodeForbidden, nil, "private grants")} {
		result, ok := assistantIdentityToolResult(err)
		require.True(t, ok)
		require.True(t, result.IsError)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.NotContains(t, text.Text, "private")
	}
}

// Exercise the real management service, not only an adapter stub: Platform MCP
// must retain endpoint project authorization even after organization admission.
func TestAssistantIdentityAPIAndMCPRequireSameProjectWrite(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	management := assistants.NewService(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, &sessions.Manager{}, engine, nil, nil, nil)
	projectID, assistantID := uuid.New(), uuid.New()
	principal := Principal{OrganizationID: "test-org", UserID: "test-user"}
	auth := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &projectID}
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), auth, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	service := &AssistantIdentityService{management: management, resolveProject: func(context.Context, string, FindMCPInput) (ResolvedProject, error) {
		return ResolvedProject{ID: projectID}, nil
	}}
	for _, grants := range [][]authz.Grant{nil, {authz.NewGrant(authz.ScopeProjectWrite, uuid.NewString())}, {authz.NewGrant(authz.ScopeProjectRead, projectID.String())}} {
		scoped := authz.GrantsToContext(ctx, grants)
		_, apiErr := management.UpgradeAssistantIdentity(scoped, &genassistants.UpgradeAssistantIdentityPayload{ID: assistantID.String()})
		_, mcpErr := service.upgrade(scoped, principal, UpgradeAssistantIdentityInput{ProjectID: projectID.String(), AssistantID: assistantID.String(), AgentID: nil, AgentName: nil, Confirmed: true})
		var apiOops, mcpOops *oops.ShareableError
		require.ErrorAs(t, apiErr, &apiOops)
		require.ErrorAs(t, mcpErr, &mcpOops)
		require.Equal(t, oops.CodeForbidden, apiOops.Code)
		require.Equal(t, apiOops.Code, mcpOops.Code)
	}
}

func TestAssistantIdentityOAuthUpgradeMatchesAPI(t *testing.T) {
	t.Parallel()
	db, err := platformMCPInfra.CloneTestDatabase(t, "platform_assistant_identity_oauth")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), db)
	principal.ClientID = "test-identity-oauth-client"
	_, err = usersrepo.New(db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: principal.UserID, Email: "identity-upgrade@example.invalid", DisplayName: "Identity upgrade test"})
	require.NoError(t, err)
	_, err = organizationsrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: principal.OrganizationID, UserID: pgtype.Text{String: principal.UserID, Valid: true}})
	require.NoError(t, err)
	legacy, err := assistantrepo.New(db).CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, CreatedByUserID: pgtype.Text{String: principal.UserID, Valid: true},
		Name: "Legacy assistant", Model: "openai/gpt-4o-mini", Instructions: "private system instructions", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	tracer, meter := testenv.NewTracerProvider(t), testenv.NewMeterProvider(t)
	identities := assistantidentity.New("https://platform.example.invalid", audit.NewLogger())
	core := assistants.NewServiceCore(logger, tracer, meter, db, nil, nil, nil, nil, nil, nil, telemetry.NewStub(logger), nil, audit.NewLogger(), identities, engine)
	management := assistants.NewService(logger, tracer, meter, db, &sessions.Manager{}, engine, core, nil, nil)
	service := NewAssistantIdentityService(management, NewPostgresReader(logger, db))
	ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &project.ID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextWithPrincipal(ctx, principal)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID), authz.NewGrant(authz.ScopeProjectWrite, project.ID.String())})
	clientID, ok := contextvalues.GetOAuthClientID(ctx)
	require.True(t, ok)
	require.Equal(t, principal.ClientID, clientID)
	output, err := service.upgrade(ctx, principal, UpgradeAssistantIdentityInput{ProjectID: project.ID.String(), AssistantID: legacy.ID.String(), AgentID: nil, AgentName: nil, Confirmed: true})
	require.NoError(t, err, "Platform MCP OAuth users can upgrade")
	require.Equal(t, "ACTIVE", *output.IdentityState)
	require.NotEmpty(t, output.AgentID)
	// The API shares the same idempotent endpoint and must return the same
	// committed identity rather than creating another agent.
	api, err := management.UpgradeAssistantIdentity(ctx, &genassistants.UpgradeAssistantIdentityPayload{ID: legacy.ID.String()})
	require.NoError(t, err)
	require.Equal(t, api.IdentityState, output.IdentityState)
	require.Equal(t, api.AgentID, output.AgentID)
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionAssistantIdentityProvision)
	require.NoError(t, err)
	require.Equal(t, int64(1), count, "idempotent API retry must not duplicate the committed provisioning audit")
	event, err := audittest.LatestAuditLogByAction(ctx, db, audit.ActionAssistantIdentityProvision)
	require.NoError(t, err)
	require.Equal(t, principal.UserID, event.ActorID)
	require.Equal(t, principal.OrganizationID, event.OrganizationID)
	require.Equal(t, project.ID, event.ProjectID.UUID)
	require.Equal(t, legacy.ID.String(), event.SubjectID)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(event.Metadata, &metadata))
	require.Equal(t, *output.AgentID, metadata["agent_id"])
}
