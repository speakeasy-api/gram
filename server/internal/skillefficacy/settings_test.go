//nolint:paralleltest // Tests share the seeded organization's product feature cache.
package skillefficacy_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skill_efficacy"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	analysisrepo "github.com/speakeasy-api/gram/server/internal/chat/analysis/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGetSettingsReturnsPlatformDefaults(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withGrant(t, ctx, authz.ScopeOrgRead)

	result, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.Equal(t, &gen.SkillEfficacySettings{
		OrganizationID: authCtx.ActiveOrganizationID,
		// Scoring is opt-in per judge: an organization with no settings row is
		// off, and the default cap is only what it would run on when enabled.
		Enabled:   false,
		DailyCap:  int(analysis.DefaultJudgeDailyCap),
		IsDefault: true,
	}, result)
}

func TestSettingsRequireProductFeature(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, false)

	_, err := ti.service.GetSettings(withGrant(t, ctx, authz.ScopeOrgRead), &gen.GetSettingsPayload{ApikeyToken: nil, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.UpsertSettings(withGrant(t, ctx, authz.ScopeOrgAdmin), &gen.UpsertSettingsPayload{
		ApikeyToken: nil, SessionToken: nil, Enabled: true,
		DailyCap: 2,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestSettingsEnforceOrganizationScopes(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, true)

	_, err := ti.service.GetSettings(authztest.WithExactGrants(t, ctx), &gen.GetSettingsPayload{ApikeyToken: nil, SessionToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.UpsertSettings(withGrant(t, ctx, authz.ScopeOrgRead), &gen.UpsertSettingsPayload{
		ApikeyToken: nil, SessionToken: nil, Enabled: true,
		DailyCap: 2,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestUpsertSettingsValidatesCaps(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, true)
	ctx = withGrant(t, ctx, authz.ScopeOrgAdmin)

	invalid := []*gen.UpsertSettingsPayload{
		{ApikeyToken: nil, SessionToken: nil, Enabled: true, DailyCap: -1},
		{ApikeyToken: nil, SessionToken: nil, Enabled: true, DailyCap: 10001},
	}
	for _, payload := range invalid {
		_, err := ti.service.UpsertSettings(ctx, payload)
		requireOopsCode(t, err, oops.CodeInvalid)
	}
}

func TestUpsertSettingsPersistsAndAuditsBeforeAfter(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, true)
	adminCtx := withGrant(t, ctx, authz.ScopeOrgAdmin)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)

	first, err := ti.service.UpsertSettings(adminCtx, &gen.UpsertSettingsPayload{
		ApikeyToken: nil, SessionToken: nil, Enabled: true,
		DailyCap: 0,
	})
	require.NoError(t, err)
	require.False(t, first.IsDefault)

	second, err := ti.service.UpsertSettings(adminCtx, &gen.UpsertSettingsPayload{
		ApikeyToken: nil, SessionToken: nil, Enabled: false,
		DailyCap: 10000,
	})
	require.NoError(t, err)
	require.Equal(t, &gen.SkillEfficacySettings{
		OrganizationID: second.OrganizationID,
		Enabled:        false,
		DailyCap:       10000,
		IsDefault:      false,
	}, second)

	stored, err := ti.service.GetSettings(withGrant(t, ctx, authz.ScopeOrgRead), &gen.GetSettingsPayload{ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, second, stored)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, "skill_efficacy_settings", record.SubjectType)
	require.False(t, record.ProjectID.Valid)

	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"enabled": true, "daily_cap": float64(0),
	}, beforeSnapshot)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"enabled": false, "daily_cap": float64(10000),
	}, afterSnapshot)
}

func TestConcurrentUpsertSettingsAuditsCommittedTransitions(t *testing.T) {
	ctx, ti := newTestService(t)
	setSkillsFeature(t, ctx, ti, true)
	adminCtx := withGrant(t, ctx, authz.ScopeOrgAdmin)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	_, err := ti.service.UpsertSettings(adminCtx, &gen.UpsertSettingsPayload{
		ApikeyToken: nil, SessionToken: nil, Enabled: true,
		DailyCap: 10,
	})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)

	lockTx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, analysisrepo.New(lockTx).LockOrganizationChatAnalysisBudget(ctx, authCtx.ActiveOrganizationID))

	payloads := []*gen.UpsertSettingsPayload{
		{ApikeyToken: nil, SessionToken: nil, Enabled: true, DailyCap: 20},
		{ApikeyToken: nil, SessionToken: nil, Enabled: false, DailyCap: 30},
	}
	results := make(chan error, len(payloads))
	var wg sync.WaitGroup
	for _, payload := range payloads {
		wg.Go(func() {
			_, callErr := ti.service.UpsertSettings(adminCtx, payload)
			results <- callErr
		})
	}
	require.Never(t, func() bool { return len(results) > 0 }, 100*time.Millisecond, 10*time.Millisecond)
	require.NoError(t, lockTx.Commit(ctx))
	wg.Wait()
	close(results)
	for callErr := range results {
		require.NoError(t, callErr)
	}

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
	stored, err := ti.service.GetSettings(withGrant(t, ctx, authz.ScopeOrgRead), &gen.GetSettingsPayload{ApikeyToken: nil, SessionToken: nil})
	require.NoError(t, err)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSkillEfficacySettingsUpsert)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.InEpsilon(t, float64(stored.DailyCap), afterSnapshot["daily_cap"], 1e-9)
	require.NotEqual(t, float64(10), beforeSnapshot["daily_cap"])
}
