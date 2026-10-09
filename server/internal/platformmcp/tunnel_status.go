package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/route"
)

// tunnelStatusReadTimeout bounds the optional tunnel enrichment of one MCP
// read: the source lookup and the runtime-store read share it. The runtime
// store is a single Redis read, so two seconds only expires when the store or
// the database is degraded, and the MCP read then still answers with unknown.
const tunnelStatusReadTimeout = 2 * time.Second

// TunnelConnectionStatus is the agent connection state of the tunnel behind a
// tunneled MCP server.
type TunnelConnectionStatus string

const (
	// TunnelConnectionConnected means at least one tunnel agent currently holds
	// an unexpired connection to the gateway.
	TunnelConnectionConnected TunnelConnectionStatus = "connected"

	// TunnelConnectionInactive means no agent is connected now, but the tunnel
	// has been used before.
	TunnelConnectionInactive TunnelConnectionStatus = "inactive"

	// TunnelConnectionNeverConnected means no agent has ever connected with
	// this tunnel's key.
	TunnelConnectionNeverConnected TunnelConnectionStatus = "never_connected"

	// TunnelConnectionUnknown means the connection state could not be read. It
	// says nothing about whether the agent is running.
	TunnelConnectionUnknown TunnelConnectionStatus = "unknown"
)

// MCPTunnel is the agent connection evidence for a tunneled MCP server. It
// reports whether a tunnel agent is connected to the Speakeasy gateway, not
// whether the private MCP server behind the agent is reachable, authenticated,
// or working, and it is independent of the server's enabled state and readiness.
type MCPTunnel struct {
	// ConnectionStatus is connected, inactive, never_connected, or unknown.
	ConnectionStatus TunnelConnectionStatus `json:"connection_status" jsonschema:"connected, inactive, never_connected, or unknown. Agent-to-gateway connection evidence only; unknown means it could not be read"`
}

// TunnelConnectionReader reads the unexpired live agent connections of one
// tunnel from the runtime store.
type TunnelConnectionReader interface {
	Connections(ctx context.Context, tunnelID string) ([]route.Connection, error)
}

// TunnelStatusService reports the agent connection state of the tunnel behind
// a tunneled MCP server, for callers who may read the project's tunneled
// sources.
type TunnelStatusService struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	authz  *authz.Engine

	// connections is nil when the runtime store is not composed; every
	// authorized read then reports unknown.
	connections TunnelConnectionReader
}

// WithTunnelStatus adds the tunnel agent connection state to get_mcp and
// get_mcp_diagnostics reads of tunneled MCP servers. It must follow
// WithAuthorization: without an authorization engine the status is never
// read. A nil connections reader keeps the status readable as unknown.
func (r *PostgresReader) WithTunnelStatus(connections TunnelConnectionReader) *PostgresReader {
	if r != nil && r.db != nil && r.authz != nil {
		r.tunnelStatus = &TunnelStatusService{logger: r.logger, db: r.db, authz: r.authz, connections: connections}
	}
	return r
}

// Status returns the tunnel connection state for one MCP server, or nil when
// it must not be reported: the caller cannot read the project's tunneled
// sources (the same project-level mcp:read the dashboard's tunnel detail
// requires), or the server has no live tunneled source in that project. A
// failure after authorization reports unknown rather than failing the read.
func (s *TunnelStatusService) Status(ctx context.Context, principal Principal, projectID, mcpServerID uuid.UUID) *MCPTunnel {
	if s == nil || s.db == nil || s.authz == nil || principal.OrganizationID == "" {
		return nil
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, projectID.String(), projectID.String())); err != nil {
		if !isAuthorizationDenied(err) {
			s.logger.WarnContext(ctx, "authorize tunnel status", attr.SlogError(err))
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, tunnelStatusReadTimeout)
	defer cancel()

	source, err := platformrepo.New(s.db).GetPlatformMCPTunneledSourceForMCP(ctx, platformrepo.GetPlatformMCPTunneledSourceForMCPParams{
		OrganizationID: principal.OrganizationID,
		McpServerID:    mcpServerID,
		ProjectID:      projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		s.logger.WarnContext(ctx, "read tunneled source for tunnel status", attr.SlogError(err))
		return &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}
	}

	if s.connections == nil {
		return &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}
	}
	connections, err := s.readConnections(ctx, source.ID.String())
	if err != nil {
		s.logger.WarnContext(ctx, "read tunnel connections for tunnel status", attr.SlogError(err), attr.SlogTunneledMCPServerID(source.ID.String()))
		return &MCPTunnel{ConnectionStatus: TunnelConnectionUnknown}
	}

	status := mv.ClassifyTunneledMcpConnection(source.Status, source.EverSeen, len(connections))
	return &MCPTunnel{ConnectionStatus: TunnelConnectionStatus(status)}
}

type tunnelConnectionsResult struct {
	connections []route.Connection
	err         error
}

// readConnections enforces the enrichment deadline whether or not the reader
// honours its context: the production Redis client does not apply context
// deadlines to socket reads. When the deadline passes first the read is
// abandoned and finishes in the background, bounded by the client's own read
// timeout; a reply that arrives after the deadline is never classified.
func (s *TunnelStatusService) readConnections(ctx context.Context, tunnelID string) ([]route.Connection, error) {
	result := make(chan tunnelConnectionsResult, 1)
	go func() {
		connections, err := s.connections.Connections(ctx, tunnelID)
		result <- tunnelConnectionsResult{connections: connections, err: err}
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("read tunnel connections: %w", ctx.Err())
	case read := <-result:
		if read.err != nil {
			return nil, fmt.Errorf("read tunnel connections: %w", read.err)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read tunnel connections after deadline: %w", err)
		}
		return read.connections, nil
	}
}
