package mcpskills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
)

func TestUpsertLinkCreatesThenUpdatesPin(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID := createSkill(t, ctx, f, f.projectID, "upserted")
	versionID := addVersion(t, ctx, f, f.projectID, skillID, skillContent("upserted", "Upserted skill."))

	for _, target := range []mcpskills.Target{
		mcpskills.MCPServerTarget(f.serverID),
		mcpskills.MetaMCPServerTarget(createMetaServer(t, ctx, f, "gateway")),
		mcpskills.OrganizationGatewayTarget(),
	} {
		created, err := mcpskills.UpsertLink(ctx, f.conn, mcpskills.NewLink{
			OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
			PinnedVersionID: nil, Target: target, CreatedByUserID: testUserID,
		})
		require.NoError(t, err, "%+v", target)
		require.False(t, created.PinnedVersionID.Valid)
		require.Equal(t, target, mcpskills.TargetOf(created.TargetKind, created.McpServerID, created.MetaMcpServerID))

		repeated, err := mcpskills.UpsertLink(ctx, f.conn, mcpskills.NewLink{
			OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
			PinnedVersionID: nil, Target: target, CreatedByUserID: testUserID,
		})
		require.NoError(t, err)
		require.Equal(t, created.ID, repeated.ID, "a repeat keeps the live link")
		require.Equal(t, created.UpdatedAt, repeated.UpdatedAt, "a repeat with the same pin changes nothing")

		pinned, err := mcpskills.UpsertLink(ctx, f.conn, mcpskills.NewLink{
			OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
			PinnedVersionID: &versionID, Target: target, CreatedByUserID: testUserID,
		})
		require.NoError(t, err)
		require.Equal(t, created.ID, pinned.ID)
		require.Equal(t, uuid.NullUUID{UUID: versionID, Valid: true}, pinned.PinnedVersionID)
	}
}

func TestUpsertLinkRejectsInvalidTarget(t *testing.T) {
	t.Parallel()

	ctx, f := newFixture(t)
	skillID := createSkill(t, ctx, f, f.projectID, "nowhere")

	_, err := mcpskills.UpsertLink(ctx, f.conn, mcpskills.NewLink{
		OrganizationID: f.organizationID, ProjectID: f.projectID, SkillID: skillID,
		PinnedVersionID: nil, Target: mcpskills.Target{Kind: mcpskills.TargetMCPServer, ID: uuid.Nil}, CreatedByUserID: testUserID,
	})
	require.ErrorIs(t, err, mcpskills.ErrInvalidTarget)
}
