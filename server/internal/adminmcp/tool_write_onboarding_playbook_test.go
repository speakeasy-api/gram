package adminmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// playbookSeeds are the playbooks a test can assign: the use case's default
// and a second shared one, which any stack supports; one that needs Anthropic
// in the stack; and a custom playbook that belongs to organization B.
type playbookSeeds struct {
	useCase   string
	defaultID string
	sharedID  string
	blockedID string
	customB   string
}

func seedPlaybooks(t *testing.T, f writeFixture) playbookSeeds {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, admin.SeedSupportMatrix(ctx, f.db))
	require.NoError(t, organizations.SyncOnboardingSteps(ctx, f.db))
	useCase, err := organizations.CreateOnboardingUseCase(ctx, f.db, "distribution", "Distribution", "Ship MCP servers to agents")
	require.NoError(t, err)
	useCaseID := uuid.MustParse(useCase.ID)
	create := func(input organizations.OnboardingPlaybookInput) string {
		t.Helper()
		playbook, err := organizations.CreateOnboardingPlaybook(ctx, f.db, input)
		require.NoError(t, err)
		return playbook.ID
	}
	orgB := f.orgB
	return playbookSeeds{
		useCase:   "distribution",
		defaultID: create(organizations.OnboardingPlaybookInput{UseCaseID: &useCaseID, OrganizationID: nil, Name: "staff-authored default name", Description: "private description", IsDefault: true, StepSlugs: []string{"identity-provider", "platform-mcp"}}),
		sharedID:  create(organizations.OnboardingPlaybookInput{UseCaseID: &useCaseID, OrganizationID: nil, Name: "Distribution lite", Description: "", IsDefault: false, StepSlugs: []string{"platform-mcp"}}),
		blockedID: create(organizations.OnboardingPlaybookInput{UseCaseID: &useCaseID, OrganizationID: nil, Name: "Needs Anthropic", Description: "", IsDefault: false, StepSlugs: []string{"anthropic-observability"}}),
		customB:   create(organizations.OnboardingPlaybookInput{UseCaseID: nil, OrganizationID: &orgB, Name: "B only", Description: "", IsDefault: false, StepSlugs: []string{"identity-provider"}}),
	}
}

func newOnboardingPlaybookWriter(f writeFixture) *onboardingPlaybookWriter {
	writes := WriteConfig{Enabled: true}
	return &onboardingPlaybookWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes}
}

func auditCount(t *testing.T, f writeFixture, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, action)
	require.NoError(t, err)
	return count
}

func assignedPlaybook(t *testing.T, f writeFixture, organizationID string) *gen.AdminOnboardingPlaybook {
	t.Helper()
	current, err := organizations.LoadOrganizationOnboardingPlaybook(t.Context(), f.db, organizationID)
	require.NoError(t, err)
	return current.Playbook
}

