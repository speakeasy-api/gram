package adminmcp

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
)

func newChatAnalysisWriter(f writeFixture) *chatAnalysisWriter {
	writes := WriteConfig{Enabled: true, Operations: map[WriteOperation]bool{OperationSetChatAnalysisSettings: true}} //nolint:exhaustive // Only the tested operation is enabled.
	return &chatAnalysisWriter{runner: f.runner, audit: audit.NewLogger(), writes: writes}
}

func TestChatAnalysisWriteApplies(t *testing.T) {
	t.Parallel()
	for _, judge := range []string{analysis.WorkUnitsJudgeName, businessmemory.JudgeName} {
		t.Run(judge, func(t *testing.T) {
			t.Parallel()
			f := newWriteFixture(t, "admin_mcp_analysis_success")
			writer := newChatAnalysisWriter(f)
			ctx := writeContext(t, f)
			input := ChatAnalysisInput{OrganizationID: f.orgA, Judge: judge, Enabled: true, DailyCap: 25}
			receipt, err := writer.apply(ctx, input)
			require.NoError(t, err)
			require.Equal(t, f.orgA, receipt.OrganizationID)
			require.Equal(t, "Synthetic A", receipt.Name)
			require.True(t, receipt.Before.IsDefault)
			require.True(t, receipt.After.Enabled)
			require.Equal(t, 25, receipt.After.DailyCap)
			require.Contains(t, receipt.SideEffects, "not currency")
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
			_, err = writer.apply(ctx, input)
			require.ErrorIs(t, err, errChatAnalysisNoChange, "repeating the call changes nothing")
			require.Equal(t, int64(1), auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
			require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetChatAnalysisSettings, f.orgA, "executed"))
		})
	}
}

func TestChatAnalysisWriteRejectsInvalidInputsAndNoOps(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_analysis_input")
	writer := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	for _, input := range []ChatAnalysisInput{
		{OrganizationID: f.orgA, Judge: "unknown", Enabled: true, DailyCap: 1},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: -1},
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: chatanalysis.MaxDailyCap + 1},
		// Validate before narrowing: converting this value to int32 first would produce the valid cap 1.
		{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, DailyCap: 1<<32 + 1},
		{OrganizationID: "synthetic-a", Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 1},
	} {
		_, err := writer.apply(ctx, input)
		require.Error(t, err)
	}
	_, err := writer.apply(ctx, ChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName})
	require.ErrorIs(t, err, errChatAnalysisNoChange)
	require.Zero(t, auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
	writer.writes.Enabled = false
	_, err = writer.apply(ctx, ChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteDisabled)
}

func TestChatAnalysisWriteReportsLiveSettingsAndRejectsUnknownTarget(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_analysis_live")
	writer := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	_, err := analysisrepo.New(f.db).UpsertChatAnalysisSettingForOrganizationJudge(t.Context(), analysisrepo.UpsertChatAnalysisSettingForOrganizationJudgeParams{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 7})
	require.NoError(t, err)
	receipt, err := writer.apply(ctx, ChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 10})
	require.NoError(t, err)
	require.False(t, receipt.Before.IsDefault)
	require.Equal(t, 7, receipt.Before.DailyCap)
	_, err = writer.apply(ctx, ChatAnalysisInput{OrganizationID: "org_missing", Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 10})
	require.ErrorIs(t, err, ErrStaleState)
}

func TestChatAnalysisWriteRollsBackAuditFailure(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_analysis_rollback")
	writer := newChatAnalysisWriter(f)
	require.NoError(t, audittest.RejectAction(t.Context(), f.db, audit.ActionChatAnalysisSettingsUpsert))
	_, err := writer.apply(writeContext(t, f), ChatAnalysisInput{OrganizationID: f.orgA, Judge: businessmemory.JudgeName, Enabled: true, DailyCap: 10})
	require.Error(t, err)
	state, err := chatanalysis.LoadSettings(t.Context(), f.db, f.orgA)
	require.NoError(t, err)
	require.True(t, state.IsDefault, "audit failure must also roll back first-row creation")
	require.Zero(t, countWriteEvents(t, f.db, OperationSetChatAnalysisSettings, f.orgA, "executed"))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetChatAnalysisSettings, f.orgA, "failed"))
}

func TestChatAnalysisWriteConcurrentFirstSettingWritesOnce(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_analysis_concurrent")
	writer := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	input := ChatAnalysisInput{OrganizationID: f.orgA, Judge: analysis.WorkUnitsJudgeName, Enabled: true, DailyCap: 10}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			_, err := writer.apply(ctx, input)
			results <- err
		})
	}
	close(start)
	group.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
			continue
		}
		require.ErrorIs(t, err, errChatAnalysisNoChange)
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, int64(1), auditCount(t, f, audit.ActionChatAnalysisSettingsUpsert))
	require.Equal(t, 1, countWriteEvents(t, f.db, OperationSetChatAnalysisSettings, f.orgA, "executed"))
}

func TestChatAnalysisWriteRequiresWritableLiveStaff(t *testing.T) {
	t.Parallel()
	f := newWriteFixture(t, "admin_mcp_analysis_authority")
	writer := newChatAnalysisWriter(f)
	ctx := writeContext(t, f)
	principal, ok := principalFromContext(ctx)
	require.True(t, ok)
	principal.Scopes = []string{ScopeRead}
	_, err := writer.apply(context.WithValue(ctx, principalKey{}, principal), ChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteScope)
	principal.Scopes = []string{ScopeRead, ScopeWrite}
	principal.staff = nil
	_, err = writer.apply(context.WithValue(ctx, principalKey{}, principal), ChatAnalysisInput{})
	require.ErrorIs(t, err, ErrWriteIdentity)
}
