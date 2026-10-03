package assistants

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// Provisioning requires persisted active membership, independently of the API
// grant fixture. Do not bypass that requirement with injected authorization.
func seedIdentityCreationMembers(t *testing.T, db *pgxpool.Pool) {
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

func TestIdentityCreationConcurrentIndependentRequests(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_create_key")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-create-key")
	core := newProvisioningCore(t, db)
	results := make([]assistantRecord, 4)
	var group errgroup.Group
	for i := range results {
		group.Go(func() error {
			record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", fmt.Sprintf("Assistant %d", i), "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
			results[i] = record
			return err
		})
	}
	require.NoError(t, group.Wait())
	for _, record := range results {
		require.NotEmpty(t, record.ID)
		require.Equal(t, "ACTIVE", record.IdentityState)
	}
	rows, err := core.ListAssistants(t.Context(), project)
	require.NoError(t, err)
	require.Len(t, rows, len(results))
	agents, err := agentrepo.New(db).ListManagedAgents(t.Context(), "org-test")
	require.NoError(t, err)
	require.Len(t, agents, len(results), "each create owns one dedicated agent")
	roots, err := triggerrepo.New(db).ListTriggerInstances(t.Context(), project)
	require.NoError(t, err)
	require.Len(t, roots, len(results), "each create owns one root trigger")
	for _, root := range roots {
		resolved, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, uuid.MustParse(root.TargetRef), root.ID)
		require.NoError(t, err)
		require.Equal(t, assistantidentity.Active, resolved.State)
		require.NotNil(t, resolved.Identity)
		require.Equal(t, "org-test", resolved.Identity.OrganizationID)
		require.Equal(t, project, resolved.Identity.ProjectID)
		require.Equal(t, root.ID, resolved.Identity.TriggerID)
	}

	require.NoError(t, core.DeleteAssistant(t.Context(), project, results[0].ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	binding, err := identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: results[0].ID})
	require.NoError(t, err)
	require.True(t, binding.Deleted)
}

func TestIdentityCreationProvisioningFailureRollsBack(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_create_rollback")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-create-rollback")
	core := newProvisioningCore(t, db)
	// The user exists but has no membership, so the mandatory provision step fails.
	_, err = usersrepo.New(db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: "ineligible-user", Email: "ineligible@example.invalid", DisplayName: "Ineligible", PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = core.CreateAssistant(t.Context(), "org-test", project, "ineligible-user", "Rollback assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.ErrorIs(t, err, assistantidentity.ErrActorIneligible)
	rows, err := core.ListAssistants(t.Context(), project)
	require.NoError(t, err)
	require.Empty(t, rows)
	roots, err := triggerrepo.New(db).ListTriggerInstances(t.Context(), project)
	require.NoError(t, err)
	require.Empty(t, roots)
	agents, err := agentrepo.New(db).ListManagedAgents(t.Context(), "org-test")
	require.NoError(t, err)
	require.Empty(t, agents, "failed provisioning must not leave an orphan agent")

	// The rolled-back reservation can be retried after eligibility and authority are fixed.
	seedProvisioningAccess(t, db, project, "ineligible-user")
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: "org-test", UserID: pgtype.Text{String: "ineligible-user", Valid: true}})
	require.NoError(t, err)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "ineligible-user", "Rollback assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", record.IdentityState)
}

func TestIdentityCreationAPIUpgradeRequiresAuthorizationAndIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, ctx, project, db := newRBACServiceWithConn(t, "identity_api_upgrade")
	legacy, err := assistantrepo.New(db).CreateAssistant(ctx, assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: "org-test", CreatedByUserID: pgtype.Text{String: "user-1", Valid: true}, Name: "Legacy assistant", Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: StatusActive})
	require.NoError(t, err)
	payload := &gen.UpgradeAssistantIdentityPayload{ID: legacy.ID.String(), SessionToken: nil, ProjectSlugInput: nil}
	_, err = svc.UpgradeAssistantIdentity(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err)
	_, err = identityrepo.New(db).GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, project.String()))
	first, err := svc.UpgradeAssistantIdentity(granted, payload)
	require.NoError(t, err)
	second, err := svc.UpgradeAssistantIdentity(granted, payload)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	binding, err := identityrepo.New(db).GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, binding.Generation)
	_, err = svc.UpgradeAssistantIdentity(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err, "idempotent retry must reauthorize")
}

