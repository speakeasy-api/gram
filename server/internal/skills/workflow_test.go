package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestService_ListVersions_ReturnsVersionsForSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Workflow Skill",
		slug: "workflow-skill",
	})
	assetA := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "workflow-a")
	assetB := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "workflow-b")

	versionA := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, assetA.ID, seedSkillVersionParams{
		contentSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})
	versionB := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, assetB.ID, seedSkillVersionParams{
		contentSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		authorName:    "Grace",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	result, err := ti.service.ListVersions(ctx, &gen.ListVersionsPayload{
		SkillID:          skill.ID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Versions, 2)
	require.Equal(t, versionB.ID.String(), result.Versions[0].ID)
	require.Equal(t, versionA.ID.String(), result.Versions[1].ID)
}

func TestService_ListPending_ReturnsOnlyPendingVersions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	pendingSkill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Pending Skill",
		slug: "pending-skill",
	})
	pendingAsset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "pending")
	pendingVersion := seedSkillVersion(t, ctx, ti, pendingSkill.ID, *authCtx.ProjectID, authCtx.UserID, pendingAsset.ID, seedSkillVersionParams{
		contentSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		authorName:    "Pending Author",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	activeSkill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Active Skill",
		slug: "active-skill",
	})
	activeAsset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "active")
	_ = seedSkillVersion(t, ctx, ti, activeSkill.ID, *authCtx.ProjectID, authCtx.UserID, activeAsset.ID, seedSkillVersionParams{
		contentSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		authorName:    "Active Author",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "active",
	})

	result, err := ti.service.ListPending(ctx, &gen.ListPendingPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Skills, 1)
	require.Equal(t, pendingSkill.ID.String(), result.Skills[0].Skill.ID)
	require.Len(t, result.Skills[0].Versions, 1)
	require.Equal(t, pendingVersion.ID.String(), result.Skills[0].Versions[0].ID)
	require.Equal(t, "pending_review", result.Skills[0].Versions[0].State)
}

