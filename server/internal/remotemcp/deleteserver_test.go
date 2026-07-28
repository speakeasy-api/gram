package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestDeleteServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateServer(ctx, &gen.CreateServerPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "https://mcp.example.com",
		TransportType:    "streamable-http",
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)

	err = ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	// Verify get returns not found
	_, err = ti.service.GetServer(ctx, &gen.GetServerPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteServer_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// Deleting a non-existent server should succeed silently
	err := ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
}

// Deleting a server soft-deletes its headers, but the cascade is intentionally
// silent in the audit log: the parent's remote-mcp:delete entry covers it, and
// per-header entries would be noise. Individually removing a header still
// audits, which deleteserverheader_test.go asserts.
func TestDeleteServer_CascadeEmitsNoHeaderDeleteAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	createSecretHeader(t, ctx, ti, server.ID, "X-API-Key", "secret-value")

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Request-ID", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("X-Request-ID")
	}))
	require.NoError(t, err)

	_, err = ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-Tenant", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("acme")
	}))
	require.NoError(t, err)

	beforeHeaderDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)
	beforeServerDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               server.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}))

	// The cascade contributes no header delete entries, however many headers it
	// tombstoned.
	afterHeaderDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)
	require.Equal(t, beforeHeaderDeletes, afterHeaderDeletes)

	afterServerDeletes, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerDelete)
	require.NoError(t, err)
	require.Equal(t, beforeServerDeletes+1, afterServerDeletes)

	// The rows are still gone: no active header may survive pointing at the
	// tombstoned server.
	surviving, err := repo.New(ti.conn).ListHeadersByServerID(ctx, uuid.MustParse(server.ID))
	require.NoError(t, err)
	require.Empty(t, surviving)
}

// The cascade must not soft-delete headers when the parent delete resolves to
// nothing (e.g. a server in another project).
func TestDeleteServer_OtherProjectLeavesHeadersIntact(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherServer := seedOtherProjectServer(t, ctx, ti)

	remotemcptest.SeedHeader(t, ctx, ti.conn, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      otherServer.ID,
		ProjectID:              otherServer.ProjectID,
		Name:                   "X-Other",
		Description:            pgtype.Text{String: "", Valid: false},
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.ToPGText("other-value"),
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               otherServer.ID.String(),
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}))

	surviving, err := repo.New(ti.conn).ListHeadersByServerID(ctx, otherServer.ID)
	require.NoError(t, err)
	require.Len(t, surviving, 1)
}
