package skills_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// connectMarketplace gives the project the GitHub connection that makes its
// plugins published packages.
func connectMarketplace(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID) {
	t.Helper()

	_, err := pluginsrepo.New(ti.conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                projectID,
		InstallationID:           1,
		RepoOwner:                "marketplace-owner",
		RepoName:                 "marketplace-repo",
		MarketplaceToken:         pgtype.Text{},
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)
}

// distributeToPlugin distributes the skill to a fresh plugin and returns the
// publisher's signal count afterwards, so a test can assert only on what its
// own write adds.
func distributeToPlugin(t *testing.T, ctx context.Context, ti *testInstance, skill *gen.RecordSkillResult, pluginName string, pinnedVersionID *string) int {
	t.Helper()

	plugin := createPlugin(t, ctx, ti, ti.projectID, pluginName)
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: skill.Skill.ID, PluginID: new(plugin.ID.String()), PinnedVersionID: pinnedVersionID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	return len(ti.publisher.recorded())
}

func addSkillVersion(t *testing.T, ctx context.Context, ti *testInstance, skill *gen.RecordSkillResult, description string) *gen.RecordSkillResult {
	t.Helper()

	result, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: skill.Skill.ID, Content: skillManifest(skill.Skill.Name, description, description),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, result.CreatedVersion)
	return result
}

func TestSkillAddVersionRepublishesPluginTrackingLatest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-add-version", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-add-version", nil)

	addSkillVersion(t, ctx, ti, created, "Second.")

	require.Len(t, ti.publisher.recorded(), before+1)
	require.Equal(t, ti.projectID, ti.publisher.recorded()[before])
}

func TestSkillCreateAsNewVersionRepublishesPluginTrackingLatest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-create-version", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-create-version", nil)

	result := createSkill(t, ctx, ti, "republish-create-version", "Second.")
	require.False(t, result.CreatedSkill)
	require.True(t, result.CreatedVersion)

	require.Len(t, ti.publisher.recorded(), before+1)
}

func TestSkillAddVersionSkipsSkillWithoutPluginDistribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	carried := createSkill(t, ctx, ti, "republish-carried", "First.")
	before := distributeToPlugin(t, ctx, ti, carried, "republish-carried", nil)
	uncarried := createSkill(t, ctx, ti, "republish-uncarried", "First.")

	addSkillVersion(t, ctx, ti, uncarried, "Second.")

	require.Len(t, ti.publisher.recorded(), before, "a skill no plugin carries changes no package")
}

func TestSkillAddVersionSkipsProjectWithoutMarketplace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "republish-unconnected", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-unconnected", nil)

	addSkillVersion(t, ctx, ti, created, "Second.")

	require.Len(t, ti.publisher.recorded(), before, "a project without a marketplace has nothing to republish")
}

func TestSkillAddVersionSkipsPinnedDistribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-pinned", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-pinned", &created.Version.ID)

	addSkillVersion(t, ctx, ti, created, "Second.")

	require.Len(t, ti.publisher.recorded(), before, "a pinned distribution keeps packaging the pinned version")
}

func TestSkillAddVersionReplaySkipsUnchangedContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-replay", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-replay", nil)

	replayed, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: created.Version.Content,
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, replayed.CreatedVersion)

	require.Len(t, ti.publisher.recorded(), before, "re-recording the current version changes no package")
}

func TestSkillApproveSuggestionRepublishesOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-approve", "Base.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-approve", nil)
	suggestion := createSuggestion(t, ti, created, skillManifest(created.Skill.Name, "Proposed.", "proposal"), "rationale")

	result, err := ti.service.ApproveSuggestion(ctx, &gen.ApproveSuggestionPayload{
		ID: suggestion.ID.String(), Content: nil, ChangeIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "applied", result.Outcome)

	require.Len(t, ti.publisher.recorded(), before+1)
}

func TestSkillApproveAllSuggestionsRepublishesOncePerProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	first := createSkill(t, ctx, ti, "republish-bulk-first", "Base.")
	distributeToPlugin(t, ctx, ti, first, "republish-bulk-first", nil)
	second := createSkill(t, ctx, ti, "republish-bulk-second", "Base.")
	before := distributeToPlugin(t, ctx, ti, second, "republish-bulk-second", nil)
	createSuggestion(t, ti, first, skillManifest(first.Skill.Name, "Proposed.", "first proposal"), "rationale")
	createSuggestion(t, ti, second, skillManifest(second.Skill.Name, "Proposed.", "second proposal"), "rationale")

	result, err := ti.service.ApproveAllSuggestions(ctx, &gen.ApproveAllSuggestionsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Items, 2)
	for _, item := range result.Items {
		require.Equal(t, "applied", item.Outcome)
	}

	require.Len(t, ti.publisher.recorded(), before+1, "a bulk approval republishes the project once")
}

func TestSkillRestoreVersionRepublishesPluginTrackingLatest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-restore", "First.")
	addSkillVersion(t, ctx, ti, created, "Second.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-restore", nil)

	_, err := ti.service.RestoreVersion(ctx, &gen.RestoreVersionPayload{
		ID: created.Skill.ID, VersionID: created.Version.ID,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Len(t, ti.publisher.recorded(), before+1)
}

func TestSkillRenameRepublishesPinnedDistribution(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-rename", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-rename", &created.Version.ID)

	_, err := ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: created.Skill.Name, DisplayName: "Display only",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, ti.publisher.recorded(), before, "display metadata never reaches a package")

	_, err = ti.service.Update(ctx, &gen.UpdatePayload{
		ID: created.Skill.ID, Name: "republish-renamed", DisplayName: "Display only",
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, ti.publisher.recorded(), before+1, "the name is the skill's package directory")
}

func TestSkillArchiveRepublishesCarriedSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connectMarketplace(t, ctx, ti, ti.projectID)
	created := createSkill(t, ctx, ti, "republish-archive", "First.")
	before := distributeToPlugin(t, ctx, ti, created, "republish-archive", &created.Version.ID)
	uncarried := createSkill(t, ctx, ti, "republish-archive-uncarried", "First.")

	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{
		ID: uncarried.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	}))
	require.Len(t, ti.publisher.recorded(), before)

	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{
		ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	}))
	require.Len(t, ti.publisher.recorded(), before+1)
}