func TestService_ApproveVersion_ActivatesVersionAndSupersedesPriorActive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Approval Skill",
		slug: "approval-skill",
	})
	assetA := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "approve-a")
	assetB := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "approve-b")

	versionA := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, assetA.ID, seedSkillVersionParams{
		contentSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
	})
	_, err := ti.skillsRepo.UpdateSkillVersionState(ctx, skillsrepo.UpdateSkillVersionStateParams{
		State:     "active",
		ID:        versionA.ID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = ti.skillsRepo.SetSkillActiveVersion(ctx, skillsrepo.SetSkillActiveVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skill.ID,
		ActiveVersionID: uuid.NullUUID{
			UUID:  versionA.ID,
			Valid: true,
		},
	})
	require.NoError(t, err)

	versionB := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, assetB.ID, seedSkillVersionParams{
		contentSHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		authorName:    "Grace",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	result, err := ti.service.ApproveVersion(ctx, &gen.ApproveVersionPayload{
		VersionID:        versionB.ID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, versionB.ID.String(), result.ID)
	require.Equal(t, "active", result.State)

	versionAAfter, err := ti.skillsRepo.GetSkillVersion(ctx, skillsrepo.GetSkillVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        versionA.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "superseded", versionAAfter.State)

	skillAfter, err := ti.skillsRepo.GetSkill(ctx, skillsrepo.GetSkillParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skill.ID,
	})
	require.NoError(t, err)
	require.True(t, skillAfter.ActiveVersionID.Valid)
	require.Equal(t, versionB.ID, skillAfter.ActiveVersionID.UUID)
}

func TestService_SupersedeVersion_UpdatesState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Supersede Skill",
		slug: "supersede-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "supersede")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "1111111111111111111111111111111111111111111111111111111111111111",
		authorName:    "Linus",
		firstSeenAt:   pgtype.Timestamptz{},
	})
	_, err := ti.skillsRepo.UpdateSkillVersionState(ctx, skillsrepo.UpdateSkillVersionStateParams{
		State:     "active",
		ID:        version.ID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = ti.skillsRepo.SetSkillActiveVersion(ctx, skillsrepo.SetSkillActiveVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skill.ID,
		ActiveVersionID: uuid.NullUUID{
			UUID:  version.ID,
			Valid: true,
		},
	})
	require.NoError(t, err)

	result, err := ti.service.SupersedeVersion(ctx, &gen.SupersedeVersionPayload{
		VersionID:        version.ID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, version.ID.String(), result.ID)
	require.Equal(t, "superseded", result.State)

	skillAfter, err := ti.skillsRepo.GetSkill(ctx, skillsrepo.GetSkillParams{
		ProjectID: *authCtx.ProjectID,
		ID:        skill.ID,
	})
	require.NoError(t, err)
	require.False(t, skillAfter.ActiveVersionID.Valid)
}

func TestService_ApproveVersion_RejectsNonPendingVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Already Active Skill",
		slug: "already-active-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "already-active")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "active",
	})

	_, err := ti.service.ApproveVersion(ctx, &gen.ApproveVersionPayload{
		VersionID:        version.ID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestService_RejectVersion_UpdatesStateAndReason(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Reject Skill",
		slug: "reject-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "reject")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "3333333333333333333333333333333333333333333333333333333333333333",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	reason := "Contains organization-specific secrets"
	result, err := ti.service.RejectVersion(ctx, &gen.RejectVersionPayload{
		VersionID:        version.ID.String(),
		Reason:           reason,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, version.ID.String(), result.ID)
	require.Equal(t, "rejected", result.State)
	require.NotNil(t, result.RejectedByUserID)
	require.Equal(t, authCtx.UserID, *result.RejectedByUserID)
	require.NotNil(t, result.RejectedReason)
	require.Equal(t, reason, *result.RejectedReason)
	require.NotNil(t, result.RejectedAt)

	versionAfter, err := ti.skillsRepo.GetSkillVersion(ctx, skillsrepo.GetSkillVersionParams{
		ProjectID: *authCtx.ProjectID,
		ID:        version.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "rejected", versionAfter.State)
	require.True(t, versionAfter.RejectedByUserID.Valid)
	require.Equal(t, authCtx.UserID, versionAfter.RejectedByUserID.String)
	require.True(t, versionAfter.RejectedReason.Valid)
	require.Equal(t, reason, versionAfter.RejectedReason.String)
	require.True(t, versionAfter.RejectedAt.Valid)
}

func TestService_RejectVersion_RejectsNonPendingVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Reject Non Pending Skill",
		slug: "reject-non-pending-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "reject-non-pending")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "4444444444444444444444444444444444444444444444444444444444444444",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "active",
	})

	reason := "Rejected by reviewer"
	_, err := ti.service.RejectVersion(ctx, &gen.RejectVersionPayload{
		VersionID:        version.ID.String(),
		Reason:           reason,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)
}

func TestService_RejectVersion_RejectsMissingReason(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Reject Missing Reason Skill",
		slug: "reject-missing-reason-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "reject-missing-reason")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "5555555555555555555555555555555555555555555555555555555555555555",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	_, err := ti.service.RejectVersion(ctx, &gen.RejectVersionPayload{
		VersionID:        version.ID.String(),
		Reason:           "",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_RejectVersion_RejectsWhitespaceReason(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestSkillsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	skill := seedSkill(t, ctx, ti, seedSkillParams{
		name: "Reject Whitespace Reason Skill",
		slug: "reject-whitespace-reason-skill",
	})
	asset := seedSkillAsset(t, ctx, ti, *authCtx.ProjectID, "reject-whitespace-reason")
	version := seedSkillVersion(t, ctx, ti, skill.ID, *authCtx.ProjectID, authCtx.UserID, asset.ID, seedSkillVersionParams{
		contentSHA256: "6666666666666666666666666666666666666666666666666666666666666666",
		authorName:    "Ada",
		firstSeenAt:   pgtype.Timestamptz{},
		state:         "pending_review",
	})

	reason := "   \n  \t "
	_, err := ti.service.RejectVersion(ctx, &gen.RejectVersionPayload{
		VersionID:        version.ID.String(),
		Reason:           reason,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}
