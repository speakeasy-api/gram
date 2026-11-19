package mcp

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
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

func (t gatewayTarget) authorizationTarget(projectID uuid.UUID) (*AgentAuthorizationTarget, bool) {
	if t.kind != gatewayKindStored || t.id == uuid.Nil || projectID == uuid.Nil {
		return nil, false
	}
	return &AgentAuthorizationTarget{
		Scope:               authz.ScopeMCPConnect,
		OrganizationID:      t.organizationID,
		ProjectID:           projectID,
		UserSessionIssuerID: t.userSessionIssuerID,
		MCPResourceID:       t.id,
	}, true
}

// admits reports only whether the credential may enter this gateway. It does
// not authorize any member.
func (t gatewayTarget) admits(caller gatewayCaller, projectID uuid.UUID, storedMachineAccess bool) (bool, error) {
	if caller.kind == gatewayCallerPerson {
		return true, nil
	}
	if !storedMachineAccess || t.kind != gatewayKindStored || projectID == uuid.Nil {
		return false, nil
	}
	if caller.projectScope != nil && *caller.projectScope != projectID {
		return false, nil
	}

	switch caller.kind {
	case gatewayCallerAgent:
		// An agent's grants are checked against each member. Pointing the agent
		// at a gateway grants no authority of its own.
		return true, nil
	case gatewayCallerWorkload:
		// The workload credential ceiling must authorize this gateway target.
		check, ok := t.authorizationTarget(projectID)
		if !ok {
			return false, nil
		}
		admitted, err := authz.GrantsAuthorize(caller.ceiling, check.connectCheck())
		if err != nil {
			return false, fmt.Errorf("authorize workload gateway target: %w", err)
		}
		return admitted, nil
	default:
		return false, nil
	}
}
