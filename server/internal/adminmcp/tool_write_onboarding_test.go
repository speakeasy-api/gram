package adminmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/organizations"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// The catalogue's default board for an organisation with no saved selection:
// every task except LiteLLM.
var defaultVisibleTasks = []string{"identity-provider", "enable-logging", "anthropic-observability", "instrument-agents", "additional-agent-config", "confirm-traffic", "create-marketplace", "distribute-servers", "platform-mcp", "anthropic-admin-controls", "configure-policies"}

// Every default task hidden by a {identity-provider, litellm} selection, in
// catalogue order.
var hiddenByLiteLLMSelection = []string{"enable-logging", "anthropic-observability", "instrument-agents", "additional-agent-config", "confirm-traffic", "create-marketplace", "distribute-servers", "platform-mcp", "anthropic-admin-controls", "configure-policies"}

func newOnboardingWriter(f proposalFixture) (*onboardingWriter, *writeTools) {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationOnboarding: true}} //nolint:exhaustive // Only selected write operations are enabled by this test.
	writer := &onboardingWriter{store: f.store, audit: audit.NewLogger(), writes: writes, baseURL: "https://staff.example.test" + Path}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationSetOrganizationOnboarding: writer}) //nolint:exhaustive // Only the implemented operation is dispatched.
	return writer, tools
}

func visibleTasks(t *testing.T, f proposalFixture, organizationID string) []string {
	t.Helper()
	config, err := organizations.LoadOnboardingConfiguration(t.Context(), f.db, organizationID)
	require.NoError(t, err)
	visible := []string{}
	for _, task := range config.Tasks {
		if !task.Hidden {
			visible = append(visible, task.Key)
		}
	}
	return visible
}

func setupTaskRows(t *testing.T, f proposalFixture, organizationID string) int {
	t.Helper()
	rows, err := orgrepo.New(f.db).ListOrganizationSetupTasks(t.Context(), organizationID)
	require.NoError(t, err)
	return len(rows)
}

func auditCount(t *testing.T, f proposalFixture, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(t.Context(), f.db, action)
	require.NoError(t, err)
	return count
}

func TestOnboardingWriteApprovalAndExecution(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_write")
	writer, tools := newOnboardingWriter(f)
	ctx := writeContext(t, f)
	require.NoError(t, orgrepo.New(f.db).SetOrganizationOnboardingPreset(t.Context(), orgrepo.SetOrganizationOnboardingPresetParams{OrganizationID: f.orgA, Preset: conv.ToPGText("gateway")}))
	require.Equal(t, defaultVisibleTasks, visibleTasks(t, f, f.orgA))

	for _, bad := range []PrepareOnboardingInput{
		{OrganizationID: f.orgA, VisibleTaskKeys: []string{"enable-logging"}},
		{OrganizationID: "synthetic-a", VisibleTaskKeys: []string{"enable-logging"}, RetryKey: "slug"},
		{OrganizationID: f.orgA, VisibleTaskKeys: nil, RetryKey: "omitted"},
		{OrganizationID: f.orgA, VisibleTaskKeys: []string{"not-a-task"}, RetryKey: "unknown"},
		{OrganizationID: f.orgA, VisibleTaskKeys: []string{"enable-logging", "enable-logging"}, RetryKey: "duplicate"},
		{OrganizationID: f.orgA, VisibleTaskKeys: slices.Repeat([]string{"enable-logging"}, 13), RetryKey: "oversized"},
		{OrganizationID: f.orgA, VisibleTaskKeys: defaultVisibleTasks, RetryKey: "no-op"},
	} {
		_, err := writer.prepare(ctx, bad)
		require.Error(t, err, "prepare must refuse input %q", bad.RetryKey)
	}

	input := PrepareOnboardingInput{OrganizationID: f.orgA, VisibleTaskKeys: []string{"identity-provider", "litellm"}, RetryKey: "onboarding-1"}
	prepared, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "https://staff.example.test"+Path+"/proposals/"+prepared.ProposalID, prepared.ApprovalURL)
	var preview onboardingPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, []string{"litellm"}, preview.Show)
	require.Equal(t, hiddenByLiteLLMSelection, preview.Hide)
	require.Equal(t, 1, preview.Unchanged)
	require.Equal(t, onboardingSideEffects, preview.SideEffects)
	require.Zero(t, setupTaskRows(t, f, f.orgA), "prepare does not write")

	input.VisibleTaskKeys = []string{"litellm", "identity-provider"}
	replay, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.True(t, replay.Replay, "the stored selection is sorted, so key order does not matter")
	require.Equal(t, prepared.ProposalID, replay.ProposalID)
	id := uuid.MustParse(prepared.ProposalID)
	stored, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.JSONEq(t, `{"visible_task_keys":["identity-provider","litellm"]}`, string(stored.Arguments))

	// Two more proposals prepared from the same starting state. One is
	// approved now; both must go stale once the first proposal executes.
	staleApproval, err := writer.prepare(ctx, PrepareOnboardingInput{OrganizationID: f.orgA, VisibleTaskKeys: []string{}, RetryKey: "stale-approval"})
	require.NoError(t, err)
	staleExecution, err := writer.prepare(ctx, PrepareOnboardingInput{OrganizationID: f.orgA, VisibleTaskKeys: []string{"platform-mcp"}, RetryKey: "stale-execution"})
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
	approval.operations = map[WriteOperation]approvableOperation{OperationSetOrganizationOnboarding: writer} //nolint:exhaustive // Only selected write operations are enabled by this test.
	handler := middleware.AdminOriginCheck(nil)(approval.Handler())
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, approvalRequest(http.MethodGet, id, nil, "browser-session"))
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "Synthetic A")
	require.Contains(t, page.Body.String(), "Show 1 and hide 10 onboarding tasks; 1 unchanged")
	require.Contains(t, page.Body.String(), "<td>litellm task</td><td>Hidden</td><td>Shown</td>")
	require.Contains(t, page.Body.String(), "<td>instrument-agents task</td><td>Shown</td><td>Hidden</td>")
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, approvalRequest(http.MethodPost, id, approvalForm(t, page.Body.String()), "browser-session"))
	require.Equal(t, http.StatusOK, accepted.Code)

	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	wantResult, err := json.Marshal(map[string][]string{"shown": {"litellm"}, "hidden": hiddenByLiteLLMSelection})
	require.NoError(t, err)
	require.JSONEq(t, string(wantResult), string(result.Result))
	require.Equal(t, []string{"identity-provider", "litellm"}, visibleTasks(t, f, f.orgA))
	config, err := organizations.LoadOnboardingConfiguration(t.Context(), f.db, f.orgA)
	require.NoError(t, err)
	require.Equal(t, new("gateway"), config.Preset, "the preset is never changed")
	require.Zero(t, setupTaskRows(t, f, f.orgB), "execution cannot affect a second tenant")
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationOnboardingUpdated)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.NotNil(t, entry.ActingSurface)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface, "customer audit feeds mask admin_mcp actors")
	require.NotNil(t, entry.ActingClientID)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingUpdated))
	require.Equal(t, int64(11), auditCount(t, f, audit.ActionOrganizationSetupTaskUpdated), "one event per changed task")

	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationOnboardingUpdated), "a replay writes no audit")

	pending, err := f.store.GetForOwner(t.Context(), uuid.MustParse(staleApproval.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), pending.ID, f.owner.SubjectURN, pending.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: staleExecution.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, countWriteEvents(t, f.db, approvedEarly.ID, "executed"))
	require.Equal(t, []string{"identity-provider", "litellm"}, visibleTasks(t, f, f.orgA))

	otherPrincipal, ok := ctx.Value(principalKey{}).(Principal)
	require.True(t, ok)
	otherPrincipal.Subject = "user:other-staff"
	_, err = tools.execute(context.WithValue(ctx, principalKey{}, otherPrincipal), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrProposalNotFound)
}

func TestOnboardingWriteAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_rollback")
	writer, tools := newOnboardingWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOnboardingInput{OrganizationID: f.orgA, VisibleTaskKeys: []string{"enable-logging"}, RetryKey: "rollback"})
	require.NoError(t, err)
	p, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)

	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationOnboardingUpdated))
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	require.Zero(t, setupTaskRows(t, f, f.orgA), "task visibility rolls back with the failed audit")
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationSetupTaskUpdated))
	still, err := f.store.GetForOwner(t.Context(), p.ID, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, still.Status, "a failed attempt does not consume the approval")
	require.Equal(t, 1, countWriteEvents(t, f.db, p.ID, "execute_refused"))
	require.Zero(t, countWriteEvents(t, f.db, p.ID, "executed"))
}

// A stored selection that prepare would refuse can only come from direct
// database access. Execution refuses it even with the expected state intact
// and approval bypassed.
func TestOnboardingWriteRejectsInvalidStoredSelection(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_onboarding_invalid")
	writer, tools := newOnboardingWriter(f)
	ctx := writeContext(t, f)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: Exercise the transaction-bound state reader with a synthetic database.
	require.NoError(t, err)
	state, err := writer.readState(t.Context(), tx, f.orgA)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	expected, err := json.Marshal(state)
	require.NoError(t, err)
	noOp, err := json.Marshal(map[string][]string{"visible_task_keys": slices.Sorted(slices.Values(defaultVisibleTasks))})
	require.NoError(t, err)

	for key, arguments := range map[string]string{
		"unknown": `{"visible_task_keys":["not-a-task"]}`,
		"no-op":   string(noOp),
		"missing": `{}`,
	} {
		input := featureProposal(f.orgA, "invalid-"+key, true)
		input.Operation = OperationSetOrganizationOnboarding
		input.Arguments = json.RawMessage(arguments)
		input.ExpectedState = expected
		p, _, err := f.store.Create(t.Context(), f.owner, input, time.Now())
		require.NoError(t, err)
		_, err = f.store.Approve(t.Context(), p.ID, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, allowProposalBrowser)
		require.NoError(t, err)
		_, err = tools.execute(ctx, ProposalIDInput{ProposalID: p.ID.String()})
		require.ErrorIs(t, err, ErrProposalInvalidated, key)
	}
	require.Zero(t, setupTaskRows(t, f, f.orgA))
}

func TestOnboardingViewRequiresMatchingTarget(t *testing.T) {
	t.Parallel()
	writer := &onboardingWriter{}
	preview, err := json.Marshal(onboardingPreview{OrganizationID: "org_b", Name: "B", Slug: "b", Show: []string{"enable-logging"}, Hide: []string{}, Unchanged: 11})
	require.NoError(t, err)

	_, err = writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_a"}, Preview: preview})
	require.ErrorIs(t, err, ErrProposalInvalidated, "a preview naming another organisation is never shown")

	view, err := writer.view(Proposal{Target: ProposalTarget{OrganizationID: "org_b"}, Preview: preview})
	require.NoError(t, err)
	require.Equal(t, "Show 1 and hide 0 onboarding tasks; 11 unchanged", view.Summary)
	require.Equal(t, []proposalViewChange{{Setting: "enable-logging task", Before: "Hidden", After: "Shown"}}, view.Changes)
}
