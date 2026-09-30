package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// playbookSeeds are the playbooks a test can propose: the use case's default
// and a second shared one, which any stack supports; one that needs Anthropic
// in the stack; and a custom playbook that belongs to organization B.
type playbookSeeds struct {
	useCase   string
	defaultID string
	sharedID  string
	blockedID string
	customB   string
}

func seedPlaybooks(t *testing.T, f proposalFixture) playbookSeeds {
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

func newOnboardingPlaybookWriter(f proposalFixture) (*onboardingPlaybookWriter, *writeTools) {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationAssignOrganizationOnboardingPlaybook: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	writer := &onboardingPlaybookWriter{store: f.store, audit: audit.NewLogger(), writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationAssignOrganizationOnboardingPlaybook: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	return writer, tools
}

func auditCount(t *testing.T, f proposalFixture, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, action)
	require.NoError(t, err)
	return count
}

func assignedPlaybook(t *testing.T, f proposalFixture, organizationID string) *gen.AdminOnboardingPlaybook {
	t.Helper()
	current, err := organizations.LoadOrganizationOnboardingPlaybook(t.Context(), f.db, organizationID)
	require.NoError(t, err)
	return current.Playbook
}

func TestOnboardingPlaybookWriteApprovalAndExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_playbook_write")
	seeds := seedPlaybooks(t, f)
	writer, tools := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)

	for _, bad := range []PrepareOnboardingPlaybookInput{
		{OrganizationID: f.orgA, PlaybookID: seeds.defaultID},
		{OrganizationID: "synthetic-a", PlaybookID: seeds.defaultID, RetryKey: "slug"},
		{OrganizationID: f.orgA, RetryKey: "neither"},
		{OrganizationID: f.orgA, PlaybookID: seeds.defaultID, UseCase: seeds.useCase, RetryKey: "both"},
		{OrganizationID: f.orgA, PlaybookID: "not-a-playbook-id", RetryKey: "malformed"},
		{OrganizationID: f.orgA, PlaybookID: uuid.NewString(), RetryKey: "unknown"},
		{OrganizationID: f.orgA, PlaybookID: seeds.customB, RetryKey: "another-tenant"},
		{OrganizationID: f.orgA, PlaybookID: seeds.blockedID, RetryKey: "unsupported"},
		{OrganizationID: f.orgA, UseCase: "no-such-use-case", RetryKey: "unknown-use-case"},
	} {
		_, err := writer.prepare(ctx, bad)
		require.Error(t, err, "prepare must refuse input %q", bad.RetryKey)
	}
	require.Nil(t, assignedPlaybook(t, f, f.orgA), "prepare does not write")

	input := PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID, RetryKey: "playbook-1"}
	prepared, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "https://staff.example.test"+Path+"/proposals/"+prepared.ProposalID, prepared.ApprovalURL)
	var preview onboardingPlaybookPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, seeds.defaultID, preview.PlaybookID)
	require.Equal(t, seeds.useCase, preview.UseCase)
	require.False(t, preview.Custom)
	require.Equal(t, []string{"identity-provider", "platform-mcp"}, preview.Steps)
	require.Empty(t, preview.Replaces)
	require.Equal(t, onboardingPlaybookSideEffects, preview.SideEffects)
	require.NotContains(t, string(prepared.Preview), "staff-authored", "playbook names stay out of tool output")
	require.NotContains(t, string(prepared.Preview), "private description")

	replay, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, UseCase: seeds.useCase, RetryKey: "playbook-1"})
	require.NoError(t, err)
	require.True(t, replay.Replay, "the use case resolves to the same playbook, so the same retry key replays")
	require.Equal(t, prepared.ProposalID, replay.ProposalID)
	id := uuid.MustParse(prepared.ProposalID)
	stored, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.JSONEq(t, `{"playbook_id":"`+seeds.defaultID+`"}`, string(stored.Arguments))

	// Two more proposals prepared from the same starting state. One is
	// approved now; both must go stale once the first proposal executes.
	staleApproval, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.sharedID, RetryKey: "stale-approval"})
	require.NoError(t, err)
	staleExecution, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.sharedID, RetryKey: "stale-execution"})
	require.NoError(t, err)
	approvedEarly, err := f.store.GetForOwner(t.Context(), uuid.MustParse(staleExecution.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), approvedEarly.ID, f.owner.SubjectURN, approvedEarly.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)

	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotApproved)

	staff := &contextvalues.AdminAuthContext{SessionID: "browser-session", OIDCSubject: "staff-subject", Email: "staff@example.test"}
	memory := testenv.NewMemoryCache()
	approval := newStaffProposalApproval(f.store, NewStaffOAuthAuthorization(nil, nil, memory, &fakeAdminVerifier{result: staff}, f.cipher, staffAudience), memory, writer.writes)
	approval.operations = map[WriteOperation]approvableOperation{OperationAssignOrganizationOnboardingPlaybook: writer} //nolint:exhaustive // Only selected write operations are enabled by this test.
	handler := middleware.AdminOriginCheck(nil)(approval.Handler())
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, approvalRequest(http.MethodGet, id, nil, "browser-session"))
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "Synthetic A")
	require.Contains(t, page.Body.String(), "Assign a copy of the shared playbook of use case distribution "+seeds.defaultID+" with 2 steps")
	require.Contains(t, page.Body.String(), "<td>Playbook</td><td>None</td><td>a copy of "+seeds.defaultID+"</td>")
	require.Contains(t, page.Body.String(), "<td>Steps</td><td>None</td><td>identity-provider, platform-mcp</td>")
	require.NotContains(t, page.Body.String(), "staff-authored")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, approvalRequest(http.MethodPost, id, approvalForm(t, page.Body.String()), "browser-session"))
	require.Equal(t, http.StatusOK, accepted.Code)

	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	assigned := assignedPlaybook(t, f, f.orgA)
	require.NotNil(t, assigned)
	require.NotEqual(t, seeds.defaultID, assigned.ID, "the organization walks its own copy of the template")
	require.Equal(t, f.orgA, *assigned.OrganizationID)
	require.Equal(t, "staff-authored default name", assigned.Name)
	require.JSONEq(t, `{"playbook_id":"`+assigned.ID+`","template_id":"`+seeds.defaultID+`","replaced":"","steps":["identity-provider","platform-mcp"]}`, string(result.Result))
	require.Nil(t, assignedPlaybook(t, f, f.orgB), "execution cannot affect a second tenant")
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationOnboardingPlaybookAssigned)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface, "customer audit feeds mask admin_mcp actors")
	require.NotNil(t, entry.ActingClientID)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned))

	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned), "a replay writes no audit")

	pending, err := f.store.GetForOwner(t.Context(), uuid.MustParse(staleApproval.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), pending.ID, f.owner.SubjectURN, pending.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: staleExecution.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, countWriteEvents(t, f.db, approvedEarly.ID, "executed"))
	require.Equal(t, assigned.ID, assignedPlaybook(t, f, f.orgA).ID)

	// A fresh proposal replaces the copy and names what it replaces. Proposing
	// the template again is a no-op, since the organization walks its copy.
	next, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.sharedID, RetryKey: "playbook-2"})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(next.Preview, &preview))
	require.Equal(t, assigned.ID, preview.Replaces)
	require.Equal(t, []string{"identity-provider", "platform-mcp"}, preview.ReplacesSteps)
	_, err = writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID, RetryKey: "no-op"})
	require.ErrorContains(t, err, "already walks")

	otherPrincipal, ok := ctx.Value(principalKey{}).(Principal)
	require.True(t, ok)
	otherPrincipal.Subject = "user:other-staff"
	_, err = tools.execute(context.WithValue(ctx, principalKey{}, otherPrincipal), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotFound)
}

