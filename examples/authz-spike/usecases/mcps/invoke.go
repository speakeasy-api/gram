// Package mcps illustrates private MCP admission and per-tool authorization after
// transport authentication. Public endpoints, upstream credentials, protocol/session
// binding, and risk approval remain separate production requirements.
package mcps

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/examples/authz-spike/authz"
)

// Server adapts a trusted MCP resource row, not caller-supplied ancestry.
type Server struct {
	// OrganizationID names the server's actual organization.
	OrganizationID string

	// ProjectID names the server's actual project.
	ProjectID string

	// ID identifies the protected MCP resource, not a tool or upstream address.
	ID string
}

// AuthzTarget describes the server and its canonical enclosing boundaries.
func (s Server) AuthzTarget() authz.Target {
	return authz.Target{
		Resource: authz.ResourceRef{OrganizationID: s.OrganizationID, Kind: authz.MCPServer, ID: s.ID},
		Ancestors: []authz.ResourceRef{
			{OrganizationID: s.OrganizationID, Kind: authz.Project, ID: s.ProjectID},
			{OrganizationID: s.OrganizationID, Kind: authz.Organization, ID: s.OrganizationID},
		},
	}
}

// Repository loads resource ownership and the selected tool's trusted metadata.
type Repository interface {
	// GetServer looks up the actual server in the authenticated organization,
	// returning a safe not-found error for missing or out-of-scope resources.
	GetServer(ctx context.Context, organizationID, serverID string) (Server, error)

	// GetTool returns facts for exactly the named tool on this server. Classification
	// comes from stored metadata/provider annotations, not the invocation payload.
	// The selected tool and target must remain valid through upstream routing.
	GetTool(ctx context.Context, server Server, toolName string) (authz.ToolFacts, error)
}

// Runtime stands in for protected upstream connection and tool execution.
type Runtime interface {
	// Open establishes this server's connection after policy admission.
	Open(context.Context, Server) error

	// Call executes the authorized tool, preserving the server's tenant identity.
	Call(ctx context.Context, server Server, toolName string) (string, error)
}

// Service receives the shared client, ordinary resource lookup, and upstream runtime.
type Service struct {
	authz   *authz.Client
	servers Repository
	runtime Runtime
}

// NewService shares application authorization wiring across both handlers.
func NewService(authorization *authz.Client, repository Repository, runtime Runtime) *Service {
	return &Service{authz: authorization, servers: repository, runtime: runtime}
}

// Connect admits a private connection only if some common policy region remains.
// The subject must already be authenticated; admission is not a reusable tool allow.
func (s *Service) Connect(ctx context.Context, subject authz.Subject, serverID string) error {
	server, err := s.servers.GetServer(ctx, subject.Principal.OrganizationID, serverID)
	if err != nil {
		return fmt.Errorf("resolve MCP server: %w", err)
	}
	decision, err := s.authz.DiscoverAccess(ctx, subject, authz.MCPConnect, server.AuthzTarget())
	if err != nil {
		return err
	}
	if decision.Outcome != authz.Allowed {
		return fmt.Errorf("%w: MCP connection admission %s", authz.ErrDenied, decision.Outcome)
	}
	return s.runtime.Open(ctx, server)
}

// Invoke mirrors the per-tool gate in server/internal/mcp/rpc_tools_call.go.
// No caller-provided disposition or earlier connection decision authorizes this call.
func (s *Service) Invoke(ctx context.Context, subject authz.Subject, serverID, toolName string) (string, error) {
	server, err := s.servers.GetServer(ctx, subject.Principal.OrganizationID, serverID)
	if err != nil {
		return "", fmt.Errorf("resolve MCP server: %w", err)
	}
	facts, err := s.servers.GetTool(ctx, server, toolName)
	if err != nil {
		return "", fmt.Errorf("resolve MCP tool facts: %w", err)
	}
	if err := s.authz.Require(ctx, subject, authz.Request{
		Permission: authz.MCPConnect, Target: server.AuthzTarget(), Tool: &facts, Creation: false,
	}); err != nil {
		return "", err
	}
	return s.runtime.Call(ctx, server, facts.Name)
}
