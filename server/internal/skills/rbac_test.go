package skills_test

import (
	"testing"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSkillsRBACRejectsMissingAndOtherProjectGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	noGrants := authztest.WithExactGrants(t, ctx)

	_, err := ti.service.List(noGrants, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Create(noGrants, &gen.CreatePayload{Content: skillManifest("no-grant", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.FetchFromGitHub(noGrants, &gen.FetchFromGitHubPayload{RepoURL: "https://github.com/example/repository", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(noGrants, &gen.DistributePayload{ID: uuid.NewString(), PluginID: uuid.NewString(), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(noGrants, &gen.UndistributePayload{ID: uuid.NewString(), PluginID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListDistributions(noGrants, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	otherProjectGrant := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, uuid.NewString()))
	_, err = ti.service.List(otherProjectGrant, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Create(otherProjectGrant, &gen.CreatePayload{Content: skillManifest("other-project-grant", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.FetchFromGitHub(otherProjectGrant, &gen.FetchFromGitHubPayload{RepoURL: "https://github.com/example/repository", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(otherProjectGrant, &gen.DistributePayload{ID: uuid.NewString(), PluginID: uuid.NewString(), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(otherProjectGrant, &gen.UndistributePayload{ID: uuid.NewString(), PluginID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListDistributions(otherProjectGrant, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSkillsFeatureDisabledRejectsEveryEndpoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	created := createSkill(t, ctx, ti, "feature-gated", "Created while enabled.")
	disableSkills(t, ctx, ti)

	_, err := ti.service.Create(ctx, &gen.CreatePayload{Content: skillManifest("disabled-create", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.FetchFromGitHub(ctx, &gen.FetchFromGitHubPayload{RepoURL: "https://github.com/example/repository", SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.AddVersion(ctx, &gen.AddVersionPayload{ID: created.Skill.ID, Content: skillManifest("feature-gated", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.List(ctx, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Get(ctx, &gen.GetPayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListVersions(ctx, &gen.ListVersionsPayload{ID: created.Skill.ID, Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Archive(ctx, &gen.ArchivePayload{ID: created.Skill.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: uuid.NewString(), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(ctx, &gen.UndistributePayload{ID: created.Skill.ID, PluginID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListDistributions(ctx, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}
