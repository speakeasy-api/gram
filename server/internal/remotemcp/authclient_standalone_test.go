package remotemcp_test

import (
	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCreateServerHeader_StandaloneTypedClient(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"static_credential", "pass_through"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			created := createSourceAndServer(t, ctx, ti)
			auth, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			q := authclientbackfill.New(ti.conn)
			beforeGrants, err := q.ListAuthClientGrantRows(ctx, authclientbackfill.ListAuthClientGrantRowsParams{OrganizationID: auth.ActiveOrganizationID, Scopes: []string{"mcp:connect", "mcp:use", "mcp:auth"}})
			require.NoError(t, err)
			header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(created.RemoteMcpServer.ID, "X-Upstream-Key", func(p *gen.CreateServerHeaderPayload) {
				if kind == "static_credential" {
					p.Value = new("standalone-secret")
					p.IsSecret = new(true)
				} else {
					p.ValueFromRequestHeader = new("X-Caller-Key")
				}
			}))
			require.NoError(t, err)
			id := authclientbackfill.SourceClientID(auth.ActiveOrganizationID, authclientbackfill.SourceKindRemote, uuid.MustParse(created.RemoteMcpServer.ID), kind)
			got, live, found := authClientState(t, ctx, ti, id)
			require.True(t, found)
			require.True(t, live)
			require.Equal(t, kind, got)
			require.Equal(t, []uuid.UUID{uuid.MustParse(created.McpServer.ID)}, liveLinkedServers(t, ctx, ti, id))
			copied, found := liveHeaderCopy(t, ctx, ti, id, "X-Upstream-Key")
			require.True(t, found)
			if kind == "static_credential" {
				require.Equal(t, storedHeaderValue(t, ctx, ti, header.ID), copied.Value)
			}
			_, err = ti.service.UpdateServerHeader(ctx, newUpdateServerHeaderPayload(header.ID, "X-Upstream-Key", func(p *gen.UpdateServerHeaderPayload) {
				if kind == "static_credential" {
					p.Value = new("rotated-secret")
					p.IsSecret = new(true)
				} else {
					p.ValueFromRequestHeader = new("X-Caller-Key")
				}
			}))
			require.NoError(t, err)
			require.Equal(t, []uuid.UUID{uuid.MustParse(created.McpServer.ID)}, liveLinkedServers(t, ctx, ti, id))
			afterGrants, err := q.ListAuthClientGrantRows(ctx, authclientbackfill.ListAuthClientGrantRowsParams{OrganizationID: auth.ActiveOrganizationID, Scopes: []string{"mcp:connect", "mcp:use", "mcp:auth"}})
			require.NoError(t, err)
			require.Equal(t, beforeGrants, afterGrants, "setup must not grant access")
		})
	}
}

func TestCreateServerHeader_StandaloneCopyFailureRollsBack(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	created := createSourceAndServer(t, ctx, ti)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	// This fixture owns its database. Fail after client creation, while copying.
	_, err := ti.conn.Exec(ctx, `ALTER TABLE mcp_auth_client_headers ADD CONSTRAINT reject_standalone_copy CHECK (name <> 'X-Upstream-Key')`)
	require.NoError(t, err)
	header, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(created.RemoteMcpServer.ID, "X-Upstream-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value, p.IsSecret = new("rollback-secret"), new(true)
	}))
	require.Error(t, err)
	require.Nil(t, header)
	id := authclientbackfill.SourceClientID(auth.ActiveOrganizationID, authclientbackfill.SourceKindRemote, uuid.MustParse(created.RemoteMcpServer.ID), "static_credential")
	_, _, found := authClientState(t, ctx, ti, id)
	require.False(t, found)
	require.Empty(t, liveLinkedServers(t, ctx, ti, id))
	var headers int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM remote_mcp_server_headers WHERE remote_mcp_server_id = $1 AND deleted IS FALSE`, created.RemoteMcpServer.ID).Scan(&headers))
	require.Zero(t, headers, "normal header write committed despite failed typed copy")
}
