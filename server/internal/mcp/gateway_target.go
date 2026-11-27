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

func (k gatewayKind) String() string {
	switch k {
	case gatewayKindStored:
		return "stored"
	case gatewayKindProject:
		return "project"
	case gatewayKindOrganization:
		return "organization"
	case gatewayKindAgent:
		return "agent"
	default:
		return "unknown"
	}
}

type gatewayRefusalReason string

const (
	gatewayRefusalMachineAccessDisabled  gatewayRefusalReason = "machine_access_disabled"
	gatewayRefusalUnsupportedGatewayType gatewayRefusalReason = "unsupported_gateway_type"
	gatewayRefusalMissingProject         gatewayRefusalReason = "missing_project"
	gatewayRefusalProjectScopeMismatch   gatewayRefusalReason = "project_scope_mismatch"
	gatewayRefusalCredentialCeiling      gatewayRefusalReason = "gateway_not_in_credential_ceiling"
	gatewayRefusalUnsupportedCaller      gatewayRefusalReason = "unsupported_caller"
)

type gatewayAdmissionDecision struct {
	admitted      bool
	refusalReason gatewayRefusalReason
}

func admitGateway() gatewayAdmissionDecision {
	return gatewayAdmissionDecision{admitted: true, refusalReason: ""}
}

func refuseGateway(reason gatewayRefusalReason) gatewayAdmissionDecision {
	return gatewayAdmissionDecision{admitted: false, refusalReason: reason}
}

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
	if (t.kind != gatewayKindStored && t.kind != gatewayKindProject) || t.id == uuid.Nil || projectID == uuid.Nil {
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
func (t gatewayTarget) admits(caller gatewayCaller, projectID uuid.UUID, machineAccess bool) (gatewayAdmissionDecision, error) {
	if caller.kind == gatewayCallerPerson {
		return admitGateway(), nil
	}
	if t.kind != gatewayKindStored && t.kind != gatewayKindProject {
		return refuseGateway(gatewayRefusalUnsupportedGatewayType), nil
	}
	if !machineAccess {
		return refuseGateway(gatewayRefusalMachineAccessDisabled), nil
	}
	if projectID == uuid.Nil {
		return refuseGateway(gatewayRefusalMissingProject), nil
	}
	if caller.projectScope != nil && *caller.projectScope != projectID {
		return refuseGateway(gatewayRefusalProjectScopeMismatch), nil
	}

	switch caller.kind {
	case gatewayCallerAgent:
		// An agent's grants are checked against each member. Pointing the agent
		// at a gateway grants no authority of its own.
		return admitGateway(), nil
	case gatewayCallerWorkload:
		if caller.principalCredential {
			// A project-bound principal credential derives its ceiling from
			// live agent policy rather than naming a gateway target. Its
			// projectScope confines it to one project, and member authorization
			// checks each member separately against live agent and owner policy.
			if caller.projectScope == nil {
				return refuseGateway(gatewayRefusalProjectScopeMismatch), nil
			}
			return admitGateway(), nil
		}
		// The workload credential ceiling must authorize this gateway target.
		check, ok := t.authorizationTarget(projectID)
		if !ok {
			return refuseGateway(gatewayRefusalCredentialCeiling), nil
		}
		admitted, err := authz.GrantsAuthorize(caller.ceiling, check.connectCheck())
		if err != nil {
			return gatewayAdmissionDecision{admitted: false, refusalReason: ""}, fmt.Errorf("authorize workload gateway target: %w", err)
		}
		if !admitted {
			return refuseGateway(gatewayRefusalCredentialCeiling), nil
		}
		return admitGateway(), nil
	default:
		return refuseGateway(gatewayRefusalUnsupportedCaller), nil
	}
}
