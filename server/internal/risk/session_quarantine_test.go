package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
)

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

	circuit, err := sessionquarantine.Read(ctx, ti.cacheAdapter, sessionID)
	require.NoError(t, err)
	require.Nil(t, circuit)

	_, err = riskrepo.New(ti.conn).GetActiveSessionQuarantineBySession(ctx, sessionID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	auditCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionQuarantineRelease)
	require.NoError(t, err)
	require.Equal(t, int64(1), auditCount)
}
