package mv

import (
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/route"
)

type TunneledMcpConnectionCache = route.Connection

// BuildTunneledMcpServerView adds live Redis connection summary fields to the tunnel row.
func BuildTunneledMcpServerView(server repo.TunneledMcpServer, connections []TunneledMcpConnectionCache) *types.TunneledMcpServer {
	agentVersion := conv.FromPGText[string](server.AgentVersion)
	if agentVersion == nil {
		agentVersion = latestConnectionAgentVersion(connections)
	}
	lastSeenAt := conv.PtrEmpty(conv.FromPGTimestamptz(server.LastSeenAt))
	if lastSeenAt == nil {
		lastSeenAt = latestConnectionHeartbeat(connections)
	}

	return &types.TunneledMcpServer{
		ID:                         server.ID.String(),
		ProjectID:                  server.ProjectID.String(),
		Name:                       server.Name,
		KeyPrefix:                  server.KeyPrefix,
		Status:                     types.TunneledMcpLifecycleStatus(server.Status),
		ConnectionStatus:           tunneledMcpConnectionStatus(server, connections),
		AgentVersion:               agentVersion,
		LastSeenAt:                 lastSeenAt,
		ActiveConnectionCount:      len(connections),
		ActiveConsumerSessionCount: activeConsumerSessionCount(connections),
		CreatedAt:                  server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                  server.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func BuildTunneledMcpServerListView(servers []repo.TunneledMcpServer, connectionsByServerID map[string][]TunneledMcpConnectionCache) []*types.TunneledMcpServer {
	result := make([]*types.TunneledMcpServer, len(servers))
	for i, server := range servers {
		result[i] = BuildTunneledMcpServerView(server, connectionsByServerID[server.ID.String()])
	}
	return result
}

func BuildTunneledMcpServerConnectionsView(connections []TunneledMcpConnectionCache) *types.TunneledMcpServerConnections {
	return &types.TunneledMcpServerConnections{
		Connections:                buildTunneledMcpConnectionViews(connections),
		ActiveConnectionCount:      len(connections),
		ActiveConsumerSessionCount: activeConsumerSessionCount(connections),
	}
}

func tunneledMcpConnectionStatus(server repo.TunneledMcpServer, connections []TunneledMcpConnectionCache) types.TunneledMcpConnectionStatus {
	if len(connections) > 0 {
		return types.TunneledMcpConnectionStatus("connected")
	}
	if server.Status == "created" && !server.LastSeenAt.Valid {
		return types.TunneledMcpConnectionStatus("never_connected")
	}
	return types.TunneledMcpConnectionStatus("inactive")
}

func buildTunneledMcpConnectionViews(connections []TunneledMcpConnectionCache) []*types.TunneledMcpConnection {
	result := make([]*types.TunneledMcpConnection, 0, len(connections))
	for _, connection := range connections {
		result = append(result, &types.TunneledMcpConnection{
			GatewaySessionID:       connection.GatewaySessionID,
			ServiceVersion:         connection.ServiceVersion,
			AgentVersion:           conv.PtrEmpty(connection.AgentVersion),
			ConnectedAt:            connection.ConnectedAt.Format(time.RFC3339),
			LastHeartbeatAt:        connection.LastHeartbeatAt.Format(time.RFC3339),
			RemoteAddr:             conv.PtrEmpty(connection.RemoteAddr),
			ActiveSubstreams:       connection.ActiveSubstreams,
			ActiveConsumerSessions: connection.ActiveConsumerSessions,
			Metadata:               connectionMetadata(connection.Metadata),
		})
	}
	return result
}

func activeConsumerSessionCount(connections []TunneledMcpConnectionCache) int {
	total := 0
	for _, connection := range connections {
		total += connection.ActiveConsumerSessions
	}
	return total
}

func latestConnectionAgentVersion(connections []TunneledMcpConnectionCache) *string {
	var latest *TunneledMcpConnectionCache
	for i := range connections {
		connection := &connections[i]
		if connection.AgentVersion == "" {
			continue
		}
		if latest == nil || connection.LastHeartbeatAt.After(latest.LastHeartbeatAt) {
			latest = connection
		}
	}
	if latest == nil {
		return nil
	}
	return conv.PtrEmpty(latest.AgentVersion)
}

func latestConnectionHeartbeat(connections []TunneledMcpConnectionCache) *string {
	var latest time.Time
	for i := range connections {
		heartbeat := connections[i].LastHeartbeatAt
		if heartbeat.IsZero() {
			continue
		}
		if latest.IsZero() || heartbeat.After(latest) {
			latest = heartbeat
		}
	}
	if latest.IsZero() {
		return nil
	}
	value := latest.Format(time.RFC3339)
	return &value
}

func connectionMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}

	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if key == "" || value == "" {
			continue
		}
		result[key] = value
	}
	return result
}
