package mcp

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
)

// gatewayKind identifies the membership and admission semantics of a gateway.
type gatewayKind uint8

const (
	gatewayKindStored gatewayKind = iota + 1
	gatewayKindProject
	gatewayKindOrganization
	gatewayKindAgent
)

// gatewayTarget is the resource a credential asks to enter. It never decides
// which member servers the caller may use; member authorization remains a
// separate per-server decision.
type gatewayTarget struct {
	organizationID      string
	userSessionIssuerID uuid.UUID
	kind                gatewayKind
	id                  uuid.UUID
}

func gatewayTargetFromEndpoint(endpoint *ResolvedMcpEndpoint) (gatewayTarget, error) {
	if endpoint == nil || endpoint.OrganizationID == "" || endpoint.UserSessionIssuerID == uuid.Nil ||
		!endpoint.MetaMcpServerID.Valid || endpoint.MetaMcpServerID.UUID == uuid.Nil {
		return gatewayTarget{}, fmt.Errorf("gateway target is incomplete")
	}

	kind := gatewayKindStored
	if endpoint.RouteBase == mcproute.OrganizationGateway {
		kind = gatewayKindProject
		if endpoint.ProjectID == uuid.Nil {
			kind = gatewayKindOrganization
		}
	}
	return gatewayTarget{
		organizationID:      endpoint.OrganizationID,
		userSessionIssuerID: endpoint.UserSessionIssuerID,
		kind:                kind,
		id:                  endpoint.MetaMcpServerID.UUID,
	}, nil
}

// admits reports only whether the credential may enter this gateway. It does
// not authorize any member. Phase one keeps agent and workload access disabled
// after this check; later phases use the same result when enabling each kind.
func (t gatewayTarget) admits(caller gatewayCaller) (bool, error) {
	// Phase one preserves the existing people-only gateways. Machine entry
	// rules are enabled separately for stored and project gateways in later
	// rollout phases; member authorization remains a distinct step.
	return caller.kind == gatewayCallerPerson, nil
}
