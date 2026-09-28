package organizations_test

import (
	"testing"

	admingen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/stretchr/testify/require"
)

func TestSyncOnboardingStepsMirrorsTheCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	require.NoError(t, admin.SeedSupportMatrix(ctx, ti.conn))
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, ti.conn))
	// A second start finds everything in place and changes nothing.
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, ti.conn))

	steps, err := organizations.ListOnboardingSteps(ctx, ti.conn)
	require.NoError(t, err)
	require.Len(t, steps, 14, "twelve cards and two groups")
	position := make(map[string]int, len(steps))
	bySlug := make(map[string]*admingen.AdminOnboardingStep, len(steps))
	for index, step := range steps {
		position[step.Slug] = index
		bySlug[step.Slug] = step
	}

	group := bySlug["agent-observability"]
	require.Equal(t, "children", group.Completion)
	require.Nil(t, group.ParentSlug)
	require.False(t, group.HiddenByDefault, "visible because a card of its is")
	require.Empty(t, group.MethodSlugs)
	require.Less(t, position["agent-observability"], position["instrument-agents"], "a group precedes its cards")
	require.Less(t, position["mcp-distribution"], position["create-marketplace"])
	require.True(t, bySlug["mcp-distribution"].HiddenByDefault)

	card := bySlug["confirm-traffic"]
	require.Equal(t, "agent-observability", *card.ParentSlug)
	require.Equal(t, "manual", card.Completion)
	require.Equal(t, []string{"instrument-agents"}, card.Requires)

	identity := bySlug["identity-provider"]
	require.Nil(t, identity.ParentSlug, "one card covers domain, single sign-on and directory sync")
	require.Equal(t, "fact", identity.Completion)
	require.Empty(t, identity.Requires)

	require.Equal(t, []string{"inference"}, bySlug["anthropic-observability"].MethodSlugs)
	require.Equal(t, []string{"settings", "plugins"}, bySlug["anthropic-admin-controls"].MethodSlugs, "support matrix order")
	require.Equal(t, []string{"anthropic-api", "cursor-api", "openai-api", "conversations"}, bySlug["additional-agent-config"].MethodSlugs)
	require.Equal(t, "manual", bySlug["configure-policies"].Completion)
	require.Empty(t, bySlug["configure-policies"].MethodSlugs, "a step with no method applies to every stack")
}