func TestOnboardingPlaybookWriteGoesStaleWhenThePlaybookChanges(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_playbook_stale")
	seeds := seedPlaybooks(t, f)
	writer, tools := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID, RetryKey: "edited"})
	require.NoError(t, err)
	p, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)

	// Staff reorder the playbook's steps between approval and execution.
	_, err = organizations.UpdateOnboardingPlaybook(t.Context(), f.db, uuid.MustParse(seeds.defaultID), "staff-authored default name", "private description", true, []string{"platform-mcp", "identity-provider"})
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Nil(t, assignedPlaybook(t, f, f.orgA))
	require.Zero(t, countWriteEvents(t, f.db, p.ID, "executed"))

	// A dashboard assignment in the meantime makes it stale too.
	retired, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.sharedID, RetryKey: "raced"})
	require.NoError(t, err)
	r, err := f.store.GetForOwner(t.Context(), uuid.MustParse(retired.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), r.ID, f.owner.SubjectURN, r.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	defaultID := uuid.MustParse(seeds.defaultID)
	_, err = organizations.AssignOrganizationOnboardingPlaybook(t.Context(), f.db, audit.NewLogger(), f.orgA, &defaultID, urn.NewPrincipal(urn.PrincipalTypeUser, "dashboard-staff"), nil)
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: retired.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Equal(t, "staff-authored default name", assignedPlaybook(t, f, f.orgA).Name)
}

func TestOnboardingPlaybookWriteAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_playbook_rollback")
	seeds := seedPlaybooks(t, f)
	writer, tools := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOnboardingPlaybookInput{OrganizationID: f.orgA, PlaybookID: seeds.defaultID, RetryKey: "rollback"})
	require.NoError(t, err)
	p, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)

	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationOnboardingPlaybookAssigned))
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	require.Nil(t, assignedPlaybook(t, f, f.orgA), "the assignment rolls back with the failed audit")
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned))
	still, err := f.store.GetForOwner(t.Context(), p.ID, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, still.Status, "a failed attempt does not consume the approval")
	require.Equal(t, 1, countWriteEvents(t, f.db, p.ID, "execute_refused"))
	require.Zero(t, countWriteEvents(t, f.db, p.ID, "executed"))
}

// Stored arguments that prepare would refuse can only come from direct
// database access. Execution refuses them even with the expected state intact
// and approval bypassed.
func TestOnboardingPlaybookWriteRejectsInvalidStoredArguments(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_playbook_invalid")
	seeds := seedPlaybooks(t, f)
	writer, tools := newOnboardingPlaybookWriter(f)
	ctx := writeContext(t, f)
	defaultID := uuid.MustParse(seeds.defaultID)
	_, err := organizations.AssignOrganizationOnboardingPlaybook(t.Context(), f.db, audit.NewLogger(), f.orgA, &defaultID, urn.NewPrincipal(urn.PrincipalTypeUser, "dashboard-staff"), nil)
	require.NoError(t, err)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: Exercise the transaction-bound state reader with a synthetic database.
	require.NoError(t, err)
	state, err := writer.readState(t.Context(), tx, f.orgA, defaultID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	expected, err := json.Marshal(state)
	require.NoError(t, err)

	for key, tc := range map[string]struct {
		arguments string
		want      error
	}{
		"malformed": {arguments: `{"playbook_id":"not-a-playbook-id"}`, want: ErrProposalInvalidated},
		"missing":   {arguments: `{}`, want: ErrProposalInvalidated},
		"no-op":     {arguments: `{"playbook_id":"` + seeds.defaultID + `"}`, want: ErrProposalInvalidated},
		"retired":   {arguments: `{"playbook_id":"` + uuid.NewString() + `"}`, want: ErrStaleState},
	} {
		input := featureProposal(f.orgA, "invalid-"+key, true)
		input.Operation = OperationAssignOrganizationOnboardingPlaybook
		input.Arguments = json.RawMessage(tc.arguments)
		input.ExpectedState = expected
		p, _, err := f.store.Create(t.Context(), f.owner, input, time.Now())
		require.NoError(t, err)
		_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, allowProposalBrowser)
		require.NoError(t, err)
		_, err = tools.execute(ctx, ProposalIDInput{ProposalID: p.ID.String()})
		require.ErrorIs(t, err, tc.want, key)
	}
	require.Equal(t, "staff-authored default name", assignedPlaybook(t, f, f.orgA).Name)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingPlaybookAssigned), "only the dashboard assignment is audited")
}

func TestOnboardingPlaybookViewRequiresMatchingTarget(t *testing.T) {
	t.Parallel()
	writer := &onboardingPlaybookWriter{}
	preview, err := json.Marshal(onboardingPlaybookPreview{OrganizationID: "org_b", Name: "B", Slug: "b", PlaybookID: uuid.NewString(), UseCase: "distribution", Custom: false, Steps: []string{"platform-mcp"}, Replaces: "", ReplacesSteps: []string{}, SideEffects: onboardingPlaybookSideEffects})
	require.NoError(t, err)
	_, err = writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_a"}, Preview: preview})
	require.ErrorIs(t, err, ErrProposalInvalidated)
	_, err = writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_b"}, Preview: json.RawMessage(`{"organization_id":"org_b","playbook_id":"","steps":[]}`)})
	require.ErrorIs(t, err, ErrProposalInvalidated)
	view, err := writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_b"}, Preview: preview})
	require.NoError(t, err)
	require.Equal(t, "org_b", view.OrganizationID)
	require.Equal(t, []proposalViewChange{{Setting: "Playbook", Before: "None", After: view.Changes[0].After}, {Setting: "Steps", Before: "None", After: "platform-mcp"}}, view.Changes)
}
