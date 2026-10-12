package adminmcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/admin"
	adminrepo "github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

func newOrganizationWhitelistFixture(t *testing.T, name string) proposalFixture {
	t.Helper()
	f := newProposalFixture(t, name)
	for _, id := range []string{f.orgA, f.orgB} {
		require.NoError(t, adminrepo.New(f.db).AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: id, Whitelisted: conv.PtrToPGBool(new(false))}))
	}
	return f
}

func newOrganizationWhitelistWriter(t *testing.T, f proposalFixture) (*organizationWhitelistWriter, *writeTools) {
	t.Helper()
	runtime := NewRuntime(nil, "")
	oauth := &StaffOAuth{Approval: &StaffProposalApproval{store: f.store}}
	config := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationWhitelist: true}} //nolint:exhaustive // Only whitelisting is enabled.
	require.NoError(t, AttachWrites(runtime, oauth, &productfeatures.Client{}, config))
	writer, ok := runtime.writes.writers[OperationSetOrganizationWhitelist].(*organizationWhitelistWriter)
	require.True(t, ok)
	return writer, runtime.writes
}

func approveOrganizationWhitelist(t *testing.T, f proposalFixture, writer *organizationWhitelistWriter, proposalID string) {
	t.Helper()
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(proposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
}

func TestOrganizationWhitelistPrepareExecuteAndReplay(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_execute")
	writer, tools := newOrganizationWhitelistWriter(t, f)
	ctx := writeContext(t, f)
	before, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	other, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)
	input := PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: !before.Whitelisted, RetryKey: "whitelist-a"}
	prepared, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	var preview organizationWhitelistPreview
	require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
	require.Equal(t, f.orgA, preview.Target.OrganizationID)
	require.Equal(t, before.Whitelisted, preview.Target.Whitelisted)
	require.Equal(t, input.Whitelisted, preview.After)
	require.Contains(t, preview.SideEffects, "not credential revocation")
	view, err := writer.view(Proposal{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: f.orgA}, Arguments: json.RawMessage(`{"whitelisted":true}`), Preview: prepared.Preview})
	require.NoError(t, err)
	require.Len(t, view.Changes, 3)
	again, err := writer.prepare(ctx, input)
	require.NoError(t, err)
	require.True(t, again.Replay)
	require.Equal(t, prepared.ProposalID, again.ProposalID)
	input.OrganizationID = f.orgB
	_, err = writer.prepare(ctx, input)
	require.ErrorIs(t, err, ErrProposalConflict)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err, "browser approval is required")
	approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
	result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	var receipt organizationWhitelistReceipt
	require.NoError(t, json.Unmarshal(result.Result, &receipt))
	require.Equal(t, organizationWhitelistReceipt{OrganizationID: f.orgA, Whitelisted: !before.Whitelisted}, receipt)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgA)
	require.NoError(t, err)
	require.Equal(t, !before.Whitelisted, after.Whitelisted)
	after.Whitelisted, after.UpdatedAt = before.Whitelisted, before.UpdatedAt
	require.Equal(t, before, after, "only whitelisted and its update timestamp change")
	otherAfter, err := orgrepo.New(f.db).GetOrganizationMetadata(ctx, f.orgB)
	require.NoError(t, err)
	require.Equal(t, other, otherAfter)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionOrganizationWhitelistUpdated)
	require.NoError(t, err)
	require.Equal(t, f.orgA, entry.OrganizationID)
	require.Equal(t, "staff-subject", entry.ActorID)
	require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
	require.Equal(t, "test-client", *entry.ActingClientID)
	require.JSONEq(t, `{"whitelisted":false}`, string(entry.BeforeSnapshot))
	require.JSONEq(t, `{"whitelisted":true}`, string(entry.AfterSnapshot))
	result, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	require.True(t, result.Replay)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
}

func TestOrganizationWhitelistRemovalPreservesDisabledState(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_remove")
	q := adminrepo.New(f.db)
	require.NoError(t, q.AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: f.orgA, Whitelisted: conv.PtrToPGBool(new(true))}))
	_, err := q.AdminDisableOrganization(t.Context(), f.orgA)
	require.NoError(t, err)
	before, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	writer, tools := newOrganizationWhitelistWriter(t, f)
	prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: false, RetryKey: "remove-whitelist"})
	require.NoError(t, err)
	approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
	_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.NoError(t, err)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, after.Whitelisted)
	require.Equal(t, before.DisabledAt, after.DisabledAt)
	require.Equal(t, before.GramAccountType, after.GramAccountType)
}

