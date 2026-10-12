package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/agents/lifecycle"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

var testIdentityService = assistantidentity.New("https://platform.example.invalid", audit.NewLogger())

// identityFlags sets the assistant identity provisioning rollout for the test
// organization.
func identityFlags(enabled bool) *feature.InMemory {
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagAgentIdentityCredentials, "org-test", enabled)
	return flags
}

func newTestAuthzEngine(t *testing.T, db *pgxpool.Pool) *authz.Engine {
	t.Helper()
	return authz.NewEngine(testenv.NewLogger(t), db, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
}

// seedIdentityMembers creates the organization members that own dedicated
// assistant agents. Agent owners must be organization members.
func seedIdentityMembers(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{ID: "org-test", Name: "Identity tests", Slug: "identity-tests", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{}, CreationSource: pgtype.Text{}})
	require.NoError(t, err)
	for _, id := range []string{"user-1", "user-2", "user-test"} {
		_, err = usersrepo.New(db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: id, Email: id + "@example.invalid", DisplayName: id, PhotoUrl: pgtype.Text{}, Admin: false})
		require.NoError(t, err)
		_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: "org-test", UserID: pgtype.Text{String: id, Valid: true}})
		require.NoError(t, err)
	}
}

func createLegacyAssistant(t *testing.T, db *pgxpool.Pool, project uuid.UUID, name string) assistantrepo.CreateAssistantRow {
	t.Helper()
	row, err := assistantrepo.New(db).CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: "org-test", CreatedByUserID: pgtype.Text{String: "user-1", Valid: true}, Name: name, Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: StatusActive})
	require.NoError(t, err)
	return row
}

func TestCreateAssistantProvisionsDedicatedAgent(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_create")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-create")
	core := newProvisioningCore(t, db)

	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Identity assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), record.IdentityState)
	require.NotNil(t, record.AgentID)

	agentID := uuid.MustParse(*record.AgentID)
	agent, err := agentrepo.New(db).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-test", ID: agentID})
	require.NoError(t, err)
	require.Equal(t, "user-1", agent.OwnerUserID)
	grants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{OrganizationID: "org-test", AgentID: agentID})
	require.NoError(t, err)
	policy := make([]authz.Grant, 0, len(grants))
	for _, row := range grants {
		selector, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		policy = append(policy, authz.NewGrantWithSelector(authz.Scope(row.Scope), selector))
	}
	anyServer := uuid.NewString()
	for _, check := range []authz.Check{
		authz.MCPCheck(authz.ScopeMCPConnect, anyServer, project.String()),
		authz.MCPCheck(authz.ScopeMCPRead, anyServer, project.String()),
		{Scope: authz.ScopeSkillRead, ResourceKind: "", ResourceID: project.String(), Dimensions: nil},
	} {
		allowed, err := authz.GrantsAuthorize(policy, check)
		require.NoError(t, err)
		require.True(t, allowed, check.Scope)
	}
	allowed, err := authz.GrantsAuthorize(policy, authz.MCPCheck(authz.ScopeMCPConnect, anyServer, uuid.NewString()))
	require.NoError(t, err)
	require.False(t, allowed, "the ceiling is limited to the assistant's project")
	for _, scope := range []authz.Scope{authz.ScopeAssistantRead, authz.ScopeAssistantWrite} {
		allowed, err = authz.GrantsAuthorize(policy, authz.AssistantCheck(scope, record.ID.String(), project.String()))
		require.NoError(t, err)
		require.True(t, allowed, scope)
	}
	allowed, err = authz.GrantsAuthorize(policy, authz.AssistantCheck(authz.ScopeAssistantWrite, uuid.NewString(), project.String()))
	require.NoError(t, err)
	require.False(t, allowed, "the agent administers only its own assistant")

	provisioned, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAssistantIdentityProvision)
	require.NoError(t, err)
	require.EqualValues(t, 1, provisioned)
	created, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionAgentCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, created)
}

