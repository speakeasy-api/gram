package admin

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
)

func TestUpdateOnboardingPlaybookOrganizationOwnership(t *testing.T) {
	t.Parallel()
	ctx, svc, conn := newTestAdminService(t)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{OIDCSubject: "staff-test"})
	ownerID, otherID := "org_playbook_owner", "org_playbook_other"
	for _, id := range []string{ownerID, otherID} {
		seedOrg(t, ctx, conn, orgFixture{id: id, name: id, slug: id, accountType: "free"})
	}
	require.NoError(t, SeedSupportMatrix(ctx, conn))
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, conn))
	custom, err := organizations.CreateOnboardingPlaybook(ctx, conn, organizations.OnboardingPlaybookInput{
		OrganizationID: &ownerID, Name: "Original", Description: "Original description", StepSlugs: []string{"enable-logging"},
	})
	require.NoError(t, err)
	useCase, err := organizations.CreateOnboardingUseCase(ctx, conn, "distribution", "Distribution", "")
	require.NoError(t, err)
	useCaseID := uuid.MustParse(useCase.ID)
	global, err := organizations.CreateOnboardingPlaybook(ctx, conn, organizations.OnboardingPlaybookInput{
		UseCaseID: &useCaseID, Name: "Shared template", IsDefault: true, StepSlugs: []string{"enable-logging"},
	})
	require.NoError(t, err)
	before, err := organizations.ListOnboardingPlaybooks(ctx, conn, &ownerID)
	require.NoError(t, err)

	// Neither another organization's custom playbook nor a shared template may
	// be changed from an organization-scoped editor, including their steps.
	for _, playbook := range []*gen.AdminOnboardingPlaybook{custom, global} {
		result, err := svc.UpdateOnboardingPlaybook(ctx, &gen.UpdateOnboardingPlaybookPayload{
			PlaybookID: playbook.ID, OrganizationID: &otherID, Name: "Changed", Description: new("Changed description"), StepSlugs: []string{"platform-mcp"},
		})
		require.Nil(t, result)
		requireOopsCode(t, err, oops.CodeBadRequest)
		after, err := organizations.ListOnboardingPlaybooks(ctx, conn, &ownerID)
		require.NoError(t, err)
		require.Equal(t, before, after, "rejected update must not mutate metadata, defaults, or steps")
	}

	updated, err := svc.UpdateOnboardingPlaybook(ctx, &gen.UpdateOnboardingPlaybookPayload{
		PlaybookID: custom.ID, OrganizationID: &ownerID, Name: "Updated", Description: new("Updated description"), StepSlugs: []string{"platform-mcp"},
	})
	require.NoError(t, err)
	require.Equal(t, ownerID, *updated.OrganizationID)
	require.Equal(t, "Updated", updated.Name)
	require.Equal(t, "Updated description", updated.Description)
	require.Len(t, updated.Steps, 1)
	require.Equal(t, "platform-mcp", updated.Steps[0].Slug)
	after, err := organizations.ListOnboardingPlaybooks(ctx, conn, &ownerID)
	require.NoError(t, err)
	require.Contains(t, after.Playbooks, updated, "successful update must be persisted")

	// Staff editing the global library may still omit the organization scope.
	updated, err = svc.UpdateOnboardingPlaybook(ctx, &gen.UpdateOnboardingPlaybookPayload{
		PlaybookID: global.ID, Name: "Updated template", IsDefault: new(true), StepSlugs: []string{"platform-mcp"},
	})
	require.NoError(t, err)
	require.Nil(t, updated.OrganizationID)
	require.True(t, updated.IsDefault)
	require.Equal(t, "Updated template", updated.Name)
	require.Len(t, updated.Steps, 1)
	require.Equal(t, "platform-mcp", updated.Steps[0].Slug)
}
