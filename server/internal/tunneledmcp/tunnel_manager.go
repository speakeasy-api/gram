package tunneledmcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const tunnelKeyPreviewBytes = 5

type tunnelManager struct {
	runtime route.RuntimeStore
}

type issuedTunnelKey struct {
	Plaintext string
	Hash      string
	Prefix    string
}

func newTunnelManager(runtime route.RuntimeStore) *tunnelManager {
	return &tunnelManager{runtime: runtime}
}

func (m *tunnelManager) issueKey() (issuedTunnelKey, error) {
	plaintext, hash, err := wire.NewKey()
	if err != nil {
		return issuedTunnelKey{
			Plaintext: "",
			Hash:      "",
			Prefix:    "",
		}, fmt.Errorf("issue tunnel key: %w", err)
	}

	return issuedTunnelKey{
		Plaintext: plaintext,
		Hash:      hash,
		Prefix:    plaintext[:len(wire.KeyPrefix)+tunnelKeyPreviewBytes],
	}, nil
}

func (m *tunnelManager) serverListView(ctx context.Context, servers []repo.TunneledMcpServer) []*types.TunneledMcpServer {
	connectionsByServerID := make(map[string][]mv.TunneledMcpConnectionCache, len(servers))
	for _, server := range servers {
		connectionsByServerID[server.ID.String()] = m.connectionsForServer(ctx, server.ID)
	}

	return mv.BuildTunneledMcpServerListView(servers, connectionsByServerID)
}

func (m *tunnelManager) serverView(ctx context.Context, server repo.TunneledMcpServer) *types.TunneledMcpServer {
	return mv.BuildTunneledMcpServerView(server, m.connectionsForServer(ctx, server.ID))
}

func (m *tunnelManager) serverConnectionsView(ctx context.Context, serverID uuid.UUID) *types.TunneledMcpServerConnections {
	return mv.BuildTunneledMcpServerConnectionsView(m.connectionsForServer(ctx, serverID))
}

func (m *tunnelManager) serverViewWithoutRuntime(server repo.TunneledMcpServer) *types.TunneledMcpServer {
	return mv.BuildTunneledMcpServerView(server, nil)
}

func (m *tunnelManager) connectionsForServer(ctx context.Context, serverID uuid.UUID) []mv.TunneledMcpConnectionCache {
	if m.runtime == nil {
		return nil
	}

	connections, err := m.runtime.Connections(ctx, serverID.String())
	if err != nil {
		return nil
	}

	return connections
}

func (m *tunnelManager) deleteRuntimeState(ctx context.Context, logger *slog.Logger, serverID uuid.UUID) {
	if m.runtime == nil {
		return
	}
	tunnelID := serverID.String()
	if err := m.runtime.DeleteConnections(ctx, tunnelID); err != nil {
		logger.WarnContext(ctx, "delete tunneled mcp connection cache", attr.SlogError(err))
	}
	if err := m.runtime.Delete(ctx, tunnelID); err != nil {
		logger.WarnContext(ctx, "delete tunneled mcp route cache", attr.SlogError(err))
	}
}
