// Who fixes an agent gateway member whose connection broke, and where.
//
// An agent reaches OAuth upstreams with a connection a person delegated to
// it. The agent cannot sign in itself, so when that connection is missing,
// rejected, or can no longer be refreshed, the gateway sends the agent to
// the people who can edit it, with the dashboard page where the agent's MCP
// sign-ins are managed. It names no one: the owner is not the only person who
// may fix it. The link carries no credential: the page asks the reader to
// sign in to Speakeasy and shows the step only to someone allowed to edit the
// agent.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

// agentEditSignInStep is the agent edit wizard's MCP sign-in step (0 Name,
// 1 MCP Selection, 2 MCP sign-in), where the owner reconnects each server.
const agentEditSignInStep = "2"

// agentEditors is who the agent sends to fix its connection. The owner is
// one of them, but anyone allowed to edit the agent may reconnect a server
// or change its access, so the message names no one.
const agentEditors = "someone who can edit this agent"

// agentReconnect holds the dashboard pages where an agent's connections and
// access are fixed. The zero value has no pages, and callers keep their
// generic wording.
type agentReconnect struct {
	// url opens the agent's MCP sign-in step in the dashboard.
	url string
	// permissionsURL opens the agent's permissions in the dashboard.
	permissionsURL string
}

// ask is the instruction the agent passes on: who opens the sign-in page,
// and what they do there.
func (r agentReconnect) ask(action string) string {
	return fmt.Sprintf("Ask %s to open %s and %s it, then retry", agentEditors, r.url, action)
}

// agentOwnerReconnect builds an agent gateway's reconnect links. It reads the
// agent and the projects once, on the first failure that needs them, so a
// request whose members all work never reads them.
type agentOwnerReconnect struct {
	// agent is the slug of the agent's own project, empty when it has none.
	agent func() (string, error)

	mu       sync.Mutex
	projects map[uuid.UUID]string
	link     func(projectSlug string) agentReconnect
	project  func(projectID uuid.UUID) (string, error)
	logger   *slog.Logger
	ctx      context.Context //nolint:containedctx // request-scoped, built per agent gateway request
}

// newAgentOwnerReconnect returns the reconnect links for one agent gateway
// request, or nil when the request is no agent's.
func (s *Service) newAgentOwnerReconnect(ctx context.Context, logger *slog.Logger, organizationID string, agentID uuid.UUID) *agentOwnerReconnect {
	if agentID == uuid.Nil || organizationID == "" || s.siteURL == nil {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.OrganizationSlug == "" {
		return nil
	}
	base := s.platformBaseURL(ctx)
	orgSlug := authCtx.OrganizationSlug
	project := func(projectID uuid.UUID) (string, error) {
		row, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: projectID, OrganizationID: organizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("load agent reconnect project: %w", err)
		}
		return row.Slug, nil
	}
	return &agentOwnerReconnect{
		agent: sync.OnceValues(func() (string, error) {
			agent, err := agentsrepo.New(s.db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: organizationID, ID: agentID})
			if err != nil {
				return "", fmt.Errorf("load agent for reconnect link: %w", err)
			}
			if !agent.ProjectID.Valid {
				return "", nil
			}
			return project(agent.ProjectID.UUID)
		}),
		mu:       sync.Mutex{},
		projects: map[uuid.UUID]string{},
		link: func(projectSlug string) agentReconnect {
			signIn := base.JoinPath(orgSlug, "projects", projectSlug, "identities", "agents", agentID.String(), "edit")
			signIn.RawQuery = "step=" + agentEditSignInStep
			permissions := base.JoinPath(orgSlug, "projects", projectSlug, "identities", "agent:"+agentID.String(), "permissions")
			return agentReconnect{url: signIn.String(), permissionsURL: permissions.String()}
		},
		project: project,
		logger:  logger,
		ctx:     ctx,
	}
}

