package tunneledmcp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

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

func (m *tunnelManager) serverListView(ctx context.Context, logger *slog.Logger, servers []repo.TunneledMcpServer) []*types.TunneledMcpServer {
	// One runtime lookup per server; fetch them concurrently so list latency
	// doesn't grow linearly with the project's server count (N sequential
	// Redis round trips otherwise).
	connections := make([][]mv.TunneledMcpConnectionCache, len(servers))
	var wg errgroup.Group
	wg.SetLimit(8)
	for i, server := range servers {
		wg.Go(func() error {
			connections[i] = m.connectionsForServer(ctx, logger, server.ID)
			return nil
		})
	}
	// Goroutines never return errors; connectionsForServer degrades to nil
	// and logs internally.
	_ = wg.Wait()

	connectionsByServerID := make(map[string][]mv.TunneledMcpConnectionCache, len(servers))
	for i, server := range servers {
		connectionsByServerID[server.ID.String()] = connections[i]
	}

	return mv.BuildTunneledMcpServerListView(servers, connectionsByServerID)
}

func (m *tunnelManager) serverView(ctx context.Context, logger *slog.Logger, server repo.TunneledMcpServer) *types.TunneledMcpServer {
	return mv.BuildTunneledMcpServerView(server, m.connectionsForServer(ctx, logger, server.ID))
}

func (m *tunnelManager) serverConnectionsView(ctx context.Context, logger *slog.Logger, serverID uuid.UUID) *types.TunneledMcpServerConnections {
	return mv.BuildTunneledMcpServerConnectionsView(m.connectionsForServer(ctx, logger, serverID))
}

func (m *tunnelManager) serverViewWithoutRuntime(server repo.TunneledMcpServer) *types.TunneledMcpServer {
	return mv.BuildTunneledMcpServerView(server, nil)
}

func (m *tunnelManager) connectionsForServer(ctx context.Context, logger *slog.Logger, serverID uuid.UUID) []mv.TunneledMcpConnectionCache {
	if m.runtime == nil {
		return nil
	}

	connections, err := m.runtime.Connections(ctx, serverID.String())
	if err != nil {
		// Degrade to "no live connections" for the management view, but leave
		// evidence: without this log a Redis outage looks identical to every
		// agent being disconnected.
		logger.ErrorContext(ctx, "load tunneled mcp connection cache", attr.SlogError(err), attr.SlogTunneledMCPServerID(serverID.String()))
		return nil
	}

	return connections
}

func (m *tunnelManager) deleteRuntimeState(ctx context.Context, logger *slog.Logger, serverID uuid.UUID) {
	if m.runtime == nil {
		return
	}
	// Runs after the DB commit: detach from request cancellation so a client
	// disconnect cannot skip cache cleanup and leave stale route/connection
	// entries pointing at a deleted or rotated source, but stay bounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tunnelID := serverID.String()
	if err := m.runtime.DeleteConnections(ctx, tunnelID); err != nil {
		logger.WarnContext(ctx, "delete tunneled mcp connection cache", attr.SlogError(err))
	}
	if err := m.runtime.Delete(ctx, tunnelID); err != nil {
		logger.WarnContext(ctx, "delete tunneled mcp route cache", attr.SlogError(err))
	}
}