func TestOnboardingPlaybookWriteApplies(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_onboarding_playbook_write")
	seeds := seedPlaybooks(t, f)
	writer := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)

	for name, bad := range map[string]OnboardingPlaybookInput{
		"slug":             {OrganizationID: "synthetic-a", PlaybookID: seeds.defaultID},
		"neither":          {OrganizationID: f.orgA},
		"both":             {OrganizationID: f.orgA, PlaybookID: seeds.defaultID, UseCase: seeds.useCase},
		"malformed":        {OrganizationID: f.orgA, PlaybookID: "not-a-playbook-id"},
		"unknown":          {OrganizationID: f.orgA, PlaybookID: uuid.NewString()},
		"another tenant":   {OrganizationID: f.orgA, PlaybookID: seeds.customB},
		"unsupported":      {OrganizationID: f.orgA, PlaybookID: seeds.blockedID},
		"unknown use case": {OrganizationID: f.orgA, UseCase: "no-such-use-case"},
	} {
		_, err := writer.apply(ctx, bad)
		require.Error(t, err, name)
	}
	require.Nil(t, assignedPlaybook(t, f, f.orgA), "a refused call does not write")

	receipt, err := writer.apply(ctx, OnboardingPlaybookInput{OrganizationID: f.orgA, UseCase: seeds.useCase})
	require.NoError(t, err)
	assigned := assignedPlaybook(t, f, f.orgA)
	require.NotNil(t, assigned)
	require.NotEqual(t, seeds.defaultID, assigned.ID, "the organization walks its own copy of the template")
	require.Equal(t, f.orgA, *assigned.OrganizationID)
	require.Equal(t, "staff-authored default name", assigned.Name)
	require.Equal(t, assigned.ID, receipt.PlaybookID)
	require.Equal(t, seeds.defaultID, receipt.TemplateID)
	require.Equal(t, seeds.useCase, receipt.UseCase)
	require.False(t, receipt.Custom)
	require.Equal(t, []string{"identity-provider", "platform-mcp"}, receipt.Steps)
	require.Empty(t, receipt.Replaced)
	require.Equal(t, onboardingPlaybookSideEffects, receipt.SideEffects)
	encoded, err := json.Marshal(receipt)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "staff-authored", "playbook names stay out of tool output")
	require.NotContains(t, string(encoded), "private description")
	require.Nil(t, assignedPlaybook(t, f, f.orgB), "a write cannot affect a second tenant")
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationAssignOrganizationOnboardingPlaybook, f.orgA, "executed"))

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationOnboardingPlaybookAssigned)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface, "customer audit feeds mask admin_mcp actors")
	require.NotNil(t, entry.ActingClientID)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned))

	// Assigning the template again is a no-op, since the organization walks its copy.
	_, err = writer.apply(ctx, OnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID})
	require.ErrorContains(t, err, "already walks")
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned))

	// Another playbook replaces the copy and names what it replaced.
	next, err := writer.apply(ctx, OnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.sharedID})
	require.NoError(t, err)
	require.Equal(t, assigned.ID, next.Replaced)
	require.Equal(t, []string{"identity-provider", "platform-mcp"}, next.ReplacedSteps)
}

func TestOnboardingPlaybookWriteReadsThePlaybookAsItIsNow(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_onboarding_playbook_live")
	seeds := seedPlaybooks(t, f)
	writer := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)

	_, err := organizations.UpdateOnboardingPlaybook(t.Context(), f.db, uuid.MustParse(seeds.defaultID), nil, "staff-authored default name", "private description", true, []string{"platform-mcp", "identity-provider"})
	require.NoError(t, err)
	receipt, err := writer.apply(ctx, OnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID})
	require.NoError(t, err)
	require.Equal(t, []string{"platform-mcp", "identity-provider"}, receipt.Steps)

	// A dashboard assignment in the meantime makes the same call a no-op.
	sharedID := uuid.MustParse(seeds.sharedID)
	_, err = organizations.AssignOrganizationOnboardingPlaybook(t.Context(), f.db, audit.NewLogger(), f.orgB, &sharedID, urn.NewPrincipal(urn.PrincipalTypeUser, "dashboard-staff"), nil)
	require.NoError(t, err)
	_, err = writer.apply(ctx, OnboardingPlaybookInput{OrganizationID: f.orgB, PlaybookID: seeds.sharedID})
	require.ErrorContains(t, err, "already walks")
	require.Equal(t, int64(2), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned), "the refused call writes no audit")
}

func TestOnboardingPlaybookWriteAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_onboarding_playbook_rollback")
	seeds := seedPlaybooks(t, f)
	writer := newOnboardingPlaybookWriter(f)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationOnboardingPlaybookAssigned))
	_, err := writer.apply(writeContext(t, f), OnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID})
	require.Error(t, err)
	require.Nil(t, assignedPlaybook(t, f, f.orgA), "the assignment rolls back with the failed audit")
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationAssignOrganizationOnboardingPlaybook, f.orgA, "failed"))
	require.Zero(t, countWriteEvents(t, f.db, OperationAssignOrganizationOnboardingPlaybook, f.orgA, "executed"))
}