func TestIdentityCreationAPICreatePreservesSemantics(t *testing.T) {
	t.Parallel()
	svc, ctx, project, _ := newRBACServiceWithConn(t, "identity_api_create")
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, project.String()))
	payload := &gen.CreateAssistantPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "API assistant", Model: "openai/gpt-4o-mini", Instructions: "", Toolsets: nil, McpServers: nil, WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil}
	first, err := svc.CreateAssistant(granted, payload)
	require.NoError(t, err)
	_, err = svc.CreateAssistant(granted, payload)
	require.Error(t, err, "existing same-name uniqueness remains unchanged")
	payload.Name = "Another API assistant"
	second, err := svc.CreateAssistant(granted, payload)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	_, err = svc.CreateAssistant(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err)
}

func TestIdentityCreationManagedLegacyAndCanonicalDashboard(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_managed")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-managed")
	core := newProvisioningCore(t, db)
	legacy, err := assistantrepo.New(db).CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: "org-test", CreatedByUserID: pgtype.Text{String: "user-1", Valid: true}, Name: "Legacy managed", Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: StatusActive})
	require.NoError(t, err)
	require.NoError(t, assistantrepo.New(db).CreateProjectManagedAssistant(t.Context(), assistantrepo.CreateProjectManagedAssistantParams{ProjectID: project, AssistantID: legacy.ID}))
	ensured, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-2")
	require.NoError(t, err)
	require.Equal(t, legacy.ID, ensured.ID)
	require.Equal(t, "NEVER_CONFIGURED", ensured.IdentityState)
	_, err = identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	canonical, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	_, err = core.UpgradeAssistantIdentity(t.Context(), "org-test", project, legacy.ID, "user-2")
	require.NoError(t, err)
	reused, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	require.Equal(t, canonical, reused)
	resolution, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, legacy.ID, canonical)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)
	require.NoError(t, core.DisableManagedAssistant(t.Context(), project, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), nil))
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), db, *resolution.Identity), assistantidentity.ErrInvalidIdentity)
	fresh, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1")
	require.NoError(t, err)
	require.NotEqual(t, legacy.ID, fresh.ID)
	require.Equal(t, "ACTIVE", fresh.IdentityState)
}

func TestIdentityCreationPauseResumePreservesBindingAndDeleteTombstones(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_assistant_lifecycle")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-assistant-lifecycle")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Lifecycle assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	before, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, before.State)
	paused := StatusPaused
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), db, *before.Identity), assistantidentity.ErrInvalidIdentity)
	active := StatusActive
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil, nil, nil, nil, nil, &active)
	require.NoError(t, err)
	after, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, after.State)
	require.Equal(t, before.Identity, after.Identity)
	require.Equal(t, before.Identity.AgentID, after.Identity.AgentID)
	require.NoError(t, testIdentityService.Validate(t.Context(), db, *before.Identity))
	require.NoError(t, core.DeleteAssistant(t.Context(), project, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	deleted, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, deleted.State)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), db, *after.Identity), assistantidentity.ErrInvalidIdentity)
}

var testIdentityService = func() *assistantidentity.Service {
	service, err := assistantidentity.New("https://platform.example.invalid", false)
	if err != nil {
		panic(err)
	}
	return service
}()

