package orggateway

import "github.com/google/uuid"

// gatewayNamespace derives the synthetic meta-server id an organization
// gateway reports. There is no meta_mcp_servers row behind it, so it must
// never be used to read one back; it exists so telemetry, session records,
// and cached challenges group per organization.
var gatewayNamespace = uuid.MustParse("a6e17a1e-0000-4000-8000-000000000002")

// GatewayID is the meta-server id an organization's gateway stamps on its
// telemetry and session records.
func GatewayID(organizationID string) uuid.UUID {
	return uuid.NewSHA1(gatewayNamespace, []byte(organizationID))
}

// ProjectGatewayID is the meta-server id of the organization gateway
// restricted to one project.
func ProjectGatewayID(organizationID string, projectID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(gatewayNamespace, []byte(organizationID+"/projects/"+projectID.String()))
}
