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

	_, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{OrganizationID: "target-org"})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.UpsertWorkUnitsSettings(ctx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: "target-org", WorkUnitsEnabled: true, WorkUnitsDailyCap: 100,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestChatAnalysisRejectsUnknownOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)

	tests := []struct {
		name string
		call func(t *testing.T)
	}{
		{
			name: "get settings",
			call: func(t *testing.T) {
				t.Helper()
				_, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: "missing-organization"})
				requireOopsCode(t, err, oops.CodeNotFound)
			},
		},
		{
			name: "upsert work units",
			call: func(t *testing.T) {
				t.Helper()
				_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
					OrganizationID: "missing-organization", WorkUnitsEnabled: true, WorkUnitsDailyCap: 100,
				})
				requireOopsCode(t, err, oops.CodeNotFound)
			},
		},
		{
			name: "upsert business memory",
			call: func(t *testing.T) {
				t.Helper()
				_, err := ti.service.UpsertBusinessMemorySettings(adminCtx, &gen.UpsertBusinessMemorySettingsPayload{
					OrganizationID: "missing-organization", BusinessMemoryEnabled: true, BusinessMemoryDailyCap: 100,
				})
				requireOopsCode(t, err, oops.CodeNotFound)
			},
		},
		{
			name: "trigger analysis",
			call: func(t *testing.T) {
				t.Helper()
				_, err := ti.service.TriggerAnalysis(adminCtx, &gen.TriggerAnalysisPayload{OrganizationID: "missing-organization"})
				requireOopsCode(t, err, oops.CodeNotFound)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.call(t)
			require.Empty(t, ti.signaler.Signaled())
		})
	}
}

func TestGetSettingsReturnsPlatformDefaults(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	result, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkUnitsEnabled:  false,
		WorkUnitsDailyCap: 0,
		IsDefault:         true,
	}, result)
}

func TestSettingsUseRequestedOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	activeOrganizationID := authCtx.ActiveOrganizationID
	targetOrganizationID := createTargetOrganization(t, ctx, ti)

	_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: activeOrganizationID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 11,
	})
	require.NoError(t, err)
	_, err = ti.service.UpsertBusinessMemorySettings(adminCtx, &gen.UpsertBusinessMemorySettingsPayload{
		OrganizationID: activeOrganizationID, BusinessMemoryEnabled: false, BusinessMemoryDailyCap: 22,
	})
	require.NoError(t, err)

	targetWorkUnits, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: targetOrganizationID, WorkUnitsEnabled: false, WorkUnitsDailyCap: 33,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID: targetOrganizationID, WorkUnitsDailyCap: 33, IsDefault: false,
	}, targetWorkUnits)

	targetBusinessMemory, err := ti.service.UpsertBusinessMemorySettings(adminCtx, &gen.UpsertBusinessMemorySettingsPayload{
		OrganizationID: targetOrganizationID, BusinessMemoryEnabled: true, BusinessMemoryDailyCap: 44,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID: targetOrganizationID, WorkUnitsDailyCap: 33,
		BusinessMemoryEnabled: true, BusinessMemoryDailyCap: 44, IsDefault: false,
	}, targetBusinessMemory)

	target, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: targetOrganizationID})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID: targetOrganizationID, WorkUnitsDailyCap: 33,
		BusinessMemoryEnabled: true, BusinessMemoryDailyCap: 44, IsDefault: false,
	}, target)

	active, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: activeOrganizationID})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID: activeOrganizationID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 11,
		BusinessMemoryDailyCap: 22, IsDefault: false,
	}, active)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, targetOrganizationID, record.OrganizationID)
	require.Equal(t, targetOrganizationID, record.SubjectID)
}

func TestSettingsDoNotRequireActiveOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	targetOrganizationID := createTargetOrganization(t, ctx, ti)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(adminCtx)
	require.True(t, ok)
	authCtxCopy := *authCtx
	authCtxCopy.ActiveOrganizationID = ""
	adminCtx = contextvalues.SetAuthContext(adminCtx, &authCtxCopy)

	result, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: targetOrganizationID})
	require.NoError(t, err)
	require.Equal(t, targetOrganizationID, result.OrganizationID)
}

func TestUpsertSettingsValidatesCap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	for _, cap := range []int{-1, 10001} {
		_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
			OrganizationID: authCtx.ActiveOrganizationID, WorkUnitsEnabled: true, WorkUnitsDailyCap: cap,
		})
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestUpsertSettingsPersistsAndAuditsBeforeAfter(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionChatAnalysisSettingsUpsert)
	require.NoError(t, err)

	first, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: authCtx.ActiveOrganizationID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 250,
	})
	require.NoError(t, err)
	require.False(t, first.IsDefault)
	require.True(t, first.WorkUnitsEnabled)
	require.Equal(t, 250, first.WorkUnitsDailyCap)

	second, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: authCtx.ActiveOrganizationID, WorkUnitsEnabled: false, WorkUnitsDailyCap: 10000,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.ChatAnalysisSettings{
		OrganizationID: second.OrganizationID, WorkUnitsDailyCap: 10000, IsDefault: false,
	}, second)

	stored, err := ti.service.GetSettings(adminCtx, &gen.GetSettingsPayload{OrganizationID: authCtx.ActiveOrganizationID})
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

func TestUpsertSettingsIsVisibleToPipeline(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	adminCtx := withAdmin(t, ctx)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.UpsertWorkUnitsSettings(adminCtx, &gen.UpsertWorkUnitsSettingsPayload{
		OrganizationID: authCtx.ActiveOrganizationID, WorkUnitsEnabled: true, WorkUnitsDailyCap: 42,
	})
	require.NoError(t, err)
	require.NotNil(t, authCtx.ProjectID)

	rows, err := analysisrepo.New(ti.conn).GetChatAnalysisSettingsForProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, analysis.WorkUnitsJudgeName, rows[0].Judge.String)
	require.True(t, rows[0].Enabled.Bool)
	require.Equal(t, int32(42), rows[0].DailyCap.Int32)
}
