package skills_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSkillShareCreatesToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "shared-skill", "A shareable skill.")

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkCreate)
	require.NoError(t, err)

	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, link.Token, 43)
	require.NotEmpty(t, link.CreatedAt)

	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkCreate)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillShareLinkCreate)
	require.NoError(t, err)
	require.Equal(t, "skill", record.SubjectType)
	require.Equal(t, "shared-skill", record.SubjectSlug)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.NotEmpty(t, metadata["share_link_id"])
	require.NotContains(t, string(record.Metadata), link.Token)

	listed, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Skills, 1)
	require.NotNil(t, listed.Skills[0].ShareToken)
	require.Equal(t, link.Token, *listed.Skills[0].ShareToken)

	fetched, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotNil(t, fetched.Skill.ShareToken)
	require.Equal(t, link.Token, *fetched.Skill.ShareToken)
}

func TestSkillShareIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "idempotent-share", "Shared twice.")

	createBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkCreate)
	require.NoError(t, err)

	first, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	second, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, first.Token, second.Token)
	require.Equal(t, first.CreatedAt, second.CreatedAt)

	createAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkCreate)
	require.NoError(t, err)
	require.Equal(t, createBefore+1, createAfter)
}

func TestSkillUnshareRevokesLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "unshared-skill", "Shared then revoked.")

	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	_, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	require.NoError(t, err)

	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)

	require.NoError(t, ti.service.Unshare(ctx, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	_, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	requireOopsCode(t, err, oops.CodeNotFound)

	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeBefore+1, revokeAfter)

	// Repeated unshare is a no-op and records no extra revoke event.
	require.NoError(t, ti.service.Unshare(ctx, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	revokeNoop, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeAfter, revokeNoop)

	listed, err := ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Len(t, listed.Skills, 1)
	require.Nil(t, listed.Skills[0].ShareToken)

	fetched, err := ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Nil(t, fetched.Skill.ShareToken)
}

func TestSkillReshareMintsDifferentToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "reshared-skill", "Shared, revoked, shared again.")

	first, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NoError(t, ti.service.Unshare(ctx, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	second, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.NotEqual(t, first.Token, second.Token)
	require.Len(t, second.Token, 43)

	// The old token stays dead after a re-share.
	_, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: first.Token})
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: second.Token})
	require.NoError(t, err)
}

func TestSkillGetSharedReturnsLatestVersion(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "public-skill", "A public summary.")

	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	shared, err := ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	require.NoError(t, err)
	require.Equal(t, "public-skill", shared.Name)
	require.Equal(t, created.Skill.DisplayName, shared.DisplayName)
	require.NotNil(t, shared.Summary)
	require.Equal(t, "A public summary.", *shared.Summary)
	require.Equal(t, created.Version.Content, shared.Content)
	require.NotEmpty(t, shared.UpdatedAt)
	require.NotNil(t, shared.CacheControl)
	require.Equal(t, "private, max-age=300", *shared.CacheControl)
	require.NotNil(t, shared.XRobotsTag)
	require.Equal(t, "noindex, nofollow", *shared.XRobotsTag)
	require.NotNil(t, shared.ReferrerPolicy)
	require.Equal(t, "no-referrer", *shared.ReferrerPolicy)

	second, err := ti.service.AddVersion(ctx, &gen.AddVersionPayload{
		ID: created.Skill.ID, Content: skillManifest("public-skill", "A public summary.", "updated body"),
		DerivedFromVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	shared, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	require.NoError(t, err)
	require.Equal(t, second.Version.Content, shared.Content)
	require.Equal(t, second.Version.CreatedAt, shared.UpdatedAt)
}

func TestSkillGetSharedUnknownToken(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createSkill(t, ctx, ti, "unshared-only", "Never shared.")

	_, err := ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestSkillArchiveRevokesShareLink(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "archived-shared-skill", "Shared then archived.")

	link, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)

	revokeBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)

	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))

	_, err = ti.service.GetShared(ctx, &gen.GetSharedPayload{Token: link.Token})
	requireOopsCode(t, err, oops.CodeNotFound)

	revokeAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeBefore+1, revokeAfter)

	// Archiving an already-unshared skill records no extra revoke event.
	other := createSkill(t, ctx, ti, "archived-unshared-skill", "Never shared, archived.")
	require.NoError(t, ti.service.Archive(ctx, &gen.ArchivePayload{ID: other.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	revokeFinal, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillShareLinkRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeAfter, revokeFinal)
}

func TestSkillShareRequiresUserIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "keyed-share-skill", "Shared by an API key.")

	// Simulate a key-based caller: full grants but no user identity.
	keyAuth := *ti.authContext
	keyAuth.UserID = ""
	keyCtx := contextvalues.SetAuthContext(ctx, &keyAuth)

	_, err := ti.service.Share(keyCtx, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	err = ti.service.Unshare(keyCtx, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestSkillShareRBACDenied(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "rbac-share-skill", "Grant checks.")

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.Share(noGrants, &gen.SharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Unshare(noGrants, &gen.UnsharePayload{SkillID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSkillShareUnknownSkill(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Share(ctx, &gen.SharePayload{SkillID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Unsharing a skill that does not exist is an idempotent no-op.
	require.NoError(t, ti.service.Unshare(ctx, &gen.UnsharePayload{SkillID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
}
