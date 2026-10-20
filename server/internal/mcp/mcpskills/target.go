package mcpskills

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// TargetKind names the kind of MCP surface a skill is linked to, the
// mcp_skill_links.target_kind value.
type TargetKind string

const (
	// TargetMCPServer is a stored mcp_servers row, named by its id.
	TargetMCPServer TargetKind = "mcp_server"

	// TargetMetaMCPServer is a stored meta_mcp_servers gateway, named by its
	// id.
	TargetMetaMCPServer TargetKind = "meta_mcp_server"

	// TargetOrganizationGateway is the organization gateway. It has no row
	// behind it, so it is named by its kind alone, one per organization.
	TargetOrganizationGateway TargetKind = "organization_gateway"
)

// ErrInvalidTarget reports a target whose id does not fit its kind.
var ErrInvalidTarget = errors.New("invalid skill link target")

// Target is the MCP surface a skill is linked to and served by. Stored
// surfaces carry their row id; row-less surfaces carry uuid.Nil, so a missing
// id never stands in for any particular surface.
type Target struct {
	Kind TargetKind
	ID   uuid.UUID
}

// MCPServerTarget names a stored MCP server.
func MCPServerTarget(id uuid.UUID) Target {
	return Target{Kind: TargetMCPServer, ID: id}
}

// MetaMCPServerTarget names a stored meta MCP gateway.
func MetaMCPServerTarget(id uuid.UUID) Target {
	return Target{Kind: TargetMetaMCPServer, ID: id}
}

// OrganizationGatewayTarget names the organization gateway of the
// organization a resolution or link belongs to.
func OrganizationGatewayTarget() Target {
	return Target{Kind: TargetOrganizationGateway, ID: uuid.Nil}
}

// RowLess reports whether the target's kind names a surface with no stored
// row, which is identified by its kind alone.
func (t Target) RowLess() bool {
	return t.Kind == TargetOrganizationGateway
}

// Validate reports [ErrInvalidTarget] unless the kind is known and the id is
// set exactly when the kind names a stored row.
func (t Target) Validate() error {
	switch t.Kind {
	case TargetMCPServer, TargetMetaMCPServer:
		if t.ID == uuid.Nil {
			return fmt.Errorf("%w: %s needs an id", ErrInvalidTarget, t.Kind)
		}
	case TargetOrganizationGateway:
		if t.ID != uuid.Nil {
			return fmt.Errorf("%w: %s takes no id", ErrInvalidTarget, t.Kind)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidTarget, t.Kind)
	}
	return nil
}
