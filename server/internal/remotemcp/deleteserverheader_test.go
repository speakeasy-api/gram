package remotemcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func TestDeleteServerHeader(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)

	require.Empty(t, listServerHeaders(t, ctx, ti, server.ID).Headers)
}

// Deleting an unknown header succeeds silently, matching deleteServer.
func TestDeleteServerHeader_NotFoundIsNoOp(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               uuid.NewString(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))
}

// Deleting is idempotent: the second call finds nothing to soft-delete and
// therefore must not emit a second audit event.
func TestDeleteServerHeader_RepeatDeleteEmitsNoAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	payload := &gen.DeleteServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
	require.NoError(t, ti.service.DeleteServerHeader(ctx, payload))

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, payload))

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteMcpServerHeaderDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

// Delete is a silent no-op on an unreachable id, so asserting the forbidden
// code is the only way to tell a denied delete apart from one that simply
// matched no row.
func TestDeleteServerHeader_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	restricted := withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeMCPRead, Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String())})

	err = ti.service.DeleteServerHeader(restricted, &gen.DeleteServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	// The header must still be there: a denied delete may not mutate anything.
	// Read it straight from the repo, since the restricted context cannot list.
	surviving, err := repo.New(ti.conn).ListServerHeaders(ctx, repo.ListServerHeadersParams{
		RemoteMcpServerID: uuid.MustParse(server.ID),
		ProjectID:         *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, surviving, 1)
	require.Equal(t, created.ID, surviving[0].ID.String())
}

// A header in another project must not be deletable by id. Silent-no-op on
// not-found means the assertion has to be that the row survives.
func TestDeleteServerHeader_OtherProjectLeavesRowIntact(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	otherServer := seedOtherProjectServer(t, ctx, ti)

	otherHeader := remotemcptest.SeedHeader(t, ctx, ti.conn, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      otherServer.ID,
		ProjectID:              otherServer.ProjectID,
		Name:                   "X-Other",
		Description:            pgtype.Text{String: "", Valid: false},
		IsRequired:             false,
		IsSecret:               false,
		Value:                  conv.ToPGText("other-value"),
		ValueFromRequestHeader: pgtype.Text{String: "", Valid: false},
	})

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               otherHeader.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	surviving, err := repo.New(ti.conn).ListServerHeaders(ctx, repo.ListServerHeadersParams{
		RemoteMcpServerID: otherServer.ID,
		ProjectID:         otherServer.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, surviving, 1)
	require.Equal(t, otherHeader.ID, surviving[0].ID)
}
