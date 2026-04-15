package skills_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestService_Get_ReturnsSkillBySlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name:        "Registry Skill",
		slug:        "registry-skill",
		description: "Registry description",
		skillUUID:   "skill-registry",
	})

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "registry-skill")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{Time: skill.CreatedAt.Time, Valid: true},
	})

	updatedSkill, err := ti.skillsRepo.SetSkillActiveVersion(ctx, skillsrepo.SetSkillActiveVersionParams{
		ProjectID:       skill.ProjectID,
		ID:              skill.ID,
		ActiveVersionID: uuid.NullUUID{UUID: version.ID, Valid: true},
	})
	require.NoError(t, err)

	result, err := ti.service.Get(ctx, &gen.GetPayload{
		Slug:             gentypes.Slug(skill.Slug),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.Equal(t, updatedSkill.ID.String(), result.ID)
	require.Equal(t, "Registry Skill", result.Name)
	require.Equal(t, "registry-skill", result.Slug)
	require.Equal(t, "Registry description", *result.Description)
	require.Equal(t, "skill-registry", *result.SkillUUID)
	require.NotNil(t, result.ActiveVersionID)
	require.Equal(t, version.ID.String(), *result.ActiveVersionID)
	require.Equal(t, updatedSkill.CreatedAt.Time.Format(time.RFC3339), result.CreatedAt)
	require.Equal(t, updatedSkill.UpdatedAt.Time.Format(time.RFC3339), result.UpdatedAt)
}

func TestService_Get_ReturnsNotFoundForUnknownSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	result, err := ti.service.Get(ctx, &gen.GetPayload{
		Slug:             gentypes.Slug("missing-skill"),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}
