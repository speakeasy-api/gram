package plugins_test

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/stretchr/testify/require"
	"testing"
)

//nolint:paralleltest,tparallel // Complete reads before the parent changes the shared plugin assignments.
func TestDistributionPluginProjection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	skill, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *ac.ProjectID, Name: "projection-skill", DisplayName: "Projection skill", Summary: pgtype.Text{}})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Distribution target", Description: new("Safe description")})
	require.NoError(t, err)
	for _, resource := range []string{ac.ProjectID.String(), skill.ID.String()} {
		t.Run(resource, func(t *testing.T) {
			readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, resource))
			result, err := ti.service.ListDistributionPlugins(readCtx, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
			require.NoError(t, err)
			require.NotEmpty(t, result.Plugins)
			detail, err := ti.service.GetDistributionPlugin(readCtx, &gen.GetDistributionPluginPayload{SkillID: skill.ID.String(), ID: plugin.ID})
			require.NoError(t, err)
			// This deliberately checks the field surface of the generated service model, not an HTTP body.
			data, err := json.Marshal(detail) //nolint:musttag // Goa service models do not carry transport JSON tags.
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(data, &fields))
			require.Len(t, fields, 4)
			require.Equal(t, plugin.ID, fields["ID"])
			require.Equal(t, "Distribution target", fields["Name"])
			require.Equal(t, "Safe description", fields["Description"])
			require.Equal(t, false, fields["IsDefault"])
			_, err = ti.service.ListPlugins(readCtx, &gen.ListPluginsPayload{})
			require.Error(t, err)
			_, err = ti.service.GetPlugin(readCtx, &gen.GetPluginPayload{ID: plugin.ID})
			require.Error(t, err)
			_, err = ti.service.PublishPlugins(readCtx, &gen.PublishPluginsPayload{})
			require.Error(t, err)
			_, err = ti.service.CreatePlugin(readCtx, &gen.CreatePluginPayload{Name: "Denied"})
			require.Error(t, err)
		})
	}
	denied := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, uuid.NewString()))
	_, err = ti.service.ListDistributionPlugins(denied, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.Error(t, err)
	_, err = ti.service.GetDistributionPlugin(denied, &gen.GetDistributionPluginPayload{SkillID: skill.ID.String(), ID: plugin.ID})
	require.Error(t, err)
}

func TestDistributionPluginProjectionIsolationAndLazyDefault(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	skill, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *ac.ProjectID, Name: "isolated-skill", DisplayName: "Isolated skill", Summary: pgtype.Text{}})
	require.NoError(t, err)
	readCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, skill.ID.String()))
	before, err := ti.service.ListDistributionPlugins(readCtx, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.NoError(t, err)
	require.Empty(t, before.Plugins, "a skill-only read must not provision configuration or default audience assignments")
	adminCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillRead, ac.ProjectID.String()), authz.NewGrant(authz.ScopeOrgAdmin, ac.ActiveOrganizationID))
	provisioned, err := ti.service.ListDistributionPlugins(adminCtx, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.NoError(t, err)
	require.Len(t, provisioned.Plugins, 1)
	require.True(t, provisioned.Plugins[0].IsDefault)
	repeated, err := ti.service.ListDistributionPlugins(readCtx, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.NoError(t, err)
	require.Len(t, repeated.Plugins, 1)
	require.Equal(t, provisioned.Plugins[0].ID, repeated.Plugins[0].ID)
	_, err = ti.service.GetDistributionPlugin(readCtx, &gen.GetDistributionPluginPayload{SkillID: uuid.NewString(), ID: provisioned.Plugins[0].ID})
	require.Error(t, err, "grant for one skill does not authorize another")
	otherOrg := *ac
	otherOrg.ActiveOrganizationID = "org_projection_other"
	mismatched := contextvalues.SetAuthContext(readCtx, &otherOrg)
	_, err = ti.service.ListDistributionPlugins(mismatched, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.Error(t, err)
	_, err = ti.service.GetDistributionPlugin(mismatched, &gen.GetDistributionPluginPayload{SkillID: skill.ID.String(), ID: provisioned.Plugins[0].ID})
	require.Error(t, err)
	foreignProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other distribution project", Slug: "other-distribution-project", OrganizationID: ac.ActiveOrganizationID})
	require.NoError(t, err)
	otherProject := *ac
	otherProject.ProjectID = &foreignProject.ID
	foreignPlugin, err := ti.service.CreatePlugin(contextvalues.SetAuthContext(ctx, &otherProject), &gen.CreatePluginPayload{Name: "Foreign target"})
	require.NoError(t, err)
	_, err = ti.service.GetDistributionPlugin(readCtx, &gen.GetDistributionPluginPayload{SkillID: skill.ID.String(), ID: foreignPlugin.ID})
	require.Error(t, err, "plugins from another project must not be disclosed")
	mismatched = contextvalues.SetAuthContext(readCtx, &otherProject)
	_, err = ti.service.ListDistributionPlugins(mismatched, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
	require.Error(t, err)
}

func TestDistributionPluginProjectionSkillExclusionsOverrideProjectGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	skill, err := skillsrepo.New(ti.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{ProjectID: *ac.ProjectID, Name: "excluded-skill", DisplayName: "Excluded skill", Summary: pgtype.Text{}})
	require.NoError(t, err)
	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Blocked target"})
	require.NoError(t, err)
	for _, blockedID := range []string{skill.ID.String(), ac.ProjectID.String()} {
		restricted := authztest.WithExactGrants(t, ctx,
			authz.NewGrant(authz.ScopeSkillRead, ac.ProjectID.String()),
			authz.NewGrant(authz.ScopeSkillRead, skill.ID.String()),
			authz.NewGrant(authz.ScopeSkillBlockedRead, blockedID),
		)
		_, err = ti.service.ListDistributionPlugins(restricted, &gen.ListDistributionPluginsPayload{SkillID: skill.ID.String()})
		require.Error(t, err, "neither project nor skill allow may bypass a matching exclusion")
		_, err = ti.service.GetDistributionPlugin(restricted, &gen.GetDistributionPluginPayload{SkillID: skill.ID.String(), ID: plugin.ID})
		require.Error(t, err, "neither project nor skill allow may bypass a matching exclusion")
	}
}
