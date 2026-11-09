// Who fixes a gateway member whose connection is missing or broken, and where.
//
// A person fixes their own connection on the member's connect page. An agent
// cannot sign in itself: someone who can edit it reconnects the member at the
// agent's MCP sign-in step in the dashboard, or reviews its permissions there.
// The message names no one, since the owner is not the only person who may
// fix it, and the links carry no credential: the page asks the reader to sign
// in to Speakeasy and shows the step only to someone allowed to edit the
// agent. Anywhere else, an administrator fixes it.

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
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// agentEditSignInStep is the agent edit wizard's MCP sign-in step (0 Name,
// 1 MCP Selection, 2 MCP sign-in), where each server is reconnected.
const agentEditSignInStep = "2"

// agentEditors is who an agent sends to fix its connection. The owner is one
// of them, but anyone allowed to edit the agent may reconnect a server or
// change its access, so the message names no one.
const agentEditors = metamcp.AgentEditors

// memberFix is who fixes a member's missing or broken connection, and where.
// The zero value has no page: an administrator fixes it.
type memberFix struct {
	// agent marks an agent gateway. The agent cannot sign in itself, so
	// someone who can edit it opens url.
	agent bool

	// url opens the page where the connection is made: the member's connect
	// page on the gateway for a person, or the agent's MCP sign-in step.
	// Empty when there is none, or it could not be built.
	url string

	// accessURL opens the agent's permissions, where a refused use is fixed.
	// Empty off an agent gateway.
	accessURL string
}

// memberFix resolves who fixes member on this gateway, and where. On an
// agent gateway it reads the agent's project once per request.
func (g metaGateContext) memberFix(member metaMember) memberFix {
	if g.agentFix != nil {
		return g.agentFix.forMember(member)
	}
	// The Platform gateway has no authentication here and serves only people.
	// Agents and workloads elsewhere have no page to reconnect on.
	person := g.authentication == nil && g.userID != "" && g.agentID == uuid.Nil
	if !person && !codeConnectAvailable(&g) {
		return memberFix{agent: false, url: "", accessURL: ""}
	}
	return memberFix{agent: false, url: g.memberConnectURL(member), accessURL: ""}
}

// callerURL is the page the caller opens to fix the member itself. An agent
// cannot, so on an agent gateway it is empty.
func (f memberFix) callerURL() string {
	if f.agent {
		return ""
	}
	return f.url
}

// askEditorToReconnect is the instruction an agent passes on: who opens the
// sign-in step to reconnect the server.
func (f memberFix) askEditorToReconnect() string {
	return fmt.Sprintf("Ask %s to open %s and reconnect it, then retry", agentEditors, f.url)
}

// memberCallerURL is the page the caller opens to fix member itself. An agent
// cannot, so on an agent gateway it is empty and its links are never built.
func (g metaGateContext) memberCallerURL(member metaMember) string {
	if g.agentFix != nil {
		return ""
	}
	return g.memberFix(member).callerURL()
}

// serverCredentialMissing is a member whose credential is configured on the
// server, such as an API key or a service account, and shared by every
// caller, with none the caller can use. Signing in does not set it, so an
// admin of the server's project adds or fixes it.
func serverCredentialMissing(slug string) *metaMemberError {
	return &metaMemberError{message: fmt.Sprintf("server %q needs a credential configured for it in Speakeasy, such as an API key or a service account, and has none this agent can use. This agent cannot add one. Ask an admin of the server's project to add or fix the server's credential, then retry", slug)}
}

// sharedCredentialError is the failure of a member whose resolved
// credentials include a service account's, which belongs to the server
// rather than to the agent: why that credential could not be obtained, or
// that none serves the member. It is nil when every credential is one of the
// agent's own connections.
func sharedCredentialError(member metaMember, tokens map[uuid.UUID]remotesessions.UpstreamToken) error {
	shared := false
	for _, token := range tokens {
		if token.CredentialOwner != remotesessions.CredentialOwnerSelf {
			continue
		}
		if token.ClientCredentialErr != nil {
			return metaMemberClientCredentialError(member, token.ClientCredentialErr)
		}
		shared = true
	}
	if !shared {
		return nil
	}
	return serverCredentialMissing(member.slug)
}

// agentOAuthSignIn reports whether an agent reached member through an
// upstream OAuth connection of its own, which its sign-in step connects. An
// agent reaches a member only with the credentials its own connections
// resolved, so a member it holds no such connection for, or only a service
// account's token, offers nothing the sign-in step connects: its credential,
// if any, is configured on the server.
func agentOAuthSignIn(member metaMember, tokens map[uuid.UUID]remotesessions.UpstreamToken) bool {
	if member.upstreamNoAuth {
		return false
	}
	for _, token := range tokens {
		if token.CredentialOwner != remotesessions.CredentialOwnerSelf {
			return true
		}
	}
	return false
}

