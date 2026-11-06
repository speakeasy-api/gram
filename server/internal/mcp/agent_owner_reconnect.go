// Who fixes an agent gateway member whose connection broke, and where.
//
// An agent reaches OAuth upstreams with a connection a person delegated to
// it. The agent cannot sign in itself, so when that connection is missing,
// rejected, or can no longer be refreshed, the gateway names the agent's
// owner and the dashboard page where the agent's MCP sign-ins are managed.
// The link carries no credential: the page asks the reader to sign in to
// Speakeasy and shows the step only to someone allowed to edit the agent.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode"

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

// agentReconnect names who reconnects an agent's member connection and the
// page they do it on. The zero value names nobody, and callers keep their
// generic wording.
type agentReconnect struct {
	// owner is the agent owner's display name, empty when unknown.
	owner string
	// url opens the agent's MCP sign-in step in the dashboard.
	url string
}

// ask is the instruction the agent passes on: who opens the page, and what
// they do there.
func (r agentReconnect) ask(action string) string {
	who := "its owner"
	if r.owner != "" {
		who = fmt.Sprintf("its owner, %s,", r.owner)
	}
	return fmt.Sprintf("Ask %s to open %s and %s it, then retry", who, r.url, action)
}

// maxOwnerNameRunes bounds the owner's name in text the agent reads.
const maxOwnerNameRunes = 64

// ownerNameForAgent is the owner's display name as it goes into text an agent
// reads: one line of printable characters, at most maxOwnerNameRunes long. A
// display name is the owner's own input, so it cannot carry line breaks or
// control characters that would make it read as separate instructions.
func ownerNameForAgent(name string) string {
	fields := strings.FieldsFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || !unicode.IsPrint(r)
	})
	clean := []rune(strings.Join(fields, " "))
	if len(clean) > maxOwnerNameRunes {
		clean = clean[:maxOwnerNameRunes]
	}
	return strings.TrimSpace(string(clean))
}

// agentOwnerReconnect builds an agent gateway's reconnect links. It reads the
// agent, its owner and the projects once, on the first failure that needs
// them, so a request whose members all work never reads them.
type agentOwnerReconnect struct {
	agent func() (agentOwnerRecord, error)

	mu       sync.Mutex
	projects map[uuid.UUID]string
	link     func(projectSlug string) string
	project  func(projectID uuid.UUID) (string, error)
	logger   *slog.Logger
	ctx      context.Context //nolint:containedctx // request-scoped, built per agent gateway request
}

type agentOwnerRecord struct {
	owner       string
	projectSlug string
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
		agent: sync.OnceValues(func() (agentOwnerRecord, error) {
			agent, err := agentsrepo.New(s.db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: organizationID, ID: agentID})
			if err != nil {
				return agentOwnerRecord{owner: "", projectSlug: ""}, fmt.Errorf("load agent for reconnect link: %w", err)
			}
			record := agentOwnerRecord{owner: "", projectSlug: ""}
			// An owner who left the organization has no profile here; the
			// message still sends the agent to its owner, unnamed.
			profile, err := agentsrepo.New(s.db).GetAgentOwnerProfile(ctx, agentsrepo.GetAgentOwnerProfileParams{OrganizationID: organizationID, OwnerUserID: agent.OwnerUserID})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return record, fmt.Errorf("load agent owner for reconnect link: %w", err)
			default:
				record.owner = ownerNameForAgent(profile.DisplayName)
			}
			if agent.ProjectID.Valid {
				slug, err := project(agent.ProjectID.UUID)
				if err != nil {
					return record, err
				}
				record.projectSlug = slug
			}
			return record, nil
		}),
		mu:       sync.Mutex{},
		projects: map[uuid.UUID]string{},
		link: func(projectSlug string) string {
			u := base.JoinPath(orgSlug, "projects", projectSlug, "identities", "agents", agentID.String(), "edit")
			u.RawQuery = "step=" + agentEditSignInStep
			return u.String()
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
	record, err := r.agent()
	if err != nil {
		r.logger.WarnContext(r.ctx, "build agent reconnect link", attr.SlogError(err))
		return none
	}
	slug := record.projectSlug
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
	return agentReconnect{owner: record.owner, url: r.link(slug)}
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

// agentMemberReconnectError is agentServerCredentialError for an agent
// gateway member: the connection needs its owner, so the message says who
// and where. Without a link it keeps the generic wording.
func (g metaGateContext) agentMemberReconnectError(member metaMember) error {
	fix := g.agentReconnectFor(member)
	if fix.url == "" {
		return agentMemberCredentialError(member)
	}
	return &metaMemberError{message: fmt.Sprintf("server %q needs re-authentication: this agent's connection to it is unavailable or not permitted, and the agent cannot sign in itself. %s", member.slug, fix.ask("reconnect"))}
}

// withAgentMemberReconnect replaces an agent credential refusal from the
// shared authorization path with the gateway's owner-facing one. Any other
// error passes through.
func (g metaGateContext) withAgentMemberReconnect(member metaMember, err error) error {
	if err == nil || g.agentOwner == nil {
		return err
	}
	if _, ok := errors.AsType[agentCredentialError](err); !ok {
		return err
	}
	return g.agentMemberReconnectError(member)
}
