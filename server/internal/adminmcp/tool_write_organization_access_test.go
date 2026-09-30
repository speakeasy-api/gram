package adminmcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

func newOrganizationAccessWriter(f proposalFixture, operation WriteOperation) (*organizationAccessWriter, *writeTools) {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{operation: true}} //nolint:exhaustive // Only the tested access operation is enabled.
	writer := &organizationAccessWriter{store: f.store, audit: audit.NewLogger(), writes: writes, baseURL: "https://staff.example.test" + Path, operation: operation}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{operation: writer}) //nolint:exhaustive // Only the tested access operation is dispatched.
	return writer, tools
}

func approveOrganizationAccess(t *testing.T, f proposalFixture, writer *organizationAccessWriter, proposalID string) {
	t.Helper()
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(proposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
}

func organizationAccessState(t *testing.T, f proposalFixture, organizationID string) admin.OrganizationAccessState {
	t.Helper()
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the shared access mutator's SQLc queries
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	state, err := admin.LockOrganizationAccessTx(t.Context(), tx, organizationID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return state
}

func TestOrganizationAccessPrepareExecuteAndReplay(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_organization_access")
	writer, tools := newOrganizationAccessWriter(f, OperationDisableOrganization)
	ctx := writeContext(t, f)
	beforeA := organizationAccessState(t, f, f.orgA)
	beforeB := organizationAccessState(t, f, f.orgB)

	for _, input := range []PrepareOrganizationAccessInput{
		{OrganizationID: "synthetic-a", RetryKey: "slug"},
		{OrganizationID: f.orgA},
	} {
		_, err := writer.prepare(ctx, input)
		require.Error(t, err)
	}

	prepared, err := writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgA, RetryKey: "disable-a"})
	require.NoError(t, err)
	var preview organizationAccessPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, f.orgA, preview.OrganizationID)
	require.Equal(t, "Synthetic A", preview.Name)
	require.True(t, preview.EnabledBefore)
	require.False(t, preview.EnabledAfter)
	replayPrepare, err := writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgA, RetryKey: "disable-a"})
	require.NoError(t, err)
	require.True(t, replayPrepare.Replay)
	require.Equal(t, prepared.ProposalID, replayPrepare.ProposalID)

	// A disabled state change after preparation invalidates both approval and execution.
	stale, err := writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgB, RetryKey: "stale-access"})
	require.NoError(t, err)
	_, err = repo.New(f.db).AdminDisableOrganization(t.Context(), f.orgB)
	require.NoError(t, err)
	beforeExecutionB := organizationAccessState(t, f, f.orgB)
	staleProposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(stale.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), staleProposal.ID, f.owner.SubjectURN, staleProposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)

	approveOrganizationAccess(t, f, writer, prepared.ProposalID)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	var receipt organizationAccessReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, organizationAccessReceipt{OrganizationID: f.orgA, Enabled: false}, receipt)
	stateA := organizationAccessState(t, f, f.orgA)
	stateB := organizationAccessState(t, f, f.orgB)
	require.NotNil(t, stateA.DisabledAt)
	require.Nil(t, beforeA.DisabledAt)
	require.Equal(t, beforeExecutionB.DisabledAt, stateB.DisabledAt, "execution cannot change the organisation outside its exact proposal target")
	require.Nil(t, beforeB.DisabledAt)

	entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionOrganizationDisabled)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationDisabled))
	_, err = writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgA, RetryKey: "disable-again"})
	require.Error(t, err, "a no-op prepare is refused")

	replay, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, replay.Replay)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationDisabled))
	require.Equal(t, 1, countWriteEvents(t, f.db, uuid.MustParse(prepared.ProposalID), "executed"))
}

func TestOrganizationEnableProposalClearsDisabledAt(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_organization_enable")
	_, err := repo.New(f.db).AdminDisableOrganization(t.Context(), f.orgA)
	require.NoError(t, err)
	writer, tools := newOrganizationAccessWriter(f, OperationEnableOrganization)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgA, RetryKey: "enable-a"})
	require.NoError(t, err)
	var preview organizationAccessPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.False(t, preview.EnabledBefore)
	require.True(t, preview.EnabledAfter)
	approveOrganizationAccess(t, f, writer, prepared.ProposalID)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.Equal(t, string(ProposalSucceeded), result.Status)
	require.Nil(t, organizationAccessState(t, f, f.orgA).DisabledAt)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationEnabled))
}

func TestOrganizationAccessAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_organization_access_rollback")
	writer, tools := newOrganizationAccessWriter(f, OperationDisableOrganization)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOrganizationAccessInput{OrganizationID: f.orgA, RetryKey: "rollback"})
	require.NoError(t, err)
	approveOrganizationAccess(t, f, writer, prepared.ProposalID)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationDisabled))
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	require.Nil(t, organizationAccessState(t, f, f.orgA).DisabledAt, "audit failure must roll back access state")
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, proposal.Status, "a failed audit does not consume approval")
	require.Equal(t, 1, countWriteEvents(t, f.db, proposal.ID, "execute_refused"))
}

func TestOrganizationAccessViewRejectsGlobalAndWrongTargets(t *testing.T) {
	t.Parallel()
	writer := &organizationAccessWriter{operation: OperationDisableOrganization}
	preview, err := json.Marshal(organizationAccessPreview{OrganizationID: "synthetic-a", Name: "Synthetic A", Slug: "synthetic-a", EnabledBefore: true, EnabledAfter: false, SideEffects: organizationAccessSideEffects})
	require.NoError(t, err)
	proposal := Proposal{Operation: OperationDisableOrganization, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: "synthetic-a"}, Preview: preview}
	view, err := writer.view(proposal)
	require.NoError(t, err)
	require.False(t, view.PlatformGlobal)
	proposal.PlatformGlobal = true
	_, err = writer.view(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
	proposal.PlatformGlobal = false
	proposal.Target.OrganizationID = "synthetic-b"
	_, err = writer.view(proposal)
	require.ErrorIs(t, err, ErrProposalInvalidated)
}

func TestOrganizationAccessWriterRejectsChangedIdentity(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_organization_access_identity")
	writer, _ := newOrganizationAccessWriter(f, OperationDisableOrganization)
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the shared access mutator's SQLc queries
	require.NoError(t, err)
	state, err := writer.readState(t.Context(), tx, f.orgA)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	expected, err := json.Marshal(state)
	require.NoError(t, err)
	digest, err := stateDigest(expected)
	require.NoError(t, err)
	proposal := Proposal{Operation: OperationDisableOrganization, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: f.orgA}, ExpectedStateDigest: digest}

	_, err = orgrepo.New(f.db).UpdateOrganizationMetadataFromWorkOS(t.Context(), orgrepo.UpdateOrganizationMetadataFromWorkOSParams{Name: "Renamed Synthetic A", ID: f.orgA})
	require.NoError(t, err)
	tx, err = f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the writer's locked SQLc state check
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	err = writer.expectedState(t.Context(), tx, proposal)
	require.ErrorIs(t, err, ErrStaleState, "identity and access state are both included in the expected-state digest")
}