// agentUnroutedError is an agent gateway member none of whose resolved
// credentials serve it. skipped, a client left out for a missing connection
// while another stood in for it, is the cause; a service account's
// credential is the server's to fix; otherwise the agent's own connection
// needs reconnecting.
func (g metaGateContext) agentUnroutedError(member metaMember, tokens map[uuid.UUID]remotesessions.UpstreamToken, skipped error) error {
	if skipped != nil {
		return skipped
	}
	if err := sharedCredentialError(member, tokens); err != nil {
		return err
	}
	return g.agentReconnectError(member)
}

// agentReconnectError is the missing or unusable connection an agent gateway
// member was refused for: a person reconnects it, so the message says who and
// where. Without a link it keeps the generic wording.
func (g metaGateContext) agentReconnectError(member metaMember) error {
	fix := g.memberFix(member)
	if !fix.agent || fix.url == "" {
		return agentServerCredentialError(member.slug)
	}
	return agentCredentialError{&metaMemberError{message: fmt.Sprintf("server %q needs re-authentication: this agent's connection to it is missing or can no longer be used, and the agent cannot sign in itself. %s", member.slug, fix.askEditorToReconnect())}}
}

// withAgentMemberFix replaces an agent refusal from the shared authorization
// path with the gateway's own: a missing connection asks for a reconnect; a
// refused use, which reconnecting does not change, asks for a permissions
// review; and a server that offers the agent no auth client asks an admin of
// the server's project. Any other error, and any gateway that is not an
// agent's, passes through.
func (g metaGateContext) withAgentMemberFix(member metaMember, err error) error {
	if err == nil || g.agentFix == nil {
		return err
	}
	if _, ok := errors.AsType[agentCredentialError](err); ok {
		return g.agentReconnectError(member)
	}
	access, ok := errors.AsType[agentAccessError](err)
	if !ok {
		return err
	}
	const refused = "server %q refused this agent: it is not allowed to use a connection to this server, and reconnecting does not change that. "
	if access.server {
		return &metaMemberError{message: fmt.Sprintf(refused+"No auth client of the server serves this agent. Ask an admin of the server's project to check the server's auth clients, then retry", member.slug)}
	}
	fix := g.memberFix(member)
	if fix.accessURL == "" {
		return &metaMemberError{message: fmt.Sprintf(refused+"Ask %s to review its permissions in Speakeasy, then retry", member.slug, agentEditors)}
	}
	return &metaMemberError{message: fmt.Sprintf(refused+"Ask %s to review its permissions at %s, then retry", member.slug, agentEditors, fix.accessURL)}
}

// agentFixLinks builds an agent gateway's fix links. It reads the agent
// and the projects once, on the first failure that needs them, so a request
// whose members all work never reads them.
type agentFixLinks struct {
	// agent is the slug of the agent's own project, empty when it has none.
	agent func() (string, error)

	mu       sync.Mutex
	projects map[uuid.UUID]string
	link     func(projectSlug string) memberFix
	project  func(projectID uuid.UUID) (string, error)
	logger   *slog.Logger
	ctx      context.Context //nolint:containedctx // request-scoped, built per agent gateway request
}

// newAgentFixLinks returns the fix links for one agent gateway request,
// or nil when the request is no agent's.
func (s *Service) newAgentFixLinks(ctx context.Context, logger *slog.Logger, organizationID string, agentID uuid.UUID) *agentFixLinks {
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
	return &agentFixLinks{
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
		link: func(projectSlug string) memberFix {
			signIn := base.JoinPath(orgSlug, "projects", projectSlug, "identities", "agents", agentID.String(), "edit")
			signIn.RawQuery = "step=" + agentEditSignInStep
			permissions := base.JoinPath(orgSlug, "projects", projectSlug, "identities", "agent:"+agentID.String(), "permissions")
			return memberFix{agent: true, url: signIn.String(), accessURL: permissions.String()}
		},
		project: project,
		logger:  logger,
		ctx:     ctx,
	}
}

// forMember is the agent's fix for one member. The agent's own project names
// the page; an agent not yet scoped to a project uses the member's, since
// agent pages read the same from any project in the organization. Without a
// project it has no links.
func (r *agentFixLinks) forMember(member metaMember) memberFix {
	none := memberFix{agent: true, url: "", accessURL: ""}
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

func (r *agentFixLinks) memberProject(projectID uuid.UUID) (string, error) {
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
