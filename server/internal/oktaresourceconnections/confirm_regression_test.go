package oktaresourceconnections_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	idprepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestConfirm_SharedResourceBatch(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Shared")
	first, backend := createServerWithBackend(t, ctx, si, f.projectID, f.issuerID, uuid.NullUUID{}, "First")
	second, err := si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{
		ProjectID: f.projectID, Name: conv.ToPGText("Second"), Slug: conv.ToPGText("second"),
		RemoteMcpServerID: uuid.NullUUID{UUID: backend, Valid: true}, RemoteSessionIssuerID: uuid.NullUUID{UUID: f.issuerID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	for _, tc := range []struct {
		name          string
		id            uuid.UUID
		otherAudience string
		appID         *string
	}{
		{"alias audience", second, audience + "/other", nil},
		{"alias application", second, audience, conv.PtrEmpty("0oa1234567890abcdef")},
		{"same server audience", first, audience + "/other", nil},
		{"same server application", first, audience, conv.PtrEmpty("0oa1234567890abcdef")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := si.svc.Confirm(ctx, &gen.ConfirmPayload{Connections: []*gen.OktaResourceConnectionConfirmation{
				{McpServerID: first.String(), Audience: audience},
				{McpServerID: tc.id.String(), Audience: tc.otherAudience, OktaApplicationID: tc.appID},
			}})
			requireOopsCode(t, err, oops.CodeBadRequest)
			rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
			require.NoError(t, err)
			require.Empty(t, rows)
			count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionConfirm)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
	result, err := confirm(t, ctx, si, audience, nil, first, second, first)
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)
	require.Equal(t, result.Servers[0].Audience, result.Servers[1].Audience)
	require.Equal(t, result.Servers[0].ConfirmedAt, result.Servers[1].ConfirmedAt)
	count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionConfirm)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "one write/audit for the actual upstream")
}

func TestConfirm_AgentChangesWhileWaitingForConnectionLock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, agentID, agentAppID string
	}{
		{"changed agent", "wlp2", ""},
		{"changed application only", "wlp1", "0oa1234567890abcdef"},
		{"cleared agent", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, si := newTestService(t)
			recordAgent(t, ctx, si, "wlp1")
			f := capableServer(t, ctx, si, "Concurrent")
			tx, err := si.conn.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = repo.New(tx).LockLiveConnection(ctx, repo.LockLiveConnectionParams{ID: si.connectionID, OrganizationID: si.orgID})
			require.NoError(t, err)
			done := make(chan error, 1)
			go func() {
				_, err := si.svc.Confirm(ctx, &gen.ConfirmPayload{Connections: []*gen.OktaResourceConnectionConfirmation{{McpServerID: f.serverID.String(), Audience: audience}}})
				done <- err
			}()
			// Wait until Confirm has read its snapshot and is blocked on our lock.
			require.Eventually(t, func() bool {
				var waiting bool
				err := si.conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND $1 = ANY(pg_blocking_pids(pid)))", int32(tx.Conn().PgConn().PID())).Scan(&waiting)
				return err == nil && waiting
			}, 5*time.Second, 10*time.Millisecond)
			_, err = idprepo.New(tx).UpdateOktaIdentityProviderConnectionAgent(ctx, idprepo.UpdateOktaIdentityProviderConnectionAgentParams{
				OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID, AgentID: conv.ToPGTextEmpty(tc.agentID), AgentAppID: conv.ToPGTextEmpty(tc.agentAppID),
			})
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			select {
			case err := <-done:
				requireOopsCode(t, err, oops.CodeConflict)
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not finish")
			}
			rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
			require.NoError(t, err)
			require.Empty(t, rows)
			result, err := confirm(t, ctx, si, audience, nil, f.serverID)
			require.NoError(t, err)
			if tc.agentID == "" {
				require.Equal(t, "needs_agent", result.Servers[0].State)
				require.Nil(t, result.Servers[0].DeepLink)
			} else {
				require.Equal(t, "connected", result.Servers[0].State)
				require.Contains(t, conv.PtrValOr(result.Servers[0].DeepLink, ""), "/"+tc.agentID+"/")
			}
		})
	}
}

func TestReset_RequiresRolloutEnabled(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Reset")
	_, err := confirm(t, ctx, si, audience, nil, f.serverID)
	require.NoError(t, err)
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, false)
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{McpServerID: f.serverID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	count, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionReset)
	require.NoError(t, err)
	require.Zero(t, count)
}