func TestUpgradeAssistantIdentityAcceptsAPIKeyUserAndIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, ctx, project, db := newRBACServiceWithConn(t, "identity_upgrade")
	legacy := createLegacyAssistant(t, db, project, "Legacy assistant")
	before, err := svc.core.GetAssistant(ctx, project, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.NeverConfigured), before.IdentityState)

	payload := &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID.String(), SessionToken: nil, ProjectSlugInput: nil}
	_, err = svc.UpgradeAssistantIdentity(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err, "session users need project write")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	apiKey := *authCtx
	apiKey.APIKeyID = uuid.NewString()
	apiKey.SessionID = nil
	keyCtx := contextvalues.WithLegacyAPIKeyAuthorization(ctx, &apiKey)
	first, err := svc.UpgradeAssistantIdentity(keyCtx, payload)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), *first.IdentityState)
	second, err := svc.UpgradeAssistantIdentity(keyCtx, payload)
	require.NoError(t, err)
	require.Equal(t, first.AgentID, second.AgentID)
}

func TestManagedDashboardTriggerBindsLikeAnyRoot(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_dashboard")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-dashboard")
	core := newProvisioningCore(t, db)

	legacy := createLegacyAssistant(t, db, project, "Legacy managed")
	require.NoError(t, assistantrepo.New(db).CreateProjectManagedAssistant(t.Context(), assistantrepo.CreateProjectManagedAssistantParams{ProjectID: project, AssistantID: legacy.ID}))
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	resolution, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, legacy.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.NeverConfigured, resolution.State)

	_, err = core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID, ActorUserID: "user-2", AgentID: uuid.Nil, AgentName: ""})
	require.NoError(t, err)
	reused, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	require.Equal(t, root, reused)
	resolution, err = testIdentityService.Resolve(t.Context(), db, "org-test", project, legacy.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)

	require.NoError(t, core.DisableManagedAssistant(t.Context(), project, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), nil))
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), db, *resolution.Identity), assistantidentity.ErrInvalidIdentity)
	fresh, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), fresh.IdentityState)
	freshRoot, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, fresh.ID, fresh.Name)
	require.NoError(t, err)
	resolution, err = testIdentityService.Resolve(t.Context(), db, "org-test", project, fresh.ID, freshRoot)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)
}

func TestUserAgentEditsDoNotBlockAssistantManagement(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_agent_edits")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-agent-edits")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Edited agent", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, true)
	require.NoError(t, err)
	agents := agentrepo.New(db)
	agentID := uuid.MustParse(*record.AgentID)

	_, err = agents.RenameAgent(t.Context(), agentrepo.RenameAgentParams{OrganizationID: "org-test", ID: agentID, Name: "Renamed by a user"})
	require.NoError(t, err)
	_, err = agents.TransferAgent(t.Context(), agentrepo.TransferAgentParams{OrganizationID: "org-test", ID: agentID, OwnerUserID: "user-2"})
	require.NoError(t, err)
	got, err := core.GetAssistant(t.Context(), project, record.ID)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), got.IdentityState)

	_, err = agents.SuspendAgent(t.Context(), agentrepo.SuspendAgentParams{OrganizationID: "org-test", ID: agentID})
	require.NoError(t, err)
	got, err = core.GetAssistant(t.Context(), project, record.ID)
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Unavailable), got.IdentityState)

	paused := StatusPaused
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	_, err = core.UpgradeAssistantIdentity(t.Context(), assistantidentity.ProvisionParams{OrganizationID: "org-test", ProjectID: project, AssistantID: record.ID, ActorUserID: "user-1", AgentID: uuid.Nil, AgentName: ""})
	require.NoError(t, err)
	require.NoError(t, core.DeleteAssistant(t.Context(), project, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	kept, err := agents.GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: "org-test", ID: agentID})
	require.NoError(t, err)
	require.Equal(t, lifecycle.Suspended, lifecycle.Derive(kept), "deleting the assistant leaves its agent as the user left it")
}

