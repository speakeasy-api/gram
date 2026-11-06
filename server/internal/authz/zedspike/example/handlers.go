// Package example shows what handler code looks like against the embedded
// schema. The service and its stores are stand-ins; only the authz calls are
// the point.
package example

import (
	"context"
	"fmt"

	authz "github.com/speakeasy-api/gram/server/internal/authz/zedspike"
)

// Service is a stand-in for a Speakeasy service.
type Service struct {
	authz   *authz.Client
	servers ServerStore
	chats   ChatStore
}

// ServerStore and ChatStore stand in for sqlc queries.
type ServerStore interface {
	Get(ctx context.Context, id string) (Server, error)
	List(ctx context.Context, projectID, where string, args []any, limit, offset int) ([]Server, error)
	Count(ctx context.Context, projectID, where string, args []any) (int, error)
}

type ChatStore interface {
	Get(ctx context.Context, id string) (Chat, error)
}

type Server struct{ ID, ProjectID, Name string }

type Chat struct {
	ID, ProjectID, UserID, ExternalUserID string
	Agent                                 *authz.AgentOwner
}

// 1. Organization admin: one line. The caller is never passed; the client
// reads it from the context admission filled.
func (s *Service) UpdateOrganizationSettings(ctx context.Context, organizationID string) error {
	if err := s.authz.Require(ctx, authz.Organization(organizationID).Administer()); err != nil {
		return err
	}
	return nil
}

// 2. MCP tool call. The project comes from the loaded server row, never from
// the request, so a project-level grant on another project cannot match.
// CallTool passes tool and disposition to the caveats: an allow limited to
// other tools does not match, and a block on this tool does.
func (s *Service) CallTool(ctx context.Context, serverID, tool, disposition string) error {
	server, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	ref := authz.MCPServer(server.ProjectID, server.ID)
	if err := s.authz.Require(ctx, ref.CallTool(tool, disposition)); err != nil {
		return err
	}
	return nil
}

// 3. Paginated list. LookupResources answers with a SQL condition, because
// most grants name "every server in the project" and only the database can
// enumerate that.
func (s *Service) ListServers(ctx context.Context, organizationID, projectID string, limit, offset int) ([]Server, int, error) {
	project := authz.Project(organizationID, projectID)
	predicate, err := s.authz.LookupResources(ctx, project.ReadableMCPServers())
	if err != nil {
		return nil, 0, err
	}
	where, args := predicate.SQL("toolsets.id")
	page, err := s.servers.List(ctx, projectID, where, args, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	// 4. Count: the same predicate, so the total equals what the caller can
	// page through. Counting a filtered page would be wrong both ways.
	total, err := s.servers.Count(ctx, projectID, where, args)
	if err != nil {
		return nil, 0, err
	}
	return page, total, nil
}

// 5. Agent precedence. Nothing here is agent-specific: when an agent calls,
// the caller holds three policies (credential ceiling, the agent, its owner)
// and Require passes only if every one allows. An agent whose own grant
// covers every server in the project still cannot open a server its owner
// cannot.
func (s *Service) ConnectServer(ctx context.Context, serverID string) error {
	server, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	return s.authz.Require(ctx, authz.MCPServer(server.ProjectID, server.ID).Connect())
}

// 6. A direct grant overriding a block. Also nothing in the handler: the
// Members role is blocked from reading this server's configuration, and
// Dana, a member, was granted mcp:read on it by name. Her row lands in
// mcp_read_direct, which rbac.zed adds back after the role's block and
// before her own blocks. Connecting works the same way through
// mcp_connect_direct.
func (s *Service) OpenServerSettings(ctx context.Context, serverID string) error {
	server, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return err
	}
	return s.authz.Require(ctx, authz.MCPServer(server.ProjectID, server.ID).Read())
}

// Chat ownership: the handler loads the chat and passes its owner, which is
// the only way to build a chat check. A chat grant, being its author, or
// owning the live agent that ran an agent-only chat each allow.
func (s *Service) LoadChat(ctx context.Context, chatID string) (Chat, error) {
	chat, err := s.chats.Get(ctx, chatID)
	if err != nil {
		return Chat{}, err
	}
	owner := authz.ChatOwner{UserID: chat.UserID, ExternalUserID: chat.ExternalUserID, Agent: chat.Agent}
	if err := s.authz.Require(ctx, authz.Chat(chat.ProjectID, chat.ID).WithOwner(owner).Read()); err != nil {
		return Chat{}, fmt.Errorf("read chat: %w", err)
	}
	return chat, nil
}
