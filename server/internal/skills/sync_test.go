package skills_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func syncPayload(hostname string, installed []*gen.SyncSkillInstalled, exceptions []*gen.SyncSkillException) *gen.SyncPayload {
	return &gen.SyncPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		IdempotencyKey:   nil,
		Hostname:         hostname,
		Provider:         "claude",
		Installed:        installed,
		Exceptions:       exceptions,
	}
}

func distributeSkill(t *testing.T, ctx context.Context, ti *testInstance, skillID string, pinnedVersionID *string, audience []string) {
	t.Helper()
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{
		ID:               skillID,
		PinnedVersionID:  pinnedVersionID,
		AudienceGroupIds: audience,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

func machineReceipts(t *testing.T, ctx context.Context, ti *testInstance, hostname string) []repo.SkillSyncReceipt {
	t.Helper()
	receipts, err := ti.repo.ListSkillSyncReceiptsForMachine(ctx, repo.ListSkillSyncReceiptsForMachineParams{
		ProjectID: ti.projectID,
		UserID:    ti.authContext.UserID,
		Hostname:  hostname,
		Provider:  "claude",
	})
	require.NoError(t, err)
	return receipts
}

func TestSkillSyncHappyMatchingAndReplay(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	alpha := createSkill(t, ctx, ti, "sync-alpha", "Alpha description.")
	beta := createSkill(t, ctx, ti, "sync-beta", "Beta description.")
	distributeSkill(t, ctx, ti, beta.Skill.ID, nil, nil)
	distributeSkill(t, ctx, ti, alpha.Skill.ID, nil, nil)

	result, err := ti.service.Sync(ctx, syncPayload("happy-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, []string{"sync-alpha", "sync-beta"}, []string{result.Updates[0].Name, result.Updates[1].Name})
	require.Equal(t, alpha.Version.RawSha256, result.Updates[0].RawSha256)
	require.Equal(t, skillManifest("sync-alpha", "Alpha description.", "# sync-alpha"), result.Updates[0].Content)
	require.Equal(t, "Alpha description.", *result.Updates[0].Description)
	require.NotNil(t, result.Removals)
	require.Empty(t, result.Removals)

	installed := []*gen.SyncSkillInstalled{
		{Name: "sync-beta", RawSha256: beta.Version.RawSha256},
		{Name: "sync-alpha", RawSha256: alpha.Version.RawSha256},
	}
	result, err = ti.service.Sync(ctx, syncPayload("happy-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.NotNil(t, result.Updates)
	require.Empty(t, result.Updates)
	require.Empty(t, result.Removals)
	require.Len(t, machineReceipts(t, ctx, ti, "happy-host"), 2)

	replayed, err := ti.service.Sync(ctx, syncPayload("happy-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, result, replayed)
	require.Len(t, machineReceipts(t, ctx, ti, "happy-host"), 2)

	status, err := ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{
		ID: alpha.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Live)
}

func TestSkillSyncTrackedPinnedAndInvalidLatest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "sync-versioned", "First.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("sync-versioned", "Second.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	distributeSkill(t, ctx, ti, first.Skill.ID, &first.Version.ID, nil)

	result, err := ti.service.Sync(ctx, syncPayload("version-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Len(t, result.Updates, 1)
	require.Equal(t, first.Version.RawSha256, result.Updates[0].RawSha256)

	distributeSkill(t, ctx, ti, first.Skill.ID, nil, nil)
	invalid, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("Sync_Versioned", "Invalid latest.", "invalid"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, invalid.Version.SpecValid)

	result, err = ti.service.Sync(ctx, syncPayload("version-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Len(t, result.Updates, 1)
	require.Equal(t, second.Version.RawSha256, result.Updates[0].RawSha256)
}

func TestSkillSyncOldUnknownHashesAndStaleReceipts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createSkill(t, ctx, ti, "sync-stale", "First.")
	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: first.Skill.ID, Content: skillManifest("sync-stale", "Second.", "second"),
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	distributeSkill(t, ctx, ti, first.Skill.ID, nil, nil)

	result, err := ti.service.Sync(ctx, syncPayload("stale-host", []*gen.SyncSkillInstalled{{
		Name: "sync-stale", RawSha256: first.Version.RawSha256,
	}}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, second.Version.RawSha256, result.Updates[0].RawSha256)
	receipts := machineReceipts(t, ctx, ti, "stale-host")
	require.Equal(t, uuid.MustParse(first.Version.ID), receipts[0].SkillVersionID.UUID)
	require.True(t, receipts[0].SkillVersionID.Valid)

	unknownHash := strings.Repeat("f", 64)
	result, err = ti.service.Sync(ctx, syncPayload("stale-host", []*gen.SyncSkillInstalled{{
		Name: "sync-stale", RawSha256: unknownHash,
	}}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, second.Version.RawSha256, result.Updates[0].RawSha256)
	receipts = machineReceipts(t, ctx, ti, "stale-host")
	require.False(t, receipts[0].SkillVersionID.Valid)
	require.Equal(t, string(skills.SyncReceiptStatusApplied), receipts[0].Status)

	status, err := ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{
		ID: first.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Stale)
}

func TestSkillSyncExceptionsAndRollups(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "sync-exception", "Exception.")
	distributeSkill(t, ctx, ti, created.Skill.ID, nil, nil)

	result, err := ti.service.Sync(ctx, syncPayload("exception-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{{
		Name: "sync-exception", Status: "conflict_skipped",
	}}))
	require.NoError(t, err)
	require.Len(t, result.Updates, 1)
	receipts := machineReceipts(t, ctx, ti, "exception-host")
	require.Equal(t, "conflict_skipped", receipts[0].Status)
	require.False(t, receipts[0].SkillVersionID.Valid)

	status, err := ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{
		ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), status.Shadowed)

	_, err = ti.service.Sync(ctx, syncPayload("exception-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{{
		Name: "sync-exception", Status: "fs_readonly",
	}}))
	require.NoError(t, err)
	status, err = ti.service.GetDistributionStatus(ctx, &gen.GetDistributionStatusPayload{
		ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), status.Shadowed)
	require.Equal(t, int64(1), status.Degraded)
}

func TestSkillSyncAudienceRemovalAndReceiptPrune(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	projectWide := createSkill(t, ctx, ti, "sync-project-wide", "Everyone.")
	targeted := createSkill(t, ctx, ti, "sync-targeted", "Targeted.")
	hidden := createSkill(t, ctx, ti, "sync-hidden", "Hidden.")
	distributeSkill(t, ctx, ti, projectWide.Skill.ID, nil, nil)

	groupID := createDirectoryGroup(t, ctx, ti, ti.authContext.ActiveOrganizationID, "sync-group", "Sync Group")
	directoryUserID := createDirectoryUser(t, ctx, ti, ti.authContext.UserID, "sync-directory-user")
	createDirectoryUserGroupMembership(t, ctx, ti, directoryUserID, groupID, "sync-directory-user", "sync-group")
	distributeSkill(t, ctx, ti, targeted.Skill.ID, nil, []string{"sync-group"})

	installed := []*gen.SyncSkillInstalled{
		{Name: projectWide.Skill.Name, RawSha256: projectWide.Version.RawSha256},
		{Name: targeted.Skill.Name, RawSha256: targeted.Version.RawSha256},
		{Name: hidden.Skill.Name, RawSha256: hidden.Version.RawSha256},
	}
	result, err := ti.service.Sync(ctx, syncPayload("audience-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, []string{"sync-hidden"}, result.Removals)
	require.Len(t, machineReceipts(t, ctx, ti, "audience-host"), 2)

	_, err = workosrepo.New(ti.conn).CloseDirectoryUserGroupMembership(ctx, workosrepo.CloseDirectoryUserGroupMembershipParams{
		DirectoryUserID:  directoryUserID,
		DirectoryGroupID: groupID,
	})
	require.NoError(t, err)
	result, err = ti.service.Sync(ctx, syncPayload("audience-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, []string{"sync-hidden", "sync-targeted"}, result.Removals)
	receipts := machineReceipts(t, ctx, ti, "audience-host")
	require.Len(t, receipts, 1)
	require.Equal(t, uuid.MustParse(projectWide.Skill.ID), receipts[0].SkillID)
}

func TestSkillSyncManifestOmissionUndistributionAndArchive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	omitted := createSkill(t, ctx, ti, "sync-omitted", "Omitted.")
	removed := createSkill(t, ctx, ti, "sync-removed", "Removed.")
	archived := createSkill(t, ctx, ti, "sync-archived", "Archived.")
	distributeSkill(t, ctx, ti, omitted.Skill.ID, nil, nil)
	distributeSkill(t, ctx, ti, removed.Skill.ID, nil, nil)
	distributeSkill(t, ctx, ti, archived.Skill.ID, nil, nil)

	installed := []*gen.SyncSkillInstalled{
		{Name: omitted.Skill.Name, RawSha256: omitted.Version.RawSha256},
		{Name: removed.Skill.Name, RawSha256: removed.Version.RawSha256},
		{Name: archived.Skill.Name, RawSha256: archived.Version.RawSha256},
	}
	_, err := ti.service.Sync(ctx, syncPayload("lifecycle-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Len(t, machineReceipts(t, ctx, ti, "lifecycle-host"), 3)

	require.NoError(t, ti.service.Undistribute(ctx, &gen.UndistributePayload{
		ID: removed.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	}))
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{
		ID: archived.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	}))
	result, err := ti.service.Sync(ctx, syncPayload("lifecycle-host", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Equal(t, []string{"sync-archived", "sync-removed"}, result.Removals)
	require.Len(t, machineReceipts(t, ctx, ti, "lifecycle-host"), 1)

	result, err = ti.service.Sync(ctx, syncPayload("lifecycle-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Len(t, result.Updates, 1)
	require.Equal(t, omitted.Skill.Name, result.Updates[0].Name)
	require.Empty(t, machineReceipts(t, ctx, ti, "lifecycle-host"))
}

func TestSkillSyncUnresolvableVisibleDistributionIsProtected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	invalid := createSkill(t, ctx, ti, "Sync_Unresolvable", "Invalid.")
	require.False(t, invalid.Version.SpecValid)
	_, err := ti.repo.CreateSkillDistribution(ctx, repo.CreateSkillDistributionParams{
		PinnedVersionID: uuid.NullUUID{UUID: uuid.MustParse(invalid.Version.ID), Valid: true},
		Audience:        nil,
		CreatedByUserID: ti.authContext.UserID,
		ProjectID:       ti.projectID,
		SkillID:         uuid.MustParse(invalid.Skill.ID),
	})
	require.NoError(t, err)

	result, err := ti.service.Sync(ctx, syncPayload("unresolvable-host", []*gen.SyncSkillInstalled{{
		Name: invalid.Skill.Name, RawSha256: invalid.Version.RawSha256,
	}}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Empty(t, result.Updates)
	require.Empty(t, result.Removals)
	receipts := machineReceipts(t, ctx, ti, "unresolvable-host")
	require.Len(t, receipts, 1)
	require.False(t, receipts[0].SkillVersionID.Valid)
}

func TestSkillSyncRejectsDuplicateOverlapAndInvalidManifestEntries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	validHash := strings.Repeat("a", 64)
	payloads := []*gen.SyncPayload{
		syncPayload("validation-host", []*gen.SyncSkillInstalled{{Name: "duplicate", RawSha256: validHash}, {Name: "duplicate", RawSha256: validHash}}, []*gen.SyncSkillException{}),
		syncPayload("validation-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{{Name: "duplicate", Status: "fs_readonly"}, {Name: "duplicate", Status: "conflict_skipped"}}),
		syncPayload("validation-host", []*gen.SyncSkillInstalled{{Name: "overlap", RawSha256: validHash}}, []*gen.SyncSkillException{{Name: "overlap", Status: "fs_readonly"}}),
		syncPayload("validation-host", []*gen.SyncSkillInstalled{{Name: "Invalid_Name", RawSha256: validHash}}, []*gen.SyncSkillException{}),
		syncPayload("validation-host", []*gen.SyncSkillInstalled{{Name: "valid-name", RawSha256: "ABC"}}, []*gen.SyncSkillException{}),
		syncPayload("validation-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{{Name: "valid-name", Status: "unknown"}}),
	}
	for _, payload := range payloads {
		_, err := ti.service.Sync(ctx, payload)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	require.Empty(t, machineReceipts(t, ctx, ti, "validation-host"))
}

func TestSkillSyncAuthorizationIdentityAndFeature(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	payload := syncPayload("auth-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{})

	noGrantsCtx := authztest.WithExactGrants(t, ctx)
	result, err := ti.service.Sync(noGrantsCtx, payload)
	require.NoError(t, err)
	require.NotNil(t, result.Updates)
	require.NotNil(t, result.Removals)

	orgWide := *ti.authContext
	orgWide.OrgWidePluginHooksKey = true
	_, err = ti.service.Sync(contextvalues.SetAuthContext(ctx, &orgWide), payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	missingProject := *ti.authContext
	missingProject.ProjectID = nil
	_, err = ti.service.Sync(contextvalues.SetAuthContext(ctx, &missingProject), payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	missingUser := *ti.authContext
	missingUser.UserID = ""
	_, err = ti.service.Sync(contextvalues.SetAuthContext(ctx, &missingUser), payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	_, err = ti.service.Sync(t.Context(), payload)
	requireOopsCode(t, err, oops.CodeUnauthorized)

	err = orgrepo.New(ti.conn).DeleteOrganizationUserRelationship(ctx, orgrepo.DeleteOrganizationUserRelationshipParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		UserID:         conv.ToPGText(ti.authContext.UserID),
	})
	require.NoError(t, err)
	_, err = ti.service.Sync(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		UserID:         conv.ToPGText(ti.authContext.UserID),
	})
	require.NoError(t, err)
	_, err = ti.service.Sync(ctx, payload)
	require.NoError(t, err)

	softDeletedUserID := "soft-deleted-sync-user"
	softDeletedWorkosID := "workos-soft-deleted-sync-user"
	now := time.Now()
	_, err = usersrepo.New(ti.conn).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              softDeletedUserID,
		Email:           "soft-deleted-sync-user@example.com",
		DisplayName:     "Soft Deleted Sync User",
		PhotoUrl:        pgtype.Text{},
		WorkosID:        conv.ToPGText(softDeletedWorkosID),
		WorkosCreatedAt: conv.ToPGTimestamptz(now),
		WorkosUpdatedAt: conv.ToPGTimestamptz(now),
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		UserID:         conv.ToPGText(softDeletedUserID),
	})
	require.NoError(t, err)
	softDeletedAuth := *ti.authContext
	softDeletedAuth.UserID = softDeletedUserID
	softDeletedCtx := contextvalues.SetAuthContext(ctx, &softDeletedAuth)
	_, err = ti.service.Sync(softDeletedCtx, payload)
	require.NoError(t, err)
	err = usersrepo.New(ti.conn).DisableUser(ctx, usersrepo.DisableUserParams{
		WorkosUpdatedAt: conv.ToPGTimestamptz(now.Add(time.Second)),
		WorkosDeletedAt: conv.ToPGTimestamptz(now.Add(time.Second)),
		WorkosID:        conv.ToPGText(softDeletedWorkosID),
	})
	require.NoError(t, err)
	_, err = ti.service.Sync(softDeletedCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	disableSkills(t, ctx, ti)
	_, err = ti.service.Sync(ctx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSkillSyncMachineIdentityIsolation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "sync-machine", "Machine.")
	distributeSkill(t, ctx, ti, created.Skill.ID, nil, nil)
	installed := []*gen.SyncSkillInstalled{{Name: created.Skill.Name, RawSha256: created.Version.RawSha256}}

	_, err := ti.service.Sync(ctx, syncPayload("machine-a", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	_, err = ti.service.Sync(ctx, syncPayload("machine-b", installed, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Len(t, machineReceipts(t, ctx, ti, "machine-a"), 1)
	require.Len(t, machineReceipts(t, ctx, ti, "machine-b"), 1)

	_, err = ti.service.Sync(ctx, syncPayload("machine-a", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{}))
	require.NoError(t, err)
	require.Empty(t, machineReceipts(t, ctx, ti, "machine-a"))
	require.Len(t, machineReceipts(t, ctx, ti, "machine-b"), 1)
}

func TestSkillSyncIgnoresNonVisibleExceptions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "sync-not-visible", "Not visible.")
	result, err := ti.service.Sync(ctx, syncPayload("ignored-host", []*gen.SyncSkillInstalled{}, []*gen.SyncSkillException{{
		Name: created.Skill.Name, Status: "fs_readonly",
	}}))
	require.NoError(t, err)
	require.Empty(t, result.Updates)
	require.Empty(t, result.Removals)
	require.Empty(t, machineReceipts(t, ctx, ti, "ignored-host"))
}
