package skills_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

func TestSkillDistributionFullLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "distributed-skill", "First valid version.")
	require.True(t, created.Version.SpecValid)

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, created.Skill.ID, distribution.SkillID)
	require.Equal(t, created.Version.ID, distribution.ResolvedVersionID)
	require.Nil(t, distribution.PinnedVersionID)
	require.Nil(t, distribution.AudienceGroupIds)
	require.Equal(t, "plugin", distribution.Channel)
	require.Equal(t, ti.authContext.UserID, distribution.CreatedByUserID)

	listed, err := ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*types.SkillDistribution{distribution}, listed.Distributions)

	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("distributed-skill", "Second valid version.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, second.Version.SpecValid)
	listed, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, second.Version.ID, listed.Distributions[0].ResolvedVersionID)

	pinned, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PinnedVersionID: &created.Version.ID, AudienceGroupIds: nil,
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, distribution.ID, pinned.ID)
	require.Equal(t, distribution.CreatedAt, pinned.CreatedAt)
	require.Equal(t, distribution.CreatedByUserID, pinned.CreatedByUserID)
	require.Equal(t, created.Version.ID, *pinned.PinnedVersionID)
	require.Equal(t, created.Version.ID, pinned.ResolvedVersionID)

	status, err := ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, &types.SkillDistributionStatus{SkillID: created.Skill.ID, ResolvedVersionID: created.Version.ID, Live: 0, Stale: 0, Shadowed: 0, Degraded: 0}, status)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	listed, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, listed.Distributions)
	require.Empty(t, listed.Distributions)
	_, err = ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	recreated, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, distribution.ID, recreated.ID)
	require.Equal(t, second.Version.ID, recreated.ResolvedVersionID)
}

func TestSkillDistributionVersionValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	invalidOnly := createSkill(t, ctx, ti, "Invalid_Only", "Invalid name format.")
	require.False(t, invalidOnly.Version.SpecValid)
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: invalidOnly.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)

	valid := createSkill(t, ctx, ti, "pin-target", "Valid target.")
	other := createSkill(t, ctx, ti, "other-pin-target", "Other valid target.")
	newerInvalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: valid.Skill.ID, Content: skillManifest("Pin_Target", "Newer invalid version.", "newer"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, newerInvalid.Version.SpecValid)

	tracked, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, valid.Version.ID, tracked.ResolvedVersionID)

	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PinnedVersionID: &newerInvalid.Version.ID, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PinnedVersionID: &other.Version.ID, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
	badUUID := "not-a-uuid"
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: valid.Skill.ID, PinnedVersionID: &badUUID, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestSkillDistributionAudienceDiscoveryAndValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	groups, err := ti.service.ListDistributionAudienceGroups(ctx, &gen.ListDistributionAudienceGroupsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, groups.Groups)
	require.Empty(t, groups.Groups)

	createDirectoryGroup(t, ctx, ti, ti.authContext.ActiveOrganizationID, "group-z", "Zulu")
	createDirectoryGroup(t, ctx, ti, ti.authContext.ActiveOrganizationID, "group-a", "Alpha")
	createDirectoryGroup(t, ctx, ti, ti.authContext.ActiveOrganizationID, "group-deleted", "Deleted")
	_, err = workosrepo.New(ti.conn).DeleteDirectoryGroupByWorkOSID(ctx, workosrepo.DeleteDirectoryGroupByWorkOSIDParams{
		WorkosDeletedAt: conv.ToPGTimestamptz(time.Now()), WorkosLastEventID: conv.ToPGText("delete-event"), WorkosDirectoryGroupID: "group-deleted",
	})
	require.NoError(t, err)
	otherOrg := "other-skills-org-" + uuid.NewString()
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: otherOrg, Name: otherOrg, Slug: otherOrg, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	createDirectoryGroup(t, ctx, ti, otherOrg, "group-other-org", "Other")

	groups, err = ti.service.ListDistributionAudienceGroups(ctx, &gen.ListDistributionAudienceGroupsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*types.SkillDistributionAudienceGroup{{ID: "group-a", Name: "Alpha"}, {ID: "group-z", Name: "Zulu"}}, groups.Groups)

	created := createSkill(t, ctx, ti, "audience-skill", "Valid audience skill.")
	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: []string{"group-z", "group-a", "group-z"},
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"group-a", "group-z"}, distribution.AudienceGroupIds)

	for _, invalidAudience := range [][]string{{}, {"missing"}, {"group-deleted"}, {"group-other-org"}} {
		_, err = ti.service.Distribute(ctx, &gen.DistributePayload{
			ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: invalidAudience,
			SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
}

func TestSkillDistributionStatusReceiptMatrixAndValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "receipt-status", "First valid version.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("receipt-status", "Second valid version.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: first.Skill.ID, PinnedVersionID: &second.Version.ID, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	firstVersionID := uuid.MustParse(first.Version.ID)
	secondVersionID := uuid.MustParse(second.Version.ID)
	insertReceipt := func(user, hostname string, version uuid.NullUUID, status skills.SyncReceiptStatus) repo.SkillSyncReceipt {
		t.Helper()
		receipt, upsertErr := skills.UpsertSkillSyncReceipt(ctx, ti.repo, repo.UpsertSkillSyncReceiptParams{
			SkillVersionID: version, UserID: user, Hostname: hostname, Provider: "claude", Status: string(status), ProjectID: ti.projectID, SkillID: uuid.MustParse(first.Skill.ID),
		})
		require.NoError(t, upsertErr)
		return receipt
	}

	live := insertReceipt("live", "host-live", uuid.NullUUID{UUID: secondVersionID, Valid: true}, skills.SyncReceiptStatusApplied)
	insertReceipt("stale-version", "host-stale", uuid.NullUUID{UUID: firstVersionID, Valid: true}, skills.SyncReceiptStatusApplied)
	insertReceipt("stale-null", "host-null", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, skills.SyncReceiptStatusApplied)
	insertReceipt("shadowed", "host-shadowed", uuid.NullUUID{UUID: secondVersionID, Valid: true}, skills.SyncReceiptStatusConflictSkipped)
	insertReceipt("degraded", "host-degraded", uuid.NullUUID{UUID: secondVersionID, Valid: true}, skills.SyncReceiptStatusFSReadonly)
	_, err = ti.repo.UpsertSkillSyncReceipt(ctx, repo.UpsertSkillSyncReceiptParams{
		SkillVersionID: uuid.NullUUID{UUID: secondVersionID, Valid: true}, UserID: "future", Hostname: "host-future", Provider: "claude", Status: "future_status", ProjectID: ti.projectID, SkillID: uuid.MustParse(first.Skill.ID),
	})
	require.NoError(t, err)

	status, err := ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, &types.SkillDistributionStatus{SkillID: first.Skill.ID, ResolvedVersionID: second.Version.ID, Live: 1, Stale: 2, Shadowed: 1, Degraded: 1}, status)

	updated := insertReceipt("live", "host-live", uuid.NullUUID{UUID: secondVersionID, Valid: true}, skills.SyncReceiptStatusConflictSkipped)
	require.Equal(t, live.CreatedAt, updated.CreatedAt)
	require.True(t, updated.SyncedAt.Time.After(live.SyncedAt.Time))
	require.True(t, updated.UpdatedAt.Time.After(live.UpdatedAt.Time))
	status, err = ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, int64(0), status.Live)
	require.Equal(t, int64(2), status.Shadowed)

	_, err = skills.UpsertSkillSyncReceipt(ctx, ti.repo, repo.UpsertSkillSyncReceiptParams{
		SkillVersionID: uuid.NullUUID{}, UserID: "invalid", Hostname: "invalid", Provider: "claude", Status: "unknown", ProjectID: ti.projectID, SkillID: uuid.MustParse(first.Skill.ID),
	})
	require.Error(t, err)
	_, otherProjectID := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	_, err = skills.UpsertSkillSyncReceipt(ctx, ti.repo, repo.UpsertSkillSyncReceiptParams{
		SkillVersionID: uuid.NullUUID{UUID: secondVersionID, Valid: true}, UserID: "cross-project", Hostname: "cross-project", Provider: "claude", Status: string(skills.SyncReceiptStatusApplied), ProjectID: otherProjectID, SkillID: uuid.MustParse(first.Skill.ID),
	})
	require.Error(t, err)
}

