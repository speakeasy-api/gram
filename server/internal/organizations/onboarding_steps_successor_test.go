package organizations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// retiredDistributionSteps are the steps an older build mirrored before the
// Create marketplace card folded into Distribute MCP servers.
var retiredDistributionSteps = []orgrepo.UpsertOnboardingStepParams{
	{Slug: "mcp-distribution", Title: "Distribute MCP servers", Description: "", Completion: "children", HiddenByDefault: false, SortOrder: 90},
	{Slug: "create-marketplace", Title: "Create marketplace", Description: "", Completion: "fact", HiddenByDefault: false, SortOrder: 91},
}

// reviveRetiredDistributionSteps makes the old steps live again, as they were
// before this build started. Upserting a retired slug clears its deleted_at.
func reviveRetiredDistributionSteps(ctx context.Context, t *testing.T, q *orgrepo.Queries) {
	t.Helper()
	for _, step := range retiredDistributionSteps {
		_, err := q.UpsertOnboardingStep(ctx, step)
		require.NoError(t, err)
	}
}

// playbookStepSlugs reads a playbook's live steps in walking order.
func playbookStepSlugs(ctx context.Context, t *testing.T, q *orgrepo.Queries, playbookID uuid.UUID) []string {
	t.Helper()
	rows, err := q.ListOnboardingPlaybookSteps(ctx, []uuid.UUID{playbookID})
	require.NoError(t, err)
	slugs := make([]string, 0, len(rows))
	for _, row := range rows {
		slugs = append(slugs, row.Slug)
	}
	return slugs
}

// setPlaybookStepSlugs overwrites a playbook's steps, the way an older build
// would have saved them.
func setPlaybookStepSlugs(ctx context.Context, t *testing.T, q *orgrepo.Queries, playbookID uuid.UUID, slugs []string) {
	t.Helper()
	require.NoError(t, q.DeleteOnboardingPlaybookSteps(ctx, playbookID))
	for position, slug := range slugs {
		inserted, err := q.InsertOnboardingPlaybookStep(ctx, orgrepo.InsertOnboardingPlaybookStepParams{PlaybookID: playbookID, Slug: slug, Position: int32(position)})
		require.NoError(t, err)
		require.Equal(t, int64(1), inserted, slug)
	}
	require.Equal(t, slugs, playbookStepSlugs(ctx, t, q, playbookID))
}

// Builds before the Create marketplace card folded into Distribute MCP
// servers saved playbooks that list the mcp-distribution group (and, in
// principle, the create-marketplace card). Start-up moves them onto
// distribute-servers before retiring the old steps, so no playbook silently
// loses MCP distribution.
func TestSyncOnboardingStepsMovesPlaybooksOffRetiredSteps(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	q := orgrepo.New(ti.conn)
	reviveRetiredDistributionSteps(ctx, t, q)

	cases := []struct {
		name   string
		before []string
		after  []string
	}{
		{"group only", []string{"enable-logging", "mcp-distribution", "configure-policies"}, []string{"enable-logging", "distribute-servers", "configure-policies"}},
		{"card only", []string{"create-marketplace", "enable-logging"}, []string{"distribute-servers", "enable-logging"}},
		{"group and card", []string{"mcp-distribution", "enable-logging", "create-marketplace"}, []string{"distribute-servers", "enable-logging"}},
		{"successor already listed", []string{"distribute-servers", "mcp-distribution"}, []string{"distribute-servers"}},
		{"neither", []string{"enable-logging", "configure-policies"}, []string{"enable-logging", "configure-policies"}},
	}
	playbookIDs := make([]uuid.UUID, len(cases))
	for index, tc := range cases {
		playbook, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
			UseCaseID: nil, OrganizationID: &ac.ActiveOrganizationID, Name: tc.name, Description: "", IsDefault: false,
			StepSlugs: []string{"enable-logging"},
		})
		require.NoError(t, err, tc.name)
		playbookIDs[index] = mustUUID(t, playbook.ID)
		setPlaybookStepSlugs(ctx, t, q, playbookIDs[index], tc.before)
	}

	require.NoError(t, organizations.SyncOnboardingSteps(ctx, ti.conn))
	for index, tc := range cases {
		require.Equal(t, tc.after, playbookStepSlugs(ctx, t, q, playbookIDs[index]), tc.name)
	}
	steps, err := organizations.ListOnboardingSteps(ctx, ti.conn)
	require.NoError(t, err)
	for _, step := range steps {
		require.NotContains(t, []string{"mcp-distribution", "create-marketplace"}, step.Slug, "the old steps are retired")
	}

	// Bring the old steps back to life: a playbook row still pointing at one
	// would show up again. None does, and the next start changes nothing.
	reviveRetiredDistributionSteps(ctx, t, q)
	for index, tc := range cases {
		require.Equal(t, tc.after, playbookStepSlugs(ctx, t, q, playbookIDs[index]), tc.name)
	}
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, ti.conn))
	for index, tc := range cases {
		require.Equal(t, tc.after, playbookStepSlugs(ctx, t, q, playbookIDs[index]), tc.name)
	}

	// A repointed playbook still validates, so staff can save it as it is.
	_, err = organizations.UpdateOnboardingPlaybook(ctx, ti.conn, playbookIDs[0], nil, cases[0].name, "", false, cases[0].after)
	require.NoError(t, err)
}
