package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type assistantMigrationOrg struct {
	t       *testing.T
	db      *pgxpool.Pool
	id      string
	project uuid.UUID
}

func newAssistantMigrationOrg(t *testing.T, db *pgxpool.Pool, slug string) *assistantMigrationOrg {
	t.Helper()
	o := &assistantMigrationOrg{t: t, db: db, id: "org_" + uuid.NewString()}
	seedOrg(t, t.Context(), db, orgFixture{id: o.id, name: slug, slug: slug, accountType: "enterprise"})
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: slug, Slug: slug, OrganizationID: o.id})
	require.NoError(t, err)
	o.project = project.ID
	return o
}

// member seeds a user, optionally as an active member of the organization.
func (o *assistantMigrationOrg) member(active bool) string {
	o.t.Helper()
	id := "user_" + uuid.NewString()
	_, err := usersrepo.New(o.db).UpsertUser(o.t.Context(), usersrepo.UpsertUserParams{ID: id, Email: id + "@example.invalid", DisplayName: id, PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(o.t, err)
	_, err = orgrepo.New(o.db).UpsertOrganizationUserRelationship(o.t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: o.id, UserID: pgtype.Text{String: id, Valid: true}})
	require.NoError(o.t, err)
	if !active {
		require.NoError(o.t, orgrepo.New(o.db).DeleteOrganizationUserRelationship(o.t.Context(), orgrepo.DeleteOrganizationUserRelationshipParams{OrganizationID: o.id, UserID: pgtype.Text{String: id, Valid: true}}))
	}
	return id
}

// legacyAssistant creates an assistant without an identity, as assistants
// created before identity provisioning were.
func (o *assistantMigrationOrg) legacyAssistant(name, creator string) uuid.UUID {
	o.t.Helper()
	row, err := assistantrepo.New(o.db).CreateAssistant(o.t.Context(), assistantrepo.CreateAssistantParams{
		ProjectID: o.project, OrganizationID: o.id, CreatedByUserID: pgtype.Text{String: creator, Valid: creator != ""},
		Name: name, Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(o.t, err)
	return row.ID
}

func newAssistantMigrationService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	_, svc, db := newTestAdminService(t)
	svc.SetAssistantIdentities(assistantidentity.New("https://platform.example.invalid", audit.NewLogger()))
	return svc, db
}

func outcomesByAssistant(run *gen.AdminAssistantIdentityMigrationRun) map[string]string {
	outcomes := make(map[string]string, len(run.Results))
	for _, result := range run.Results {
		outcomes[result.AssistantID] = result.Outcome
	}
	return outcomes
}

func TestMigrateOrganizationAssistantIdentities(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "staff@example.invalid", Name: "Staff Member", HD: testAdminHD})

	org := newAssistantMigrationOrg(t, db, "assistant-identity-migration")
	owner := org.member(true)
	departed := org.member(false)
	legacy := org.legacyAssistant("Legacy", owner)
	orphaned := org.legacyAssistant("Orphaned", departed)
	unattributed := org.legacyAssistant("Unattributed", "")
	configured := org.legacyAssistant("Configured", owner)
	tx, err := db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; provisioning uses generated SQLc methods.
	require.NoError(t, err)
	require.NoError(t, svc.assistantIdentities.Provision(t.Context(), tx, assistantidentity.ProvisionParams{OrganizationID: org.id, ProjectID: org.project, AssistantID: configured, ActorUserID: owner, Staff: nil}))
	require.NoError(t, tx.Commit(t.Context()))

	other := newAssistantMigrationOrg(t, db, "assistant-identity-migration-other")
	untouched := other.legacyAssistant("Other organization", other.member(true))

	listed, err := svc.ListOrganizationAssistants(ctx, &gen.ListOrganizationAssistantsPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Len(t, listed.Assistants, 4)
	creatorActive := map[string]bool{}
	for _, assistant := range listed.Assistants {
		creatorActive[assistant.ID] = assistant.CreatorActive
	}
	require.Equal(t, map[string]bool{legacy.String(): true, orphaned.String(): false, unattributed.String(): false, configured.String(): true}, creatorActive)

	first, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		legacy.String():       assistantIdentityMigrated,
		orphaned.String():     assistantIdentityNeedsOwner,
		unattributed.String(): assistantIdentityNeedsOwner,
		configured.String():   assistantIdentityAlreadyMigrated,
	}, outcomesByAssistant(first))
	require.Equal(t, int64(1), first.Migrated)
	require.Equal(t, int64(2), first.NeedsOwner)
	require.Equal(t, int64(1), first.AlreadyMigrated)
	require.Zero(t, first.Failed)

	states, err := assistantidentity.States(t.Context(), db, org.project, []uuid.UUID{legacy, orphaned, unattributed})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, states[legacy].State)
	require.Equal(t, assistantidentity.NeverConfigured, states[orphaned].State)
	require.Equal(t, assistantidentity.NeverConfigured, states[unattributed].State)
	agent, err := agentrepo.New(db).GetAgentByID(t.Context(), agentrepo.GetAgentByIDParams{OrganizationID: org.id, ID: *states[legacy].AgentID})
	require.NoError(t, err)
	require.Equal(t, owner, agent.OwnerUserID, "the creator owns the new agent, never staff")

	provisioned, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), db, audit.ActionAssistantIdentityProvision, legacy.String())
	require.NoError(t, err)
	require.Equal(t, "staff-subject", provisioned.ActorID, "the staff member is the audit actor")

	again, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.Migrated)
	require.Equal(t, int64(2), again.AlreadyMigrated)
	require.Equal(t, int64(2), again.NeedsOwner)
	rerun, err := assistantidentity.States(t.Context(), db, org.project, []uuid.UUID{legacy})
	require.NoError(t, err)
	require.Equal(t, states[legacy].AgentID, rerun[legacy].AgentID, "a rerun keeps the agent")

	otherStates, err := assistantidentity.States(t.Context(), db, other.project, []uuid.UUID{untouched})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.NeverConfigured, otherStates[untouched].State, "another organization is untouched")
}

func TestMigrateOrganizationAssistantIdentitiesUnavailableWithoutIssuer(t *testing.T) {
	t.Parallel()
	_, svc, db := newTestAdminService(t)
	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{SessionID: "staff-session", OIDCSubject: "staff-subject", Email: "", Name: "", HD: testAdminHD})
	org := newAssistantMigrationOrg(t, db, "assistant-identity-unavailable")

	_, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeUnavailable, shareable.Code)
}
