package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func ensureAssistantTestOrganization(t *testing.T, conn skillsrepo.DBTX) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: "org-test", Name: "Test organization", Slug: "org-test", WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
}

func createSkillAttachmentFixture(t *testing.T, conn skillsrepo.DBTX, projectID, assistantID uuid.UUID, name, userID string) (skillsrepo.Skill, skillsrepo.SkillVersion) {
	t.Helper()
	queries := skillsrepo.New(conn)
	skill, err := queries.CreateSkill(t.Context(), skillsrepo.CreateSkillParams{
		ProjectID: projectID, Name: name, DisplayName: name, Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	version, err := queries.CreateSkillVersion(t.Context(), skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: " + name + "\ndescription: first\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "first", Valid: true},
		Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: userID,
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	_, err = queries.CreateSkillDistribution(t.Context(), skillsrepo.CreateSkillDistributionParams{
		PluginID: uuid.NullUUID{}, AssistantID: uuid.NullUUID{UUID: assistantID, Valid: true},
		PinnedVersionID: uuid.NullUUID{}, Channel: "assistant", CreatedByUserID: userID,
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	return skill, version
}

func TestAssistantSkillHydrationTracksLatestAndPin(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_hydration")
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	emptyView, err := toHTTPAssistant(record)
	require.NoError(t, err)
	require.NotNil(t, emptyView.Skills)
	require.Empty(t, emptyView.Skills)

	skill, first := createSkillAttachmentFixture(t, conn, projectID, record.ID, "hydrated-skill", "user-test")

	got, err := svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Len(t, got.Skills, 1)
	require.Equal(t, skill.ID, got.Skills[0].SkillID)
	require.Equal(t, first.ID, got.Skills[0].ResolvedVersionID)

	view, err := toHTTPAssistant(got)
	require.NoError(t, err)
	require.Len(t, view.Skills, 1)
	require.Equal(t, skill.ID.String(), view.Skills[0].SkillID)
	require.Nil(t, view.Skills[0].PinnedVersionID)
	require.Equal(t, first.ID.String(), view.Skills[0].ResolvedVersionID)

	listed, err := svc.core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Len(t, listed[0].Skills, 1)

	second, err := skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: hydrated-skill\ndescription: second\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "second", Valid: true},
		Metadata: []byte(`{}`), SpecValid: true, ValidationErrors: []byte(`[]`), CreatedByUserID: "user-test",
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, got.Skills[0].ResolvedVersionID)

	_, err = skillsrepo.New(conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content:         "---\nname: Hydrated_Skill\ndescription: invalid\n---\n\nbody\n",
		CanonicalSha256: uuid.NewString(), RawSha256: uuid.NewString(), Description: pgtype.Text{String: "invalid", Valid: true},
		Metadata: []byte(`{}`), SpecValid: false, ValidationErrors: []byte(`[]`), CreatedByUserID: "user-test",
		ProjectID: projectID, SkillID: skill.ID,
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, got.Skills[0].ResolvedVersionID)

	updatedName := "Updated skill assistant"
	updated, err := svc.core.UpdateAssistant(ctx, projectID, record.ID, &updatedName, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, updated.Skills, 1)
	require.Equal(t, second.ID, updated.Skills[0].ResolvedVersionID)

	_, err = skillsrepo.New(conn).UpdateSkillDistribution(ctx, skillsrepo.UpdateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: first.ID, Valid: true}, ProjectID: projectID, SkillID: skill.ID,
		PluginID: uuid.NullUUID{}, AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true}, Channel: "assistant",
	})
	require.NoError(t, err)
	got, err = svc.core.GetAssistant(ctx, projectID, record.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got.Skills[0].ResolvedVersionID)
	require.Equal(t, first.ID, got.Skills[0].PinnedVersionID.UUID)
}

func TestDeleteAssistantRevokesAndAuditsSkillAttachments(t *testing.T) {
	t.Parallel()

	svc, ctx, projectID, conn := newRBACServiceWithConn(t, "assistant_skill_delete")
	ensureAssistantTestOrganization(t, conn)
	record, err := svc.core.CreateAssistant(ctx, "org-test", projectID, "user-test", "Delete skill assistant", "test-model", "", nil, nil, 60, 1, StatusActive)
	require.NoError(t, err)
	skill, _ := createSkillAttachmentFixture(t, conn, projectID, record.ID, "delete-skill", "user-test")

	before, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, svc.core.DeleteAssistant(ctx, projectID, record.ID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-test"), nil))
	after, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = skillsrepo.New(conn).GetActiveSkillDistributionRecord(ctx, skillsrepo.GetActiveSkillDistributionRecordParams{
		ProjectID: projectID, SkillID: skill.ID, PluginID: uuid.NullUUID{},
		AssistantID: uuid.NullUUID{UUID: record.ID, Valid: true}, Channel: "assistant",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	details, err := skillsrepo.New(conn).GetSkillDetails(ctx, skillsrepo.GetSkillDetailsParams{ProjectID: projectID, SkillID: skill.ID})
	require.NoError(t, err)
	require.Zero(t, details.AssistantCount)

	entry, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	snapshot, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, record.ID.String(), snapshot["AssistantID"])
}

func TestDisableManagedAssistantRevokesAndAuditsSkillAttachments(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "managed_assistant_skill_disable")
	require.NoError(t, err)
	projectID := newProvisioningProject(t, conn, "managed-skill-disable")
	ensureAssistantTestOrganization(t, conn)
	core := newProvisioningCore(t, conn)
	record, err := core.EnableManagedAssistant(t.Context(), "org-test", projectID, "user-test")
	require.NoError(t, err)
	createSkillAttachmentFixture(t, conn, projectID, record.ID, "managed-delete-skill", "user-test")

	before, err := audittest.AuditLogCountByAction(t.Context(), conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, core.DisableManagedAssistant(t.Context(), projectID, urn.NewPrincipal(urn.PrincipalTypeUser, "user-test"), nil))
	after, err := audittest.AuditLogCountByAction(t.Context(), conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
}