// forMember is the reconnect link for one member. The agent's own project
// names the page; an agent not yet scoped to a project uses the member's,
// since agent pages read the same from any project in the organization.
func (r *agentOwnerReconnect) forMember(member metaMember) agentReconnect {
	var none agentReconnect
	if r == nil {
		return none
	}
	slug, err := r.agent()
	if err != nil {
		r.logger.WarnContext(r.ctx, "build agent reconnect link", attr.SlogError(err))
		return none
	}
	if slug == "" {
		slug, err = r.memberProject(member.projectID)
		if err != nil {
			r.logger.WarnContext(r.ctx, "build agent reconnect link", attr.SlogError(err))
			return none
		}
	}
	if slug == "" {
		return none
	}
	return r.link(slug)
}

func (r *agentOwnerReconnect) memberProject(projectID uuid.UUID) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if slug, ok := r.projects[projectID]; ok {
		return slug, nil
	}
	slug, err := r.project(projectID)
	if err != nil {
		return "", err
	}
	r.projects[projectID] = slug
	return slug, nil
}

// agentReconnectFor is the reconnect link for member on an agent gateway,
// or the zero value anywhere else.
func (g metaGateContext) agentReconnectFor(member metaMember) agentReconnect {
	return g.agentOwner.forMember(member)
}

// hasOAuthSignIn reports whether a member signs in through an upstream OAuth
// client, which a person connects at the agent's sign-in step: it has a
// derived or chosen sign-in issuer, a chosen client, or associated clients.
// A member without one is reached with a credential configured on the server,
// or none.
func (m metaMember) hasOAuthSignIn() bool {
	if m.upstreamNoAuth {
		return false
	}
	return m.remoteSessionIssuerID.Valid || m.chosenClientID.Valid || len(m.associatedClients) > 0
}

// Short row details connection_status shows for an agent's member that a
// person reconnects at the agent's sign-in step. The step's link is in the
// status hint, so the row does not repeat it.
const (
	agentDetailNeedsReauth  = "needs re-authentication"
	agentDetailNotConnected = "this agent has no connection to it"
)

// agentMemberError carries a short detail for connection_status rows beside
// the full message a tool call returns.
type agentMemberError struct {
	error
	detail string
}

func (e agentMemberError) Unwrap() error { return e.error }

// agentMemberReconnectError is agentServerCredentialError for an agent
// gateway member whose connection is missing or can no longer be used: a
// person reconnects it, so the message says where. Without a link it keeps
// the generic wording.
func (g metaGateContext) agentMemberReconnectError(member metaMember) error {
	fix := g.agentReconnectFor(member)
	if fix.url == "" {
		return agentMemberCredentialError(member)
	}
	return agentMemberError{&metaMemberError{message: fmt.Sprintf("server %q needs re-authentication: this agent's connection to it is missing or can no longer be used, and the agent cannot sign in itself. %s", member.slug, fix.ask("reconnect"))}, agentDetailNeedsReauth}
}

// agentMemberAccessError is agentServerAccessError for an agent gateway
// member: the agent may not use a connection to it, which reconnecting does
// not change, so the message sends a person to the agent's permissions.
// Without a link it keeps the generic wording.
func (g metaGateContext) agentMemberAccessError(member metaMember) error {
	fix := g.agentReconnectFor(member)
	if fix.permissionsURL == "" {
		return agentServerAccessError(member.slug)
	}
	return &metaMemberError{message: fmt.Sprintf("server %q refused this agent: it is not allowed to use a connection to this server, and reconnecting does not change that. Ask %s to review its permissions at %s, then retry", member.slug, agentEditors, fix.permissionsURL)}
}

// withAgentMemberReconnect replaces an agent refusal from the shared
// authorization path with the gateway's own: a missing connection asks for a
// reconnect, and a refused use asks for a permissions review. Any other
// error passes through.
func (g metaGateContext) withAgentMemberReconnect(member metaMember, err error) error {
	if err == nil || g.agentOwner == nil {
		return err
	}
	if _, ok := errors.AsType[agentAccessError](err); ok {
		return g.agentMemberAccessError(member)
	}
	if _, ok := errors.AsType[agentCredentialError](err); ok {
		return g.agentMemberReconnectError(member)
	}
	return err
}
