package assistants

import (
	"testing"

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
	"github.com/speakeasy-api/gram/server/internal/oops"
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

func TestIdentityCreationConcurrentKeyReplayAndReservation(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_create_key")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-create-key")
	core := newProvisioningCore(t, db)
	results := make([]assistantRecord, 4)
	var group errgroup.Group
	for i := range results {
		group.Go(func() error {
			record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Keyed assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, "request-key")
			results[i] = record
			return err
		})
	}
	require.NoError(t, group.Wait())
	for _, record := range results {
		require.Equal(t, results[0].ID, record.ID)
		require.Equal(t, "ACTIVE", record.IdentityState)
	}
	rows, err := core.ListAssistants(t.Context(), project)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	agents, err := agentrepo.New(db).ListManagedAgents(t.Context(), "org-test")
	require.NoError(t, err)
	require.Len(t, agents, 1, "concurrent replay must not orphan dedicated agents")

	_, err = core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Changed payload", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, "request-key")
	require.ErrorIs(t, err, errAssistantCreateConflict)
	require.NoError(t, core.DeleteAssistant(t.Context(), project, results[0].ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	_, err = core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Keyed assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, "request-key")
	require.ErrorIs(t, err, errAssistantCreateConflict, "deleted result must not release key reservation")
	binding, err := identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{OrganizationID: "org-test", ProjectID: project, AssistantID: results[0].ID})
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
	_, err = core.CreateAssistant(t.Context(), "org-test", project, "ineligible-user", "Rollback assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, "retry-key")
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

	// The rolled-back reservation can be retried after eligibility is fixed.
	_, err = orgrepo.New(db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: "org-test", UserID: pgtype.Text{String: "ineligible-user", Valid: true}})
	require.NoError(t, err)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "ineligible-user", "Rollback assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, "retry-key")
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
	_, err = identityrepo.New(db).GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, project.String()))
	first, err := svc.UpgradeAssistantIdentity(granted, payload)
	require.NoError(t, err)
	second, err := svc.UpgradeAssistantIdentity(granted, payload)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	binding, err := identityrepo.New(db).GetAssistantBinding(ctx, identityrepo.GetAssistantBindingParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, binding.Generation)
	_, err = svc.UpgradeAssistantIdentity(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err, "idempotent retry must reauthorize")
}

func TestIdentityCreationAPICreateReplayReauthorizes(t *testing.T) {
	t.Parallel()
	svc, ctx, project, _ := newRBACServiceWithConn(t, "identity_api_create")
	granted := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, project.String()))
	key := "api-create-key"
	payload := &gen.CreateAssistantPayload{SessionToken: nil, ProjectSlugInput: nil, Name: "API assistant", Model: "openai/gpt-4o-mini", Instructions: "", Toolsets: nil, McpServers: nil, WarmTTLSeconds: nil, MaxConcurrency: nil, Status: nil, IdempotencyKey: &key}
	first, err := svc.CreateAssistant(granted, payload)
	require.NoError(t, err)
	second, err := svc.CreateAssistant(granted, payload)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	_, err = svc.CreateAssistant(authztest.WithExactGrants(t, ctx), payload)
	require.Error(t, err)
	payload.Name = "Conflicting name"
	_, err = svc.CreateAssistant(granted, payload)
	requireOopsCode(t, err, oops.CodeConflict)
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
	_, err = identityrepo.New(db).GetAssistantBinding(t.Context(), identityrepo.GetAssistantBindingParams{OrganizationID: "org-test", ProjectID: project, AssistantID: legacy.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	canonical, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	_, err = core.UpgradeAssistantIdentity(t.Context(), "org-test", project, legacy.ID, "user-2")
	require.NoError(t, err)
	reused, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, legacy.ID, legacy.Name)
	require.NoError(t, err)
	require.Equal(t, canonical, reused)
	resolution, err := assistantidentity.Resolve(t.Context(), db, "org-test", project, legacy.ID, canonical)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, resolution.State)
	require.NoError(t, core.DisableManagedAssistant(t.Context(), project, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), nil))
	require.Error(t, assistantidentity.Validate(t.Context(), db, *resolution.Identity))
	fresh, err := core.EnableManagedAssistant(t.Context(), "org-test", project, "user-1")
	require.NoError(t, err)
	require.NotEqual(t, legacy.ID, fresh.ID)
	require.Equal(t, "ACTIVE", fresh.IdentityState)
}

func TestIdentityCreationPauseResumeInvalidatesEpochAndDeleteTombstones(t *testing.T) {
	t.Parallel()
	db, err := assistantsInfra.CloneTestDatabase(t, "identity_assistant_lifecycle")
	require.NoError(t, err)
	project := newProvisioningProject(t, db, "identity-assistant-lifecycle")
	core := newProvisioningCore(t, db)
	record, err := core.CreateAssistant(t.Context(), "org-test", project, "user-1", "Lifecycle assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive)
	require.NoError(t, err)
	root, err := core.resolveDashboardTriggerInstance(t.Context(), "org-test", project, record.ID, record.Name)
	require.NoError(t, err)
	before, err := assistantidentity.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, before.State)
	paused := "paused"
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil, nil, nil, nil, nil, &paused)
	require.NoError(t, err)
	require.Error(t, assistantidentity.Validate(t.Context(), db, *before.Identity))
	active := StatusActive
	_, err = core.UpdateAssistant(t.Context(), project, record.ID, nil, nil, nil, nil, nil, nil, nil, &active)
	require.NoError(t, err)
	after, err := assistantidentity.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, after.State)
	require.Greater(t, after.Identity.AgentIdentityEpoch, before.Identity.AgentIdentityEpoch)
	require.Equal(t, before.Identity.AgentID, after.Identity.AgentID)
	require.Error(t, assistantidentity.Validate(t.Context(), db, *before.Identity))
	require.NoError(t, core.DeleteAssistant(t.Context(), project, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), nil))
	deleted, err := assistantidentity.Resolve(t.Context(), db, "org-test", project, record.ID, root)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, deleted.State)
	require.Error(t, assistantidentity.Validate(t.Context(), db, *after.Identity))
}
