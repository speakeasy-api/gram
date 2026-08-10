package chatanalysis_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin_chat_analysis"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSettingsRequirePlatformAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	_, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.UpsertWorkUnitsSettings(ctx, &gen.UpsertWorkUnitsSettingsPayload{
		SessionToken: nil, WorkUnitsEnabled: true, WorkUnitsDailyCap: 100,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetSettingsReturnsPlatformDefaults(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	result, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{SessionToken: nil})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkUnitsEnabled:  false,
		WorkUnitsDailyCap: 0,
		IsDefault:         true,
	}, result)
}

func TestUpsertSettingsValidatesCap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	for _, cap := range []int{-1, 10001} {
		_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
			SessionToken: nil, WorkUnitsEnabled: true, WorkUnitsDailyCap: cap,
		})
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestUpsertSettingsPersistsAndAuditsBeforeAfter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)

	first, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		SessionToken: nil, WorkUnitsEnabled: true, WorkUnitsDailyCap: 250,
	})
	require.NoError(t, err)
	require.False(t, first.IsDefault)
	require.True(t, first.WorkUnitsEnabled)
	require.Equal(t, 250, first.WorkUnitsDailyCap)

	second, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		SessionToken: nil, WorkUnitsEnabled: false, WorkUnitsDailyCap: 10000,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID:    second.OrganizationID,
		WorkUnitsEnabled:  false,
		WorkUnitsDailyCap: 10000,
		IsDefault:         false,
	}, second)

	stored, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, second, stored)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, "chat_analysis_settings", record.SubjectType)
	require.False(t, record.ProjectID.Valid)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"judge": analysis.WorkUnitsJudgeName, "enabled": true, "daily_cap": float64(250),
	}, beforeSnapshot)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"judge": analysis.WorkUnitsJudgeName, "enabled": false, "daily_cap": float64(10000),
	}, afterSnapshot)
}

// TestUpsertSettingsIsVisibleToPipeline asserts the row the management API
// writes is the row the enqueue path reads through the caller's project.
func TestUpsertSettingsIsVisibleToPipeline(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		SessionToken: nil, WorkUnitsEnabled: true, WorkUnitsDailyCap: 42,
	})
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	rows, err := analysisrepo.New(ti.conn).GetChatAnalysisSettingsForProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, analysis.WorkUnitsJudgeName, rows[0].Judge.String)
	require.True(t, rows[0].Enabled.Bool)
	require.Equal(t, int32(42), rows[0].DailyCap.Int32)
}
