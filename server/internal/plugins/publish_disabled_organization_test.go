package plugins_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestListPluginPublishCandidates_ExcludesDisabledOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	queries := pluginsrepo.New(ti.conn)

	_, err := queries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	listCandidates := func() []pluginsrepo.ListPluginPublishCandidatesRow {
		candidates, err := queries.ListPluginPublishCandidates(ctx, pluginsrepo.ListPluginPublishCandidatesParams{
			AfterProjectID: uuid.Nil,
			ResultLimit:    100,
		})
		require.NoError(t, err)
		return candidates
	}

	require.Len(t, listCandidates(), 1)

	require.NoError(t, testrepo.New(ti.conn).SetOrganizationServingStateFixture(ctx, testrepo.SetOrganizationServingStateFixtureParams{
		ID:          authCtx.ActiveOrganizationID,
		Whitelisted: true,
		Disabled:    true,
	}))

	require.Empty(t, listCandidates(), "the sweep must not publish for a disabled organization")
}

func TestPublishProject_RejectsDisabledOrganization(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Disabled Org"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "disabled-org-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   conv.PtrEmpty(toolset.ID.String()),
		DisplayName: conv.PtrEmpty("Disabled Org Server"),
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	setDisabled := func(disabled bool) {
		require.NoError(t, testrepo.New(ti.conn).SetOrganizationServingStateFixture(ctx, testrepo.SetOrganizationServingStateFixtureParams{
			ID:          authCtx.ActiveOrganizationID,
			Whitelisted: true,
			Disabled:    disabled,
		}))
	}
	input := plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: false,
	}

	setDisabled(true)
	_, err = ti.service.PublishProject(ctx, input)
	require.ErrorIs(t, err, plugins.ErrOrganizationDisabled)
	require.False(t, mock.pushFilesCalled)

	keys, err := keysrepo.New(ti.conn).ListAPIKeysByOrganization(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	for _, key := range keys {
		require.False(t, strings.HasPrefix(key.Name, "plugins-"), "a disabled organization must not get new plugin API keys")
	}

	// Re-enabling the organization lets the same publish through.
	setDisabled(false)
	_, err = ti.service.PublishProject(ctx, input)
	require.NoError(t, err)
	require.True(t, mock.pushFilesCalled)
}

func TestPublishPlugins_DisabledOrganizationIsForbidden(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	require.NoError(t, testrepo.New(ti.conn).SetOrganizationServingStateFixture(ctx, testrepo.SetOrganizationServingStateFixtureParams{
		ID:          authCtx.ActiveOrganizationID,
		Whitelisted: true,
		Disabled:    true,
	}))

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{
		GithubUsernames:  nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
	require.ErrorIs(t, err, plugins.ErrOrganizationDisabled)
	require.False(t, mock.pushFilesCalled)
}
