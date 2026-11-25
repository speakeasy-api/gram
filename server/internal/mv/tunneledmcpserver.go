package mv

import (
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/publiclimits"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/tunnel/route"
	"github.com/speakeasy-api/gram/tunnel/wire"
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

	effectiveRate, effectiveBurst := publiclimits.Effective(server.PublicRequestRatePerSecond, server.PublicRequestBurst)

	return &types.TunneledMcpServer{
		ID:                                  server.ID.String(),
		ProjectID:                           server.ProjectID.String(),
		Name:                                server.Name,
		KeyPrefix:                           server.KeyPrefix,
		Status:                              types.TunneledMcpLifecycleStatus(server.Status),
		ConnectionStatus:                    tunneledMcpConnectionStatus(server, connections),
		AllowPublic:                         server.AllowPublic,
		AgentVersion:                        agentVersion,
		ResourceIdentifier:                  conv.FromPGText[string](server.ResourceIdentifier),
		PublicRequestRatePerSecond:          optionalInt(server.PublicRequestRatePerSecond),
		PublicRequestBurst:                  optionalInt(server.PublicRequestBurst),
		EffectivePublicRequestRatePerSecond: effectiveRate,
		EffectivePublicRequestBurst:         effectiveBurst,
		LastSeenAt:                          lastSeenAt,
		ActiveConnectionCount:               len(connections),
		ActiveConsumerSessionCount:          activeConsumerSessionCount(connections),
		CreatedAt:                           server.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                           server.UpdatedAt.Time.Format(time.RFC3339),
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
		CollectionState:            new("available"),
		ObservedAt:                 new(time.Now().UTC().Format(time.RFC3339)),
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
			TargetDisplay:          conv.PtrEmpty(wire.TargetDisplay(connection.TargetDisplay)),
			Diagnostics:            buildTunnelDiagnostics(connection.Diagnostics, time.Now()),
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

// optionalInt maps a nullable integer column to the view's optional int.
func optionalInt(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int32)
	return &n
}

func buildTunnelDiagnostics(status *route.Diagnostics, now time.Time) *types.TunnelDiagnostics {
	if status == nil {
		return nil
	}
	view := &types.TunnelDiagnostics{HTTPProgress: nil, State: status.State, ReceivedAt: nil, SampleAgeMs: nil, TargetState: nil, ConsecutiveFailures: nil, DNS: nil, TCP: nil, TLS: nil, RequestsTotal: nil, TransportErrorsTotal: nil, LastHTTPStatus: nil, LastHTTPResponseAgeMs: nil, LastTransportError: nil, LastTransportErrorAgeMs: nil}
	if status.Report == nil {
		return view
	}
	r := status.Report
	elapsed := max(now.Sub(status.ReceivedAt).Milliseconds(), 0)
	age := func(value int64) *int64 {
		if value < 0 {
			return new(int64(-1))
		}
		return new(value + elapsed)
	}
	view.ReceivedAt = new(status.ReceivedAt.Format(time.RFC3339))
	view.SampleAgeMs = age(r.SampleAgeMillis)
	if status.State == "available" && (elapsed > wire.DiagnosticsFreshness.Milliseconds() || (r.SampleAgeMillis >= 0 && *view.SampleAgeMs > wire.DiagnosticsFreshness.Milliseconds())) {
		view.State = "stale"
	}
	view.TargetState = new(r.TargetState)
	if r.TargetState == "unreachable" && r.ConsecutiveFailures < 2 {
		view.TargetState = new("unknown")
	}
	view.ConsecutiveFailures = new(int64(r.ConsecutiveFailures))
	step := func(s wire.DiagnosticStep) *types.TunnelDiagnosticStep {
		return &types.TunnelDiagnosticStep{State: s.State, DurationMs: s.DurationMillis, Failure: s.Failure}
	}
	view.DNS, view.TCP, view.TLS = step(r.DNS), step(r.TCP), step(r.TLS)
	if r.HTTPProgress != nil {
		view.HTTPProgress = &types.TunnelHTTPProgress{
			WaitingHeaders: int64(min(r.HTTPProgress.WaitingHeaders, math.MaxInt64)),
			OpenResponses:  int64(min(r.HTTPProgress.OpenResponses, math.MaxInt64)),
		}
	}
	view.RequestsTotal = new(int64(min(r.RequestsTotal, math.MaxInt64)))
	view.TransportErrorsTotal = new(int64(min(r.TransportErrorsTotal, math.MaxInt64)))
	view.LastHTTPStatus = new(r.LastHTTPStatus)
	view.LastHTTPResponseAgeMs = age(r.LastHTTPResponseAgeMillis)
	view.LastTransportError = new(r.LastTransportError)
	view.LastTransportErrorAgeMs = age(r.LastTransportErrorAgeMillis)
	return view
}
