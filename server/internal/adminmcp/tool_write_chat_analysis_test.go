package adminmcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func newChatAnalysisWriter(f proposalFixture) (*chatAnalysisWriter, *writeTools) {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetChatAnalysisSettings: true}} //nolint:exhaustive // Only the tested operation is enabled.
	writer := &chatAnalysisWriter{store: f.store, audit: audit.NewLogger(), writes: writes, baseURL: "https://staff.example.test" + Path, now: time.Now}
	tools := newWriteTools(f.store, writes, writer.baseURL, map[WriteOperation]operationWriter{OperationSetChatAnalysisSettings: writer}) //nolint:exhaustive // Only the tested writer is dispatched.
	return writer, tools
}

func TestChatAnalysisWriteApprovesExecutesAndReplays(t *testing.T) {
	t.Parallel()
	for _, judge := range []string{analysis.WorkUnitsJudgeName, businessmemory.JudgeName} {
		t.Run(judge, func(t *testing.T) {
			t.Parallel()
			f := newProposalFixture(t, "admin_mcp_analysis_success")
			writer, tools := newChatAnalysisWriter(f)
			ctx := writeContext(t, f)
			input := PrepareChatAnalysisInput{OrganizationID: f.orgA, Judge: judge, Enabled: true, DailyCap: 25, RetryKey: "analysis-enable"}
			prepared, err := writer.prepare(ctx, input)
			require.NoError(t, err)
			var preview chatAnalysisPreview
			require.NoError(t, json.Unmarshal(prepared.Preview, &preview))
			require.True(t, preview.Before.IsDefault)
			require.True(t, preview.After.Enabled)
			require.Contains(t, preview.SideEffects, "not currency")
			before, err := chatanalysis.LoadSettings(t.Context(), f.db, f.orgA)
			require.NoError(t, err)
			require.True(t, before.IsDefault, "prepare must not create a setting")
			retry, err := writer.prepare(ctx, input)
			require.NoError(t, err)
			require.True(t, retry.Replay)
			require.Equal(t, prepared.ProposalID, retry.ProposalID)
			input.DailyCap++
			_, err = writer.prepare(ctx, input)
			require.ErrorIs(t, err, ErrProposalConflict)
			_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			require.ErrorIs(t, err, ErrProposalNotApproved)
			id := uuid.MustParse(prepared.ProposalID)
			p, err := f.store.GetForOwner(t.Context(), id, f.owner)
			require.NoError(t, err)
			view, err := writer.view(p)
			require.NoError(t, err)
			require.Equal(t, f.orgA, view.OrganizationID)
			require.False(t, view.PlatformGlobal)
			_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
			require.NoError(t, err)
			result, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			require.NoError(t, err)
			require.Equal(t, string(ProposalSucceeded), result.Status)
			row, err := analysisrepo.New(f.db).GetChatAnalysisSettingForOrganizationJudge(t.Context(), analysisrepo.GetChatAnalysisSettingForOrganizationJudgeParams{OrganizationID: f.orgA, Judge: judge})
			require.NoError(t, err)
			require.True(t, row.Enabled)
			require.EqualValues(t, 25, row.DailyCap)
			neighbour, err := chatanalysis.LoadSettings(t.Context(), f.db, f.orgB)
			require.NoError(t, err)
			require.True(t, neighbour.IsDefault)
			entry, err := audittest.LatestAuditLogByAction(t.Context(), f.db, audit.ActionChatAnalysisSettingsUpsert)
			require.NoError(t, err)
			require.Equal(t, f.orgA, entry.OrganizationID)
			require.Equal(t, "staff-subject", entry.ActorID)
			require.Equal(t, string(audit.SurfaceAdminMCP), *entry.ActingSurface)
			require.Equal(t, "test-client", *entry.ActingClientID)
			replayed, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			require.NoError(t, err)
			require.True(t, replayed.Replay)
			require.Equal(t, result.Result, replayed.Result)
			require.Equal(t, int64(1), auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
			require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
		})
	}
}

func TestChatAnalysisWriteRejectsInvalidInputsAndNoOps(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_analysis_input")
	writer, _ := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	for _, input := range []PrepareChatAnalysisInput{
		{OrganizationID: f.orgA, Judge: "unknown", Enabled: true, DailyCap: 1, RetryKey: "judge"},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: -1, RetryKey: "negative"},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: chatanalysis.MaxDailyCap + 1, RetryKey: "oversized"},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: 1<<32 + 1, RetryKey: "narrowing"},
		{OrganizationID: "synthetic-a", Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 1, RetryKey: "slug"},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 1},
	} {
		_, err := writer.prepare(ctx, input)
		require.Error(t, err, input.RetryKey)
	}
	_, err := writer.prepare(ctx, PrepareChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, RetryKey: "noop"})
	require.ErrorIs(t, err, errChatAnalysisNoChange)
	writer.writes.Enabled = false
	_, err = writer.prepare(ctx, PrepareChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
}

