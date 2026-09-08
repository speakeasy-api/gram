package risk_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
)

type deleteErrorCache struct {
	cache.Cache
	attempts atomic.Int32
}

func (c *deleteErrorCache) Delete(_ context.Context, _ string) error {
	c.attempts.Add(1)
	return errors.New("redis unavailable")
}

func TestReleaseSessionQuarantineClearsCircuitAndAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	sessionID := "release-quarantine-" + uuid.NewString()
	row, err := riskrepo.New(ti.conn).CreateSessionQuarantine(ctx, riskrepo.CreateSessionQuarantineParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		SessionID:      sessionID,
		RiskPolicyID:   uuid.NullUUID{},
		RiskPolicyName: "Release test policy",
		UserID:         authCtx.UserID,
		Reason:         "release test",
	})
	require.NoError(t, err)
	require.NoError(t, sessionquarantine.Write(ctx, ti.cacheAdapter, sessionquarantine.FromRow(row)))

	result, err := ti.service.ReleaseSessionQuarantine(ctx, &gen.ReleaseSessionQuarantinePayload{ID: row.ID.String()})
	require.NoError(t, err)
	require.Equal(t, row.ID.String(), result.ID)
	require.NotNil(t, result.ReleasedAt)

	circuit, err := sessionquarantine.Read(ctx, ti.cacheAdapter, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), sessionID)
	require.NoError(t, err)
	require.Nil(t, circuit)

	_, err = riskrepo.New(ti.conn).GetActiveSessionQuarantineBySession(ctx, riskrepo.GetActiveSessionQuarantineBySessionParams{
		SessionID:      sessionID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)

	auditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionQuarantineRelease)
	require.NoError(t, err)
	require.Equal(t, int64(1), auditCount)
}

func TestReleaseSessionQuarantineSucceedsWhenCircuitDeleteExhaustsRetries(t *testing.T) {
	t.Parallel()

	var failingCache *deleteErrorCache
	ctx, ti := newTestRiskService(t, func(ti *testInstance) {
		failingCache = &deleteErrorCache{Cache: ti.cacheAdapter}
		ti.cacheAdapter = failingCache
	})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeOrgAdmin,
		Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	})

	row, err := riskrepo.New(ti.conn).CreateSessionQuarantine(ctx, riskrepo.CreateSessionQuarantineParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		SessionID:      "release-delete-failure-" + uuid.NewString(),
		RiskPolicyID:   uuid.NullUUID{},
		RiskPolicyName: "Release retry policy",
		UserID:         authCtx.UserID,
		Reason:         "release retry test",
	})
	require.NoError(t, err)
	require.NoError(t, sessionquarantine.Write(ctx, failingCache.Cache, sessionquarantine.FromRow(row)))

	result, err := ti.service.ReleaseSessionQuarantine(ctx, &gen.ReleaseSessionQuarantinePayload{ID: row.ID.String()})
	require.NoError(t, err)
	require.NotNil(t, result.ReleasedAt)
	require.Equal(t, int32(3), failingCache.attempts.Load())

	_, err = riskrepo.New(ti.conn).GetActiveSessionQuarantineBySession(ctx, riskrepo.GetActiveSessionQuarantineBySessionParams{
		SessionID:      row.SessionID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSessionQuarantineTenantScopeAllowsSharedSessionIDs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Other quarantine project",
		Slug:           "other-quarantine-" + uuid.NewString()[:8],
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	sessionID := "shared-session-" + uuid.NewString()
	rows := make([]riskrepo.SessionQuarantine, 0, 2)
	for _, projectID := range []uuid.UUID{*authCtx.ProjectID, otherProject.ID} {
		row, err := riskrepo.New(ti.conn).CreateSessionQuarantine(ctx, riskrepo.CreateSessionQuarantineParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      projectID,
			SessionID:      sessionID,
			RiskPolicyID:   uuid.NullUUID{},
			RiskPolicyName: "Tenant scope policy",
			UserID:         authCtx.UserID,
			Reason:         "tenant scope test",
		})
		require.NoError(t, err)
		require.NoError(t, sessionquarantine.Write(ctx, ti.cacheAdapter, sessionquarantine.FromRow(row)))
		rows = append(rows, row)
	}

	for _, row := range rows {
		stored, err := riskrepo.New(ti.conn).GetActiveSessionQuarantineBySession(ctx, riskrepo.GetActiveSessionQuarantineBySessionParams{
			SessionID:      sessionID,
			OrganizationID: row.OrganizationID,
			ProjectID:      row.ProjectID,
		})
		require.NoError(t, err)
		require.Equal(t, row.ID, stored.ID)

		circuit, err := sessionquarantine.Read(ctx, ti.cacheAdapter, row.OrganizationID, row.ProjectID.String(), sessionID)
		require.NoError(t, err)
		require.NotNil(t, circuit)
		require.Equal(t, row.ProjectID.String(), circuit.ProjectID)
	}
}