func TestDisableLegacyManagedAssistantWithoutBinding(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_legacy_disable")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-legacy-disable")
	core := newProvisioningCore(t, db)
	q := assistantrepo.New(db)
	legacy, err := q.CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{ProjectID: project, OrganizationID: "org-test", CreatedByUserID: pgtype.Text{String: "user-1", Valid: true}, Name: "Legacy managed", Model: "openai/gpt-4o-mini", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: StatusActive})
	require.NoError(t, err)
	require.NoError(t, q.CreateProjectManagedAssistant(t.Context(), assistantrepo.CreateProjectManagedAssistantParams{ProjectID: project, AssistantID: legacy.ID}))
	_, err = core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	_, err = identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, core.DisableManagedAssistant(t.Context(), project, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	_, err = q.GetManagedAssistantByProject(t.Context(), project)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	assistants, err := core.ListAssistants(t.Context(), project)
	require.NoError(t, err)
	require.Empty(t, assistants)
	roots, err := triggerrepo.New(db).ListTriggerInstances(t.Context(), project)
	require.NoError(t, err)
	require.Empty(t, roots)
}

func TestIdentityPresentationIncludesAgentOwnerBarrier(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_state_agent")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-state-agent")
	core := newProvisioningCore(t, db)
	item, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Identity presentation", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	binding, err := identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{CaptureSuspended: false, OrganizationID: "org-test", ProjectID: project, AssistantID: item.ID})
	require.NoError(t, err)
	require.NoError(t, identityrepo.New(db).FixtureLatchOwner(t.Context(), identityrepo.FixtureLatchOwnerParams{OrganizationID: "org-test", AgentID: binding.OriginalAgentID}))
	rows, err := core.ListAssistants(t.Context(), project)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "TOMBSTONED", rows[0].IdentityState)
}

func TestDashboardRootPauseSendResumePreservesBinding(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "paused_dashboard_identity")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "paused-dashboard-identity")
	core := newProvisioningCore(t, db)
	record, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1")
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	before, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, before.State)
	ingestor := &fakeDashboardIngestor{core: core, assistantID: record.ID}
	core.SetDashboardIngestor(ingestor)
	q := triggerrepo.New(db)
	_, err = q.SetTriggerInstanceStatus(t.Context(), triggerrepo.SetTriggerInstanceStatusParams{ID: root, ProjectID: project, Status: StatusPaused})
	require.NoError(t, err)
	counts, err := identityrepo.New(db).FixtureAuthorityCounts(t.Context(), "org-test")
	require.NoError(t, err)
	// The management ensure path must not create a replacement either.
	_, err = core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1")
	require.NoError(t, err)
	result, err := core.SendDashboardMessage(t.Context(), project, record.ID, "user-1", uuid.Nil, "paused message", "paused-dashboard", nil, nil)
	require.ErrorContains(t, err, "dashboard ingress is not active")
	require.False(t, result.Accepted)
	require.Equal(t, uuid.Nil, ingestor.lastInstance)
	roots, err := q.ListDashboardTriggerInstances(t.Context(), triggerrepo.ListDashboardTriggerInstancesParams{ProjectID: project, TargetRef: record.ID.String()})
	require.NoError(t, err)
	require.Len(t, roots, 1)
	require.Equal(t, root, roots[0].ID)
	require.Equal(t, StatusPaused, roots[0].Status)
	afterCounts, err := identityrepo.New(db).FixtureAuthorityCounts(t.Context(), "org-test")
	require.NoError(t, err)
	require.Equal(t, counts, afterCounts)
	_, err = q.SetTriggerInstanceStatus(t.Context(), triggerrepo.SetTriggerInstanceStatusParams{ID: root, ProjectID: project, Status: StatusActive})
	require.NoError(t, err)
	result, err = core.SendDashboardMessage(t.Context(), project, record.ID, "user-1", uuid.Nil, "resumed message", "resumed-dashboard", nil, nil)
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Equal(t, root, ingestor.lastInstance)
	after, err := testIdentityService.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, before.Identity, after.Identity)
	require.NoError(t, testIdentityService.Validate(t.Context(), db, *before.Identity))
}