func TestDeleteAssistantWithdrawsWorkloads(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_delete")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-delete")
	core := newProvisioningCore(t, db)
	record, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	resolution, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)

	require.NoError(t, core.DeleteAssistant(t.Context(), project, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	admitted, err := workloadidentity.IsAdmitted(t.Context(), db, workloadidentity.AdmissionParams{OrganizationID: "org-test", ProjectID: uuid.NullUUID{UUID: project, Valid: true}, WorkloadIssuerID: resolution.Identity.IssuerID, Subject: resolution.Identity.Subject})
	require.NoError(t, err)
	require.False(t, admitted)
	_, assigned, err := workloadidentity.ResolveAssignedAgent(t.Context(), db, workloadidentity.AssignmentParams{OrganizationID: "org-test", WorkloadIssuerID: resolution.Identity.IssuerID, Subject: resolution.Identity.Subject})
	require.NoError(t, err)
	require.False(t, assigned)
	states, err := assistantidentity.States(t.Context(), db, project, []uuid.UUID{record.ID})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.NeverConfigured, states[record.ID].State)
}

func TestIdentityProvisioningFollowsRolloutFlag(t *testing.T) {
	t.Parallel()
	svc, ctx, project, _ := newRBACServiceWithConn(t, "identity_rollout")
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeAssistantWrite, project.String()), authz.NewGrant(authz.ScopeProjectWrite, project.String()))
	create := func(name string) *types.Assistant {
		t.Helper()
		created, err := svc.CreateAssistant(granted, &gen.CreateAssistantPayload{SessionToken: nil, ProjectSlugInput: nil, Name: name, Model: "openai/gpt-4o-mini", Instructions: "", Toolsets: nil, McpServers: nil, WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil})
		require.NoError(t, err)
		return created
	}

	svc.features = identityFlags(true)
	active := create("Rolled out")
	require.Equal(t, string(assistantidentity.Active), *active.IdentityState)

	svc.features = identityFlags(false)
	legacy := create("Not rolled out")
	require.Equal(t, string(assistantidentity.NeverConfigured), *legacy.IdentityState)
	managed, err := svc.EnsureManagedAssistant(granted, &gen.EnsureManagedAssistantPayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.NeverConfigured), *managed.IdentityState)
	_, err = svc.UpgradeAssistantIdentity(granted, &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID, SessionToken: nil, ProjectSlugInput: nil})
	var refused *oops.ShareableError
	require.ErrorAs(t, err, &refused)
	require.Equal(t, oops.CodeForbidden, refused.Code)

	// Turning the rollout off gates provisioning only.
	kept, err := svc.core.GetAssistant(ctx, project, uuid.MustParse(active.ID))
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), kept.IdentityState)

	svc.features = identityFlags(true)
	upgraded, err := svc.UpgradeAssistantIdentity(granted, &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID, SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, string(assistantidentity.Active), *upgraded.IdentityState)
}

func TestSnapshotCeilingCarriesAssistantSelfAdministration(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_ceiling")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-ceiling")
	core := newProvisioningCore(t, db)
	record, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1", true)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	resolution, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)

	ceiling, err := testIdentityService.SnapshotCeiling(t.Context(), db, *resolution.Identity)
	require.NoError(t, err)
	require.Equal(t, runtimepolicy.CurrentDelegatedPolicyVersion, ceiling.EncodingVersion)
	policy, err := runtimepolicy.DecodeDelegatedPolicy(ceiling.EncodingVersion, ceiling.Policy)
	require.NoError(t, err)
	grants := policy.RuntimeGrants()
	allowed, err := authz.GrantsAuthorize(grants, authz.AssistantCheck(authz.ScopeAssistantWrite, record.ID.String(), project.String()))
	require.NoError(t, err)
	require.True(t, allowed, "the ceiling keeps the agent's own-assistant administration")
	allowed, err = authz.GrantsAuthorize(grants, authz.AssistantCheck(authz.ScopeAssistantWrite, uuid.NewString(), project.String()))
	require.NoError(t, err)
	require.False(t, allowed)
}
