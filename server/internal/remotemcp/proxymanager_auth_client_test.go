package remotemcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestBuildWithAuthClientHeadersDropsServerCredentials(t *testing.T) {
	t.Parallel()

	logger := testenv.NewLogger(t)
	manager := NewProxyManager(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	server := &remotemcprepo.RemoteMcpServer{ID: uuid.New(), Url: "https://example.com/mcp"}
	headers := []remotemcprepo.RemoteMcpServerHeader{
		{Name: "Authorization", Value: pgtype.Text{String: "Bearer server", Valid: true}},
		{Name: "X-Api-Key", Value: pgtype.Text{String: "server-key", Valid: true}, IsSecret: true},
		{Name: "X-Forwarded-Key", ValueFromRequestHeader: pgtype.Text{String: "X-Caller", Valid: true}},
		{Name: "X-Workspace", Value: pgtype.Text{String: "server-ws", Valid: true}},
	}
	client := []proxy.ConfiguredHeader{{Name: "X-Api-Key", StaticValue: "client-key"}}

	built := manager.Build(logger, server, uuid.NewString(), headers, mcpservers.VisibilityPrivate, "organization-id", "project-id", "", "", nil, WithAuthClientHeaders(client))
	require.Equal(t, []proxy.ConfiguredHeader{
		{Name: "X-Workspace", StaticValue: "server-ws"},
		{Name: "X-Api-Key", StaticValue: "client-key"},
	}, built.Headers)

	legacy := manager.Build(logger, server, uuid.NewString(), headers, mcpservers.VisibilityPrivate, "organization-id", "project-id", "", "", nil)
	require.Equal(t, []proxy.ConfiguredHeader{
		{Name: "Authorization", StaticValue: "Bearer server"},
		{Name: "X-Api-Key", StaticValue: "server-key"},
		{Name: "X-Forwarded-Key", ValueFromRequestHeader: "X-Caller"},
		{Name: "X-Workspace", StaticValue: "server-ws"},
	}, legacy.Headers, "without a resolved client every server header is kept, in order")

	none := manager.Build(logger, server, uuid.NewString(), headers, mcpservers.VisibilityPrivate, "organization-id", "project-id", "", "", nil, WithAuthClientHeaders(nil))
	require.Equal(t, []proxy.ConfiguredHeader{{Name: "X-Workspace", StaticValue: "server-ws"}}, none.Headers, "a client with no headers still drops server credentials")
}
