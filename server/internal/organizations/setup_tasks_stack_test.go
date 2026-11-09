package organizations_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// recordStack replaces the organization's recorded stack with these vendors,
// each on the given plan.
func recordStack(ctx context.Context, t *testing.T, ti *testInstance, organizationID string, vendorPlans ...[2]string) {
	t.Helper()
	vendors := make([]organizations.OnboardingStackVendorInput, 0, len(vendorPlans))
	for _, vendorPlan := range vendorPlans {
		vendors = append(vendors, organizations.OnboardingStackVendorInput{Vendor: vendorPlan[0], PlanSlug: conv.PtrEmpty(vendorPlan[1])})
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")
	_, err := organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), organizationID, organizations.OnboardingStackInput{Vendors: vendors, MdmVendor: "none", MdmVendorName: nil}, actor, nil)
	require.NoError(t, err)
}

func listedSetupTaskKeys(ctx context.Context, t *testing.T, ti *testInstance) []string {
	t.Helper()
	result, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	keys := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		keys = append(keys, task.Key)
	}
	return keys
}

func TestService_ListSetupTasksHidesCardsTheStackRulesOut(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	everyDefaultCard := []string{
		"identity-provider", "enable-logging", "anthropic-observability",
		"agent-observability", "instrument-agents", "confirm-traffic", "additional-agent-config",
		"mcp-distribution", "create-marketplace", "distribute-servers", "platform-mcp",
		"anthropic-admin-controls", "configure-policies",
	}

	// Without a recorded stack nothing is hidden: an unknown stack is not an
	// unsupported one.
	require.Equal(t, everyDefaultCard, listedSetupTaskKeys(ctx, t, ti))

	// Cursor and OpenAI lose the Anthropic cards and keep MCP distribution,
	// which the device agent delivers on any stack.
	recordStack(ctx, t, ti, ac.ActiveOrganizationID, [2]string{"Cursor", "cursor-teams"}, [2]string{"OpenAI", "openai-enterprise"})
	require.Equal(t, []string{
		"identity-provider", "enable-logging",
		"agent-observability", "instrument-agents", "confirm-traffic", "additional-agent-config",
		"mcp-distribution", "create-marketplace", "distribute-servers", "platform-mcp",
		"configure-policies",
	}, listedSetupTaskKeys(ctx, t, ti))

	// Copilot alone also loses the provider integrations: none of their APIs
	// belongs to GitHub.
	recordStack(ctx, t, ti, ac.ActiveOrganizationID, [2]string{"GitHub", "github-copilot-business"})
	require.Equal(t, []string{
		"identity-provider", "enable-logging",
		"agent-observability", "instrument-agents", "confirm-traffic",
		"mcp-distribution", "create-marketplace", "distribute-servers", "platform-mcp",
		"configure-policies",
	}, listedSetupTaskKeys(ctx, t, ti))

	// Anthropic alone supports every card shown by default.
	recordStack(ctx, t, ti, ac.ActiveOrganizationID, [2]string{"Anthropic", "anthropic-team"})
	require.Equal(t, everyDefaultCard, listedSetupTaskKeys(ctx, t, ti))
}

func TestService_ListSetupTasksShowsTheCursorCardOnlyToCursorStacks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	platformCtx := contextvalues.SetAuthContext(ctx, &platformAuth)

	// Staff turn the optional Cursor card on.
	visible := false
	_, err := ti.service.UpdateSetupTask(platformCtx, &gen.UpdateSetupTaskPayload{TaskKey: "cursor-admin-controls", Hidden: &visible})
	require.NoError(t, err)

	recordStack(ctx, t, ti, authCtx.ActiveOrganizationID, [2]string{"Anthropic", "anthropic-team"})
	require.NotContains(t, listedSetupTaskKeys(ctx, t, ti), "cursor-admin-controls", "the stack has no Cursor")
	includeHidden := true
	staffView, err := ti.service.ListSetupTasks(platformCtx, &gen.ListSetupTasksPayload{IncludeHidden: &includeHidden})
	require.NoError(t, err)
	cursorCard := setupTask(staffView.Tasks, "cursor-admin-controls")
	require.NotNil(t, cursorCard, "staff listing hidden cards still see it")
	require.True(t, cursorCard.Hidden)

	recordStack(ctx, t, ti, authCtx.ActiveOrganizationID, [2]string{"Anthropic", "anthropic-team"}, [2]string{"Cursor", "cursor-teams"})
	require.Contains(t, listedSetupTaskKeys(ctx, t, ti), "cursor-admin-controls")
}

func TestService_ListSetupTasksAppliesTheStackOverAnAssignedPlaybook(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")

	recordStack(ctx, t, ti, ac.ActiveOrganizationID, [2]string{"Anthropic", "anthropic-team"})
	playbook, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
		UseCaseID: nil, OrganizationID: &ac.ActiveOrganizationID, Name: "Ours", Description: "", IsDefault: false,
		StepSlugs: []string{"enable-logging", "anthropic-observability"},
	})
	require.NoError(t, err)
	playbookID := mustUUID(t, playbook.ID)
	_, err = organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, &playbookID, actor, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"enable-logging", "anthropic-observability"}, listedSetupTaskKeys(ctx, t, ti))

	// Staff change the stack after assigning the playbook. The wizard drops the
	// card the new stack rules out instead of walking it.
	recordStack(ctx, t, ti, ac.ActiveOrganizationID, [2]string{"Cursor", "cursor-teams"})
	require.Equal(t, []string{"enable-logging"}, listedSetupTaskKeys(ctx, t, ti))
}