func TestOrganizationWhitelistRejectsDriftAtApproval(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_stale_approval")
	writer, _ := newOrganizationWhitelistWriter(t, f)
	prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "stale"})
	require.NoError(t, err)
	_, err = orgrepo.New(f.db).UpdateOrganizationMetadataFromWorkOS(t.Context(), orgrepo.UpdateOrganizationMetadataFromWorkOSParams{ID: f.orgA, Name: "Renamed Synthetic A"})
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.ErrorIs(t, err, ErrStaleState)
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
}

func TestOrganizationWhitelistRejectsDriftAfterApproval(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_stale_execution")
	writer, tools := newOrganizationWhitelistWriter(t, f)
	prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "stale"})
	require.NoError(t, err)
	approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
	require.NoError(t, adminrepo.New(f.db).AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: f.orgA, AccountType: conv.ToPGText("pro")}))
	_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, after.Whitelisted)
	require.Equal(t, "pro", after.GramAccountType)
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
}

func TestOrganizationWhitelistAuditFailureRollsBack(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_rollback")
	writer, tools := newOrganizationWhitelistWriter(t, f)
	prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "rollback"})
	require.NoError(t, err)
	approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionOrganizationWhitelistUpdated))
	_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	after, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
	require.NoError(t, err)
	require.False(t, after.Whitelisted)
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, proposal.Status)
	require.Zero(t, countWriteEvents(t, f.db, proposal.ID, "executed"))
}

func TestOrganizationWhitelistConcurrentExecutionWritesOnce(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_concurrent")
	writer, tools := newOrganizationWhitelistWriter(t, f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "concurrent"})
	require.NoError(t, err)
	approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
	results := make([]ProposalOutput, 2)
	errors := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Go(func() { results[i], errors[i] = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID}) })
	}
	wg.Wait()
	for _, err := range errors {
		require.NoError(t, err)
	}
	require.NotEqual(t, results[0].Replay, results[1].Replay)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
	require.Equal(t, 1, countWriteEvents(t, f.db, uuid.MustParse(prepared.ProposalID), "executed"))
}

func TestOrganizationWhitelistPrepareRefusesInvalidTargetsAndNoop(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_invalid")
	writer, _ := newOrganizationWhitelistWriter(t, f)
	for _, input := range []PrepareOrganizationWhitelistInput{
		{OrganizationID: "synthetic-a", Whitelisted: true, RetryKey: "slug"},
		{OrganizationID: f.orgA, Whitelisted: true},
		{OrganizationID: f.orgA, Whitelisted: false, RetryKey: "noop"},
	} {
		_, err := writer.prepare(writeContext(t, f), input)
		require.Error(t, err)
	}
	require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
}

func TestOrganizationWhitelistViewRejectsWrongEnvelope(t *testing.T) {
	t.Parallel()
	writer := &organizationWhitelistWriter{}
	preview, err := json.Marshal(organizationWhitelistPreview{Target: admin.OrganizationWhitelistState{OrganizationID: "synthetic-a"}, After: true})
	require.NoError(t, err)
	for _, proposal := range []Proposal{
		{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, PlatformGlobal: true, Target: ProposalTarget{OrganizationID: "synthetic-a"}, Arguments: json.RawMessage(`{"whitelisted":true}`), Preview: preview},
		{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: "synthetic-b"}, Arguments: json.RawMessage(`{"whitelisted":true}`), Preview: preview},
		{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: "synthetic-a", ProjectID: uuid.NullUUID{UUID: uuid.New(), Valid: true}}, Arguments: json.RawMessage(`{"whitelisted":true}`), Preview: preview},
		{Operation: OperationSetOrganizationWhitelist, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: "synthetic-a"}, Arguments: json.RawMessage(`{"whitelisted":false}`), Preview: preview},
	} {
		_, err := writer.view(proposal)
		require.ErrorIs(t, err, ErrProposalInvalidated)
		if proposal.Target.ProjectID.Valid || proposal.PlatformGlobal {
			_, err = writer.expectedState(t.Context(), nil, proposal)
			require.ErrorIs(t, err, ErrProposalInvalidated)
		}
	}
}