func TestSkillDistributionProjectIsolationAndArchiveRevocation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "isolated-distribution", "Project one.")
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	otherCtx, _ := createProjectContext(t, ctx, ti, authz.ScopeSkillWrite)
	_, err = ti.service.Distribute(otherCtx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
	otherList, err := ti.service.ListDistributions(otherCtx, &gen.ListDistributionsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, otherList.Distributions)
	err = ti.service.Undistribute(otherCtx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	undistributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	_, err = skills.UpsertSkillSyncReceipt(ctx, ti.repo, repo.UpsertSkillSyncReceiptParams{
		SkillVersionID: uuid.NullUUID{}, UserID: "archived", Hostname: "archived", Provider: "claude", Status: string(skills.SyncReceiptStatusApplied), ProjectID: ti.projectID, SkillID: uuid.MustParse(created.Skill.ID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	undistributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeBefore+1, undistributeAfter)
	listed, err := ti.repo.ListActiveSkillDistributions(ctx, ti.projectID)
	require.NoError(t, err)
	require.Empty(t, listed)
	err = ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	withoutDistribution := createSkill(t, ctx, ti, "archive-without-distribution", "No distribution.")
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: withoutDistribution.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeFinal, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeAfter, undistributeFinal)
}

func TestSkillDistributionAuditSnapshotsAndNoOpDeltas(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "audit-distribution", "Sensitive summary.")
	distributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	updateBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	undistributeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)

	distribution, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	distributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, distributeBefore+1, distributeAfter)
	createRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Nil(t, createRecord.BeforeSnapshot)
	createSnapshot, err := audittest.DecodeAuditData(createRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, distribution.ID, createSnapshot["ID"])
	require.Equal(t, created.Skill.ID, createSnapshot["SkillID"])
	require.Nil(t, createSnapshot["AudienceGroupIDs"])
	require.NotContains(t, createSnapshot, "Summary")

	unchanged, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, distribution.UpdatedAt, unchanged.UpdatedAt)
	distributeNoOp, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillDistribute)
	require.NoError(t, err)
	require.Equal(t, distributeAfter, distributeNoOp)

	pinned, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: &created.Version.ID, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	updateAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	require.Equal(t, updateBefore+1, updateAfter)
	updateRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUpdateDistribution)
	require.NoError(t, err)
	updateBeforeSnapshot, err := audittest.DecodeAuditData(updateRecord.BeforeSnapshot)
	require.NoError(t, err)
	updateAfterSnapshot, err := audittest.DecodeAuditData(updateRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, updateBeforeSnapshot["PinnedVersionID"])
	require.Equal(t, created.Version.ID, updateAfterSnapshot["PinnedVersionID"])
	require.Equal(t, distribution.CreatedByUserID, pinned.CreatedByUserID)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeBefore+1, undistributeAfter)
	undistributeRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	undistributeBeforeSnapshot, err := audittest.DecodeAuditData(undistributeRecord.BeforeSnapshot)
	require.NoError(t, err)
	undistributeAfterSnapshot, err := audittest.DecodeAuditData(undistributeRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Nil(t, undistributeBeforeSnapshot["RevokedAt"])
	require.NotNil(t, undistributeAfterSnapshot["RevokedAt"])

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	undistributeNoOp, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillUndistribute)
	require.NoError(t, err)
	require.Equal(t, undistributeAfter, undistributeNoOp)
}

func TestSkillDistributionReadScopeAndWriteMutations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "distribution-rbac", "Valid.")
	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ti.projectID.String()))
	_, err := ti.service.ListDistributions(readCtx, &gen.ListDistributionsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.ListDistributionAudienceGroups(readCtx, &gen.ListDistributionAudienceGroupsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.Distribute(readCtx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(readCtx, &gen.UndistributePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PinnedVersionID: nil, AudienceGroupIds: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.GetDistributionStatus(readCtx, &gen.GetDistributionStatusPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
}
