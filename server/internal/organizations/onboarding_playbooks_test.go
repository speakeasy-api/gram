package organizations_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func seedOnboardingCatalog(t *testing.T, ti *testInstance) {
	t.Helper()
	require.NoError(t, admin.SeedSupportMatrix(t.Context(), ti.conn))
	require.NoError(t, organizations.SyncOnboardingSteps(t.Context(), ti.conn))
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	require.NoError(t, err)
	return id
}

func TestOnboardingUseCasesAreCreatedByStaff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	empty, err := organizations.ListOnboardingUseCases(ctx, ti.conn)
	require.NoError(t, err)
	require.Empty(t, empty.UseCases, "nothing seeds use cases")

	created, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "security", " Security & Policies ", "Block what should not leave.")
	require.NoError(t, err)
	require.Equal(t, "Security & Policies", created.Name)
	require.Nil(t, created.DefaultPlaybookID)

	// Upper case, spaces, a trailing dash and nothing at all are not slugs.
	for _, slug := range []string{"Security", "spend controls", "spend-", ""} {
		_, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, slug, "Name", "")
		requireOopsCode(t, err, oops.CodeBadRequest)
	}
	_, err = organizations.CreateOnboardingUseCase(ctx, ti.conn, "security", "Again", "")
	requireOopsCode(t, err, oops.CodeConflict)
	_, err = organizations.CreateOnboardingUseCase(ctx, ti.conn, "blank", "   ", "")
	requireOopsCode(t, err, oops.CodeBadRequest)

	renamed, err := organizations.UpdateOnboardingUseCase(ctx, ti.conn, mustUUID(t, created.ID), "Security", "")
	require.NoError(t, err)
	require.Equal(t, "Security", renamed.Name)
	require.Equal(t, "security", renamed.Slug, "the slug never changes")
	_, err = organizations.UpdateOnboardingUseCase(ctx, ti.conn, uuid.New(), "Ghost", "")
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOnboardingPlaybookStepsAreValidated(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	useCase, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "distribution", "Distribution", "")
	require.NoError(t, err)
	id := mustUUID(t, useCase.ID)
	attempt := func(slugs ...string) error {
		_, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{UseCaseID: &id, OrganizationID: nil, Name: "Attempt", Description: "", IsDefault: false, StepSlugs: slugs})
		if err != nil {
			return fmt.Errorf("attempt %v: %w", slugs, err)
		}
		return nil
	}
	requireOopsCode(t, attempt(), oops.CodeBadRequest)
	requireOopsCode(t, attempt("nope"), oops.CodeBadRequest)
	requireOopsCode(t, attempt("instrument-agents"), oops.CodeBadRequest)
	requireOopsCode(t, attempt("platform-mcp", "platform-mcp"), oops.CodeBadRequest)
	require.NoError(t, attempt("identity-provider"))
	// A group brings its cards in catalog order, so a prerequisite inside
	// it is met by the group itself.
	require.NoError(t, attempt("agent-observability"))
	_, err = organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{UseCaseID: conv.PtrEmpty(uuid.New()), OrganizationID: nil, Name: "Orphan", Description: "", IsDefault: false, StepSlugs: []string{"platform-mcp"}})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestOnboardingDefaultPlaybookMovesBetweenPlaybooks(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	useCase, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "distribution", "Distribution", "")
	require.NoError(t, err)
	id := mustUUID(t, useCase.ID)
	first, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{UseCaseID: &id, OrganizationID: nil, Name: "First", Description: "", IsDefault: true, StepSlugs: []string{"platform-mcp"}})
	require.NoError(t, err)
	require.True(t, first.IsDefault)
	second, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{UseCaseID: &id, OrganizationID: nil, Name: "Second", Description: "", IsDefault: true, StepSlugs: []string{"mcp-distribution"}})
	require.NoError(t, err)
	require.True(t, second.IsDefault)

	list, err := organizations.ListOnboardingPlaybooks(ctx, ti.conn, nil)
	require.NoError(t, err)
	require.Len(t, list.Playbooks, 2)
	require.Equal(t, "Second", list.Playbooks[0].Name, "the default lists first")
	require.False(t, list.Playbooks[1].IsDefault)
	useCases, err := organizations.ListOnboardingUseCases(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, second.ID, *useCases.UseCases[0].DefaultPlaybookID)

	updated, err := organizations.UpdateOnboardingPlaybook(ctx, ti.conn, mustUUID(t, first.ID), "First again", "Two steps", true, []string{"platform-mcp", "configure-policies"})
	require.NoError(t, err)
	require.True(t, updated.IsDefault)
	require.Len(t, updated.Steps, 2)
	list, err = organizations.ListOnboardingPlaybooks(ctx, ti.conn, nil)
	require.NoError(t, err)
	require.Equal(t, "First again", list.Playbooks[0].Name)
	require.False(t, list.Playbooks[1].IsDefault)

	remaining, err := organizations.DeleteOnboardingPlaybook(ctx, ti.conn, mustUUID(t, second.ID))
	require.NoError(t, err)
	require.Len(t, remaining.Playbooks, 1)
	_, err = organizations.DeleteOnboardingPlaybook(ctx, ti.conn, mustUUID(t, second.ID))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOnboardingPlaybookAssignmentFollowsTheStack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	seedOnboardingCatalog(t, ti)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, "staff-test")
	useCase, err := organizations.CreateOnboardingUseCase(ctx, ti.conn, "observability", "Observability", "")
	require.NoError(t, err)
	playbook, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
		UseCaseID: conv.PtrEmpty(mustUUID(t, useCase.ID)), OrganizationID: nil, Name: "Anthropic first", Description: "", IsDefault: true,
		StepSlugs: []string{"anthropic-observability", "enable-logging", "agent-observability"},
	})
	require.NoError(t, err)
	playbookID := mustUUID(t, playbook.ID)

	// No stack recorded: the Anthropic step cannot be judged, so the
	// assignment is refused and names it.
	_, err = organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, &playbookID, actor, nil)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "Set up Anthropic observability")

	// A Cursor-only stack still lacks Anthropic.
	_, err = organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, organizations.OnboardingStackInput{Vendors: []organizations.OnboardingStackVendorInput{{Vendor: "Cursor", PlanSlug: conv.PtrEmpty("cursor-teams")}}, MdmVendor: "none", MdmVendorName: nil}, actor, nil)
	require.NoError(t, err)
	_, err = organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, &playbookID, actor, nil)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "needs Anthropic")
	// A card applies when any of its methods matches the stack: Configure
	// integrations lists Anthropic's API before Cursor's, and Cursor is enough.
	integrations, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
		UseCaseID: nil, OrganizationID: &ac.ActiveOrganizationID, Name: "Integrations", Description: "", IsDefault: false, StepSlugs: []string{"additional-agent-config"},
	})
	require.NoError(t, err)
	_, err = organizations.DeleteOnboardingPlaybook(ctx, ti.conn, mustUUID(t, integrations.ID))
	require.NoError(t, err)

	// With Anthropic in the stack the assignment lands, and the wizard walks
	// the playbook in its order with the group's cards.
	_, err = organizations.SaveOnboardingStack(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, organizations.OnboardingStackInput{Vendors: []organizations.OnboardingStackVendorInput{{Vendor: "Anthropic", PlanSlug: conv.PtrEmpty("anthropic-team")}}, MdmVendor: "none", MdmVendorName: nil}, actor, nil)
	require.NoError(t, err)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingPlaybookAssigned)
	require.NoError(t, err)
	assigned, err := organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, &playbookID, actor, nil)
	require.NoError(t, err)
	require.Equal(t, playbook.ID, assigned.Playbook.ID)
	for _, step := range assigned.Applicability {
		require.True(t, step.Applies, step.Slug)
	}
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingPlaybookAssigned)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingPlaybookAssigned)
	require.NoError(t, err)
	var snapshot audit.OrganizationOnboardingPlaybookSnapshot
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &snapshot))
	require.Equal(t, "observability", snapshot.UseCase)
	require.Equal(t, []string{"anthropic-observability", "enable-logging", "agent-observability"}, snapshot.StepSlugs)

	listed, err := ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	keys := make([]string, 0, len(listed.Tasks))
	for _, task := range listed.Tasks {
		keys = append(keys, task.Key)
	}
	require.Equal(t, []string{"anthropic-observability", "enable-logging", "agent-observability", "instrument-agents", "confirm-traffic"}, keys)
	require.Nil(t, setupTask(listed.Tasks, "identity-provider"), "cards outside the playbook are hidden")

	// A custom copy can drop a step and is checked against the stack too.
	custom, err := organizations.CloneOnboardingPlaybook(ctx, ti.conn, ac.ActiveOrganizationID, playbookID, nil)
	require.NoError(t, err)
	require.Equal(t, ac.ActiveOrganizationID, *custom.OrganizationID)
	require.Nil(t, custom.UseCaseID, "a copy belongs to the organization, not the use case")
	require.False(t, custom.IsDefault)
	// A playbook has a use case or an organization, never both or neither.
	for name, input := range map[string]organizations.OnboardingPlaybookInput{
		"both":    {UseCaseID: conv.PtrEmpty(mustUUID(t, useCase.ID)), OrganizationID: &ac.ActiveOrganizationID, Name: "Both", Description: "", IsDefault: false, StepSlugs: []string{"enable-logging"}},
		"neither": {UseCaseID: nil, OrganizationID: nil, Name: "Neither", Description: "", IsDefault: false, StepSlugs: []string{"enable-logging"}},
	} {
		_, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, input)
		requireOopsCode(t, err, oops.CodeBadRequest)
		require.ErrorContains(t, err, "not both", name)
	}
	_, err = organizations.UpdateOnboardingPlaybook(ctx, ti.conn, mustUUID(t, custom.ID), "Ours", "", false, []string{"enable-logging", "litellm"})
	require.NoError(t, err, "LiteLLM belongs to no vendor in particular")
	_, err = organizations.UpdateOnboardingPlaybook(ctx, ti.conn, mustUUID(t, custom.ID), "Ours", "", false, []string{"enable-logging", "mcp-distribution"})
	require.NoError(t, err, "plugins are an Anthropic method and Anthropic is in the stack")
	_, err = organizations.UpdateOnboardingPlaybook(ctx, ti.conn, mustUUID(t, custom.ID), "Ours", "", true, []string{"enable-logging"})
	requireOopsCode(t, err, oops.CodeBadRequest)
	mine, err := organizations.ListOnboardingPlaybooks(ctx, ti.conn, &ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, mine.Playbooks, 2, "the default and the custom copy")
	other, err := organizations.ListOnboardingPlaybooks(ctx, ti.conn, conv.PtrEmpty("org_someone_else"))
	require.NoError(t, err)
	require.Len(t, other.Playbooks, 1, "another organization never sees the copy")
	all, err := organizations.ListOnboardingPlaybooks(ctx, ti.conn, nil)
	require.NoError(t, err)
	require.Len(t, all.Playbooks, 2, "unscoped, every organization's copy lists with the shared ones")
	require.Nil(t, all.Playbooks[0].OrganizationName)
	require.NotNil(t, all.Playbooks[1].OrganizationName, "a copy names its organization")
	customID := mustUUID(t, custom.ID)
	_, err = organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), "org_someone_else", &customID, actor, nil)
	require.Error(t, err)
	// Nor can another organization's copy be copied again for this one.
	require.NoError(t, orgrepo.New(ti.conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{ID: "org_someone_else", Name: "Someone else", Slug: "someone-else"}))
	theirs, err := organizations.CreateOnboardingPlaybook(ctx, ti.conn, organizations.OnboardingPlaybookInput{
		UseCaseID: nil, OrganizationID: conv.PtrEmpty("org_someone_else"), Name: "Theirs", Description: "", IsDefault: false, StepSlugs: []string{"enable-logging"},
	})
	require.NoError(t, err)
	_, err = organizations.CloneOnboardingPlaybook(ctx, ti.conn, ac.ActiveOrganizationID, mustUUID(t, theirs.ID), nil)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.ErrorContains(t, err, "another organization")

	// Clearing the assignment returns the wizard to the saved selection, and
	// is audited as its own action.
	clearsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingPlaybookUnassigned)
	require.NoError(t, err)
	cleared, err := organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, nil, actor, nil)
	require.NoError(t, err)
	require.Nil(t, cleared.Playbook)
	clearsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationOnboardingPlaybookUnassigned)
	require.NoError(t, err)
	require.Equal(t, clearsBefore+1, clearsAfter)
	listed, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.NotNil(t, setupTask(listed.Tasks, "identity-provider"))

	// Retiring the use case retires its playbooks and the organization falls
	// back the same way.
	_, err = organizations.AssignOrganizationOnboardingPlaybook(ctx, ti.conn, audit.NewLogger(), ac.ActiveOrganizationID, &playbookID, actor, nil)
	require.NoError(t, err)
	useCases, err := organizations.DeleteOnboardingUseCase(ctx, ti.conn, mustUUID(t, useCase.ID))
	require.NoError(t, err)
	require.Empty(t, useCases.UseCases)
	fallen, err := organizations.LoadOrganizationOnboardingPlaybook(ctx, ti.conn, ac.ActiveOrganizationID)
	require.NoError(t, err)
	require.Nil(t, fallen.Playbook)
	listed, err = ti.service.ListSetupTasks(ctx, &gen.ListSetupTasksPayload{})
	require.NoError(t, err)
	require.NotNil(t, setupTask(listed.Tasks, "identity-provider"))
}