func TestOrganizationWhitelistRefusesTrialTargets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		tier      string
		converted bool
		expired   bool
	}{
		{name: "running", tier: "enterprise"},
		{name: "expired", tier: "enterprise", expired: true},
		{name: "converted", tier: "enterprise", converted: true},
		{name: "non_enterprise", tier: "pro"},
		{name: "unknown_tier", tier: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_trial_"+tc.name)
			endsAt := time.Now().Add(time.Hour)
			if tc.expired {
				endsAt = time.Now().Add(-time.Hour)
			}
			q := trialsrepo.New(f.db)
			require.NoError(t, q.CreateTrial(t.Context(), trialsrepo.CreateTrialParams{OrganizationID: f.orgA, Tier: tc.tier, EndsAt: conv.ToPGTimestamptz(endsAt)}))
			if tc.converted {
				_, err := q.MarkTrialConverted(t.Context(), f.orgA)
				require.NoError(t, err)
			}
			trialBefore, err := q.GetTrial(t.Context(), f.orgA)
			require.NoError(t, err)
			writer, _ := newOrganizationWhitelistWriter(t, f)
			_, err = writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "trial-refused"})
			require.ErrorIs(t, err, errWhitelistTrial)
			trialAfter, err := trialsrepo.New(f.db).GetTrial(t.Context(), f.orgA)
			require.NoError(t, err)
			require.Equal(t, trialBefore, trialAfter)
			org, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
			require.NoError(t, err)
			require.False(t, org.Whitelisted)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
		})
	}
}

func TestOrganizationWhitelistRefusesNewTrialAtApproval(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"enterprise", "pro", "unknown"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_trial_approval_"+tier)
			writer, _ := newOrganizationWhitelistWriter(t, f)
			prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "new-trial"})
			require.NoError(t, err)
			require.NoError(t, trialsrepo.New(f.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{OrganizationID: f.orgA, Tier: tier, EndsAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
			proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
			require.NoError(t, err)
			_, err = f.store.Approve(t.Context(), proposal.ID, f.owner.SubjectURN, proposal.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
			require.ErrorIs(t, err, ErrStaleState)
			org, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
			require.NoError(t, err)
			require.False(t, org.Whitelisted)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
		})
	}
}

func TestOrganizationWhitelistRefusesNewTrialAtExecution(t *testing.T) {
	t.Parallel()
	for _, tier := range []string{"enterprise", "pro", "unknown"} {
		t.Run(tier, func(t *testing.T) {
			t.Parallel()
			f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_trial_execution_"+tier)
			writer, tools := newOrganizationWhitelistWriter(t, f)
			prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "new-trial"})
			require.NoError(t, err)
			approveOrganizationWhitelist(t, f, writer, prepared.ProposalID)
			require.NoError(t, trialsrepo.New(f.db).CreateTrial(t.Context(), trialsrepo.CreateTrialParams{OrganizationID: f.orgA, Tier: tier, EndsAt: conv.ToPGTimestamptz(time.Now().Add(time.Hour))}))
			_, err = tools.execute(writeContext(t, f), ProposalIDInput{ProposalID: prepared.ProposalID})
			require.ErrorIs(t, err, ErrStaleState)
			org, err := orgrepo.New(f.db).GetOrganizationMetadata(t.Context(), f.orgA)
			require.NoError(t, err)
			require.False(t, org.Whitelisted)
			require.Zero(t, auditCount(t, f, audit.ActionOrganizationWhitelistUpdated))
		})
	}
}

func TestOrganizationWhitelistPrepareRequiresAuthority(t *testing.T) {
	t.Parallel()
	writer := &organizationWhitelistWriter{}
	_, err := writer.prepare(t.Context(), PrepareOrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
	writer.writes = WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetOrganizationWhitelist: true}} //nolint:exhaustive // Only whitelisting is enabled.
	_, err = writer.prepare(t.Context(), PrepareOrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteIdentity)
	_, err = writer.prepare(writePrincipalContext(t, []string{ScopeRead}), PrepareOrganizationWhitelistInput{})
	require.ErrorIs(t, err, ErrWriteScope)
}

func TestOrganizationWhitelistExpectedStateRejectsVersionOnlyChange(t *testing.T) {
	t.Parallel()
	f := newOrganizationWhitelistFixture(t, "admin_mcp_whitelist_version")
	writer, _ := newOrganizationWhitelistWriter(t, f)
	prepared, err := writer.prepare(writeContext(t, f), PrepareOrganizationWhitelistInput{OrganizationID: f.orgA, Whitelisted: true, RetryKey: "version"})
	require.NoError(t, err)
	proposal, err := f.store.GetForOwner(t.Context(), uuid.MustParse(prepared.ProposalID), f.owner)
	require.NoError(t, err)
	// Same values, but an intervening account write must invalidate approval.
	require.NoError(t, adminrepo.New(f.db).AdminUpdateOrganization(t.Context(), adminrepo.AdminUpdateOrganizationParams{ID: f.orgA, Whitelisted: conv.PtrToPGBool(new(false))}))
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the writer's SQLc state check.
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(t.Context())) })
	_, err = writer.expectedState(t.Context(), tx, proposal)
	require.ErrorIs(t, err, ErrStaleState)
}
