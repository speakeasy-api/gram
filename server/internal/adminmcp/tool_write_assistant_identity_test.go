package adminmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func seedLegacyAssistant(t *testing.T, f writeFixture, organizationID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "assistants", Slug: "assistants-" + uuid.NewString()[:8], OrganizationID: organizationID})
	require.NoError(t, err)
	creator := "user_" + uuid.NewString()
	_, err = usersrepo.New(f.db).UpsertUser(t.Context(), usersrepo.UpsertUserParams{ID: creator, Email: creator + "@example.invalid", DisplayName: creator, PhotoUrl: pgtype.Text{}, Admin: false})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpsertOrganizationUserRelationship(t.Context(), orgrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: organizationID, UserID: pgtype.Text{String: creator, Valid: true}})
	require.NoError(t, err)
	row, err := assistantrepo.New(f.db).CreateAssistant(t.Context(), assistantrepo.CreateAssistantParams{
		ProjectID: project.ID, OrganizationID: organizationID, CreatedByUserID: pgtype.Text{String: creator, Valid: true},
		Name: "Legacy", Model: "openai/gpt-4o-mini", Instructions: "", WarmTtlSeconds: 300, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	return project.ID, row.ID
}

func TestAssistantIdentityMigrationApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_assistant_identity")
	writer := &assistantIdentityMigrationWriter{runner: f.runner, identities: assistantidentity.New("https://platform.example.invalid", audit.NewLogger()), writes: WriteConfig{Enabled: true}}
	ctx := writeContext(t, f)
	project, assistant := seedLegacyAssistant(t, f, f.orgA)
	foreignProject, foreign := seedLegacyAssistant(t, f, f.orgB)

	receipt, err := writer.apply(ctx, MigrateAssistantIdentitiesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Equal(t, int64(1), receipt.Migrated)
	require.Equal(t, []assistantIdentityMigrationResult{{AssistantID: assistant.String(), ProjectID: project.String(), Outcome: "migrated"}}, receipt.Results)
	states, err := assistantidentity.States(t.Context(), f.db, project, []uuid.UUID{assistant})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Active, states[assistant].State)
	provisioned, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), f.db, audit.ActionAssistantIdentityProvision, assistant.String())
	require.NoError(t, err)
	require.Equal(t, new(string(audit.SurfaceAdminMCP)), provisioned.ActingSurface)
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationMigrateAssistantIdentities, f.orgA, "executed"))

	again, err := writer.apply(ctx, MigrateAssistantIdentitiesInput{OrganizationID: f.orgA})
	require.NoError(t, err)
	require.Zero(t, again.Migrated)
	require.Equal(t, int64(1), again.AlreadyMigrated)

	foreignStates, err := assistantidentity.States(t.Context(), f.db, foreignProject, []uuid.UUID{foreign})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.NeverConfigured, foreignStates[foreign].State)

	_, err = writer.apply(ctx, MigrateAssistantIdentitiesInput{OrganizationID: "org_missing_" + uuid.NewString()})
	require.ErrorContains(t, err, "no organization with that ID")
}
