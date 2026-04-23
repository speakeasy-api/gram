package skills_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestService_List_ReturnsSkillsWithActiveVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	firstSkill := seedSkill(t, ctx, ti, seedSkillParams{
		name:        "First Skill",
		slug:        "first-skill",
		description: "First description",
		skillUUID:   "skill-first",
	})
	firstAsset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "asset-first")
	firstVersion := seedSkillVersion(t, ctx, ti, firstSkill.ID, *authCtx.ProjectID, authCtx.UserID, firstAsset.ID, seedSkillVersionParams{
		contentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{Time: firstSkill.CreatedAt.Time, Valid: true},
		state:         "active",
	})
	_, err := ti.skillsRepo.SetSkillActiveVersion(ctx, skillsrepo.SetSkillActiveVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        firstSkill.ID,
		ActiveVersionID: uuid.NullUUID{
			UUID:  firstVersion.ID,
			Valid: true,
		},
	})
	require.NoError(t, err)

	secondSkill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Second Skill",
		slug: "second-skill",
	})

	result, err := ti.service.List(ctx, &gen.ListPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Skills, 2)

	bySlug := map[string]*gen.SkillEntry{}
	for _, skill := range result.Skills {
		bySlug[skill.Slug] = skill
	}

	first := bySlug["first-skill"]
	require.NotNil(t, first)
	require.Equal(t, firstSkill.ID.String(), first.ID)
	require.Equal(t, "First Skill", first.Name)
	require.Equal(t, "First description", *first.Description)
	require.Equal(t, "skill-first", *first.SkillUUID)
	require.EqualValues(t, 1, first.VersionCount)
	require.NotNil(t, first.ActiveVersion)
	require.Equal(t, firstVersion.ID.String(), first.ActiveVersion.ID)
	require.Equal(t, firstVersion.ContentSha256, first.ActiveVersion.ContentSha256)
	require.Equal(t, firstVersion.AssetFormat, first.ActiveVersion.AssetFormat)
	require.Equal(t, firstVersion.SizeBytes, first.ActiveVersion.SizeBytes)
	require.Equal(t, "Ada", *first.ActiveVersion.AuthorName)
	require.NotNil(t, first.ActiveVersion.FirstSeenAt)

	second := bySlug["second-skill"]
	require.NotNil(t, second)
	require.Equal(t, secondSkill.ID.String(), second.ID)
	require.Nil(t, second.Description)
	require.Nil(t, second.SkillUUID)
	require.EqualValues(t, 0, second.VersionCount)
	require.Nil(t, second.ActiveVersion)
}

type seedSkillParams struct {
	name        string
	slug        string
	description string
	skillUUID   string
}

func seedSkill(t *testing.T, ctx context.Context, ti *testInstance, params seedSkillParams) skillsrepo.Skill {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill, err := ti.skillsRepo.CreateSkill(ctx, skillsrepo.CreateSkillParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		Name:            params.name,
		Slug:            params.slug,
		Description:     pgtype.Text{String: params.description, Valid: params.description != ""},
		SkillUuid:       pgtype.Text{String: params.skillUUID, Valid: params.skillUUID != ""},
		ActiveVersionID: uuid.NullUUID{},
		CreatedByUserID: authCtx.UserID,
	})
	require.NoError(t, err)

	return skill
}

func seedSkillAsset(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, suffix string) assetsrepo.Asset {
	t.Helper()

	asset, err := ti.repo.CreateAsset(ctx, assetsrepo.CreateAssetParams{
		Name:          "skill-" + suffix + ".zip",
		Url:           "s3://skills/" + suffix + ".zip",
		ProjectID:     projectID,
		Sha256:        suffix + suffix + suffix + suffix + suffix + suffix + suffix + suffix,
		Kind:          "skill",
		ContentType:   "application/zip",
		ContentLength: 128,
	})
	require.NoError(t, err)

	return asset
}

type seedSkillVersionParams struct {
	contentSHA256 string
	authorName    string
	firstSeenAt   pgtype.Timestamptz
	state         string
}

func seedSkillVersion(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	skillID uuid.UUID,
	projectID uuid.UUID,
	userID string,
	assetID uuid.UUID,
	params seedSkillVersionParams,
) skillsrepo.SkillVersion {
	t.Helper()

	state := params.state
	if state == "" {
		state = "pending_review"
	}

	version, err := ti.skillsRepo.CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		AssetID:            assetID,
		ContentSha256:      params.contentSHA256,
		AssetFormat:        "zip",
		SizeBytes:          128,
		SkillBytes:         pgtype.Int8{},
		State:              state,
		CapturedByUserID:   userID,
		AuthorName:         pgtype.Text{String: params.authorName, Valid: params.authorName != ""},
		RejectedByUserID:   pgtype.Text{},
		RejectedReason:     pgtype.Text{},
		RejectedAt:         pgtype.Timestamptz{},
		FirstSeenTraceID:   pgtype.Text{},
		FirstSeenSessionID: pgtype.Text{},
		FirstSeenAt:        params.firstSeenAt,
		SkillID:            skillID,
		ProjectID:          projectID,
	})
	require.NoError(t, err)

	return version
}
