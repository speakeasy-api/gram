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
	_, err = ti.service.ListUnknownActivations(noGrants, &gen.ListUnknownActivationsPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(noGrants, &gen.DistributePayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	pluginID := uuid.NewString()
	assistantID := uuid.NewString()
	_, err = ti.service.Distribute(noGrants, &gen.DistributePayload{ID: uuid.NewString(), PluginID: &pluginID, AssistantID: &assistantID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Create(noGrants, &gen.CreatePayload{Content: skillManifest("no-grant", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(noGrants, &gen.DistributePayload{ID: uuid.NewString(), PluginID: new(uuid.NewString()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(noGrants, &gen.UndistributePayload{ID: uuid.NewString(), PluginID: new(uuid.NewString()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListDistributions(noGrants, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	otherProjectGrant := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, uuid.NewString()))
	_, err = ti.service.List(otherProjectGrant, &gen.ListPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListUnknownActivations(otherProjectGrant, &gen.ListUnknownActivationsPayload{Cursor: nil, Limit: 10, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Create(otherProjectGrant, &gen.CreatePayload{Content: skillManifest("other-project-grant", "Denied.", "body"), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Distribute(otherProjectGrant, &gen.DistributePayload{ID: uuid.NewString(), PluginID: new(uuid.NewString()), PinnedVersionID: nil, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	err = ti.service.Undistribute(otherProjectGrant, &gen.UndistributePayload{ID: uuid.NewString(), PluginID: new(uuid.NewString()), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.ListDistributions(otherProjectGrant, &gen.ListDistributionsPayload{Cursor: nil, Limit: 50, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}
