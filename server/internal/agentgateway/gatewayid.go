// Package agentgateway names the synthetic gateway an agent's key is served
// through.
package agentgateway

import "github.com/google/uuid"

// gatewayNamespace derives the synthetic meta-server id an agent gateway
// reports. There is no meta_mcp_servers row behind it, so it must never be
// used to read one back; it exists so telemetry and session records group per
// agent instead of collapsing onto a single nil id.
var gatewayNamespace = uuid.MustParse("a6e17a1e-0000-4000-8000-000000000001")

// GatewayID is the meta-server id an agent's gateway stamps on its telemetry
// and session records.
func GatewayID(agentID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(gatewayNamespace, agentID[:])
}