func TestChatAnalysisWriteRejectsAbsentRowAndIdentityChanges(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_analysis_stale")
	writer, tools := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 10, RetryKey: "stale"})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	p, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	_, err = analysisrepo.New(f.db).UpsertChatAnalysisSettingForOrganizationJudge(t.Context(), analysisrepo.UpsertChatAnalysisSettingForOrganizationJudgeParams{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: false, DailyCap: 0})
	require.NoError(t, err)
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.ErrorIs(t, err, ErrStaleState, "an explicit row invalidates an absent-row snapshot even with the same values")
	p.Target.OrganizationID = f.orgB
	tx, err := f.db.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary for the writer's locked SQLc state check
	require.NoError(t, err)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(t.Context()) })
	_, err = writer.expectedState(t.Context(), tx, p)
	require.ErrorIs(t, err, ErrStaleState)
	p.PlatformGlobal = true
	_, err = writer.expectedState(t.Context(), tx, p)
	require.ErrorIs(t, err, ErrProposalInvalidated)
	require.Zero(t, auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
}

func TestChatAnalysisWriteRollsBackAuditFailure(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_analysis_rollback")
	writer, tools := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareChatAnalysisInput{OrganizationID: f.orgA, Judge: businessmemory.JudgeName, Enabled: true, DailyCap: 10, RetryKey: "rollback"})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	p, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionChatAnalysisSettingsUpsert))
	_, err = tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
	require.Error(t, err)
	state, err := chatanalysis.LoadSettings(t.Context(), f.db, f.orgA)
	require.NoError(t, err)
	require.True(t, state.IsDefault, "audit failure must also roll back first-row creation")
	p, err = f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	require.Equal(t, ProposalApproved, p.Status)
	require.Zero(t, countWriteEvents(t, f.db, id, "executed"))
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "execute_refused"))
}

func TestChatAnalysisWriteConcurrentFirstSettingExecutesOnce(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_analysis_concurrent")
	writer, tools := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	prepared, err := writer.prepare(ctx, PrepareChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 10, RetryKey: "concurrent"})
	require.NoError(t, err)
	id := uuid.MustParse(prepared.ProposalID)
	p, err := f.store.GetForOwner(t.Context(), id, f.owner)
	require.NoError(t, err)
	_, err = f.store.Approve(t.Context(), id, f.owner.SubjectURN, p.ProposalDigest, time.Now(), allowProposalBrowser, writer.revalidate)
	require.NoError(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			_, err := tools.execute(ctx, ProposalIDInput{ProposalID: prepared.ProposalID})
			results <- err
		})
	}
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
	require.Equal(t, 1, countWriteEvents(t, f.db, id, "executed"))
}

func TestChatAnalysisWriteRequiresWritableLiveStaff(t *testing.T) {
	t.Parallel()
	f := newProposalFixture(t, "admin_mcp_analysis_authority")
	writer, _ := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	principal, ok := principalFromContext(ctx)
	require.True(t, ok)
	principal.Scopes = []string{ScopeRead}
	_, err := writer.prepare(context.WithValue(ctx, principalKey{}, principal), PrepareChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteScope)
	principal.Scopes = []string{ScopeRead, ScopeWrite}
	principal.staff = nil
	_, err = writer.prepare(context.WithValue(ctx, principalKey{}, principal), PrepareChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteIdentity)
}
