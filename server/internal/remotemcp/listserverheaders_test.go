package remotemcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/remotemcptest"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
)

func listServerHeaders(t *testing.T, ctx context.Context, ti *testInstance, serverID string) *gen.ListServerHeadersResult {
	t.Helper()

	result, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{
		RemoteMcpServerID: serverID,
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.NoError(t, err)

	return result
}

func TestListServerHeaders_OrderedByNameAndRedacted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	createSecretHeader(t, ctx, ti, server.ID, "X-API-Key", "secret-value")

	_, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "Authorization", func(p *gen.CreateServerHeaderPayload) {
		p.ValueFromRequestHeader = new("Authorization")
	}))
	require.NoError(t, err)

	result := listServerHeaders(t, ctx, ti, server.ID)
	require.Len(t, result.Headers, 2)

	// ORDER BY name
	require.Equal(t, "Authorization", result.Headers[0].Name)
	require.Equal(t, "X-API-Key", result.Headers[1].Name)

	require.True(t, result.Headers[1].IsSecret)
	require.NotNil(t, result.Headers[1].Value)
	require.Equal(t, "***", *result.Headers[1].Value)
}

func TestListServerHeaders_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	result := listServerHeaders(t, ctx, ti, server.ID)
	require.Empty(t, result.Headers)
}

func TestListServerHeaders_UnknownServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{
		RemoteMcpServerID: uuid.NewString(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A server in another project must be indistinguishable from one that does not
// exist: a 404, never that project's headers and never a bare empty list.
func TestListServerHeaders_OtherProjectNotFound(t *testing.T) {
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

	_, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{
		RemoteMcpServerID: otherServer.ID.String(),
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// A soft-deleted server is gone, not empty.
func TestListServerHeaders_DeletedServerNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	require.NoError(t, ti.service.DeleteServer(ctx, &gen.DeleteServerPayload{
		ID:               server.ID,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	}))

	_, err := ti.service.ListServerHeaders(ctx, &gen.ListServerHeadersPayload{
		RemoteMcpServerID: server.ID,
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
	})
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListServerHeaders_ExcludesDeleted(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := createTestServer(t, ctx, ti)

	created, err := ti.service.CreateServerHeader(ctx, newCreateServerHeaderPayload(server.ID, "X-API-Key", func(p *gen.CreateServerHeaderPayload) {
		p.Value = new("value")
	}))
	require.NoError(t, err)

	require.NoError(t, ti.service.DeleteServerHeader(ctx, &gen.DeleteServerHeaderPayload{
		ID:               created.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	result := listServerHeaders(t, ctx, ti, server.ID)
	require.Empty(t, result.Headers)
}
