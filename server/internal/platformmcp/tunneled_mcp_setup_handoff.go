package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

// tunneledMCPAgentSetupFragment is the dashboard anchor of an MCP server's
// tunnel agent setup panel on its settings page.
const tunneledMCPAgentSetupFragment = "agent-setup"

// TunneledMCPSetupIntent names what the returned dashboard page is for.
type TunneledMCPSetupIntent string

const (
	// TunneledMCPSetupIntentAdd opens the form that adds a new MCP server
	// reachable through a tunnel.
	TunneledMCPSetupIntentAdd TunneledMCPSetupIntent = "add_tunneled_mcp"

	// TunneledMCPSetupIntentAgent opens an existing tunneled MCP server's
	// tunnel agent setup panel.
	TunneledMCPSetupIntentAgent TunneledMCPSetupIntent = "set_up_tunnel_agent"
)

var (
	ErrTunneledMCPSetupInvalid     = errors.New("platform mcp tunneled setup request invalid")
	ErrTunneledMCPSetupForbidden   = errors.New("platform mcp tunneled setup requires organization administration")
	ErrTunneledMCPSetupNotFound    = errors.New("platform mcp tunneled setup target not found")
	ErrTunneledMCPSetupNotTunneled = errors.New("platform mcp tunneled setup target is not tunneled")
)

// Instructions returned with each handoff. They are fixed text: nothing from
// the caller, the tunnel, or its configuration is ever interpolated.
var (
	tunneledMCPAddInstructions = []string{
		"Open setup_url while signed in to the dashboard as an organization administrator. It opens the form that adds an MCP server reachable through a tunnel.",
		"Fill in the form there, then follow the agent setup panel for your server's transport to run the tunnel agent inside the network that can reach the private MCP server.",
		"A new tunnel's key is shown once, in the dashboard only. Never paste a tunnel key, header value, or other credential into this chat.",
		"Afterwards, find the new MCP server with find_mcp and read it with get_mcp. Its tunnel.connection_status reports whether the tunnel agent is connected to the gateway, not whether the private server behind it works.",
	}
	tunneledMCPAgentInstructions = []string{
		"Open setup_url while signed in to the dashboard as an organization administrator. It opens this MCP server's tunnel agent setup panel.",
		"Follow the panel for your server's transport to run the tunnel agent inside the network that can reach the private MCP server.",
		"The tunnel key was shown once, when the tunnel was created or its key last rotated. Use the key you saved. Rotate it in the dashboard only if it is lost: rotation disconnects every agent using this tunnel and affects every MCP server on it. Never paste a tunnel key, header value, or other credential into this chat.",
		"Afterwards, read this MCP server with get_mcp. Its tunnel.connection_status reports whether the tunnel agent is connected to the gateway, not whether the private server behind it works.",
	}
)

type GetTunneledMCPSetupHandoffInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID to set up the tunneled MCP server in"`
	MCPID     string `json:"mcp_id,omitempty" jsonschema:"optional: an existing tunneled MCP server's ID, to open its tunnel agent setup. Omit it to add a new MCP server reachable through a tunnel."`
}

type GetTunneledMCPSetupHandoffOutput struct {
	ProjectID   string `json:"project_id"`
	ProjectSlug string `json:"project_slug"`

	// MCPID is the existing tunneled MCP server the handoff opens, and is
	// empty when it opens the form that adds a new one.
	MCPID string `json:"mcp_id,omitempty"`

	Intent TunneledMCPSetupIntent `json:"intent"`

	// SetupURL is the dashboard page that finishes the setup. Tunnel keys and
	// header values are entered or revealed only there.
	SetupURL string `json:"setup_url"`

	Instructions []string `json:"instructions"`
}

// TunneledMCPSetupHandoffService issues dashboard links for setting up MCP
// servers reachable through a tunnel. It never creates, rotates, or reveals a
// tunnel or its key: the dashboard does that under its own session.
type TunneledMCPSetupHandoffService struct {
	db       *pgxpool.Pool
	authz    *authz.Engine
	projects interface {
		ResolveProjectRead(ctx context.Context, principal Principal, input FindMCPInput) (ResolvedProject, error)
	}
	dashboardURL *url.URL
	budget       OperationBudget
}

// WithTunneledMCPSetupHandoff enables get_tunneled_mcp_setup_handoff. It must
// follow WithAuthorization. Without an HTTPS dashboard origin or a handoff
// budget the tool stays in the catalogue as a stable refusal.
func (r *PostgresReader) WithTunneledMCPSetupHandoff(dashboardURL *url.URL, budget OperationBudget) *PostgresReader {
	if r != nil && r.db != nil && r.authz != nil && validDashboardURL(dashboardURL) && budget.valid() {
		origin := *dashboardURL
		origin.RawQuery = ""
		origin.Fragment = ""
		origin.RawFragment = ""
		r.tunneledSetup = &TunneledMCPSetupHandoffService{db: r.db, authz: r.authz, projects: r, dashboardURL: &origin, budget: budget}
	}
	return r
}

// Handoff authorizes the caller and the exact target before spending the
// handoff budget, then returns the dashboard page for the requested setup.
// Hidden, missing, deleted, and foreign targets are indistinguishable.
func (s *TunneledMCPSetupHandoffService) Handoff(ctx context.Context, principal Principal, input GetTunneledMCPSetupHandoffInput) (GetTunneledMCPSetupHandoffOutput, error) {
	if s == nil || s.db == nil || s.authz == nil || s.projects == nil || s.dashboardURL == nil || principal.OrganizationID == "" {
		return GetTunneledMCPSetupHandoffOutput{}, ErrUnavailable
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupInvalid
	}
	// Only an omitted mcp_id selects the add form. A supplied one names an
	// existing server, so even the nil UUID is a target, never a fallback.
	var mcpID uuid.UUID
	if input.MCPID != "" {
		mcpID, err = uuid.Parse(input.MCPID)
		if err != nil || mcpID == uuid.Nil {
			return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupInvalid
		}
	}

	if err := s.require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		if errors.Is(err, ErrForbidden) {
			return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupForbidden
		}
		return GetTunneledMCPSetupHandoffOutput{}, err
	}
	project, err := s.projects.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: projectID.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if errors.Is(err, ErrForbidden) {
		return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupNotFound
	}
	if err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, fmt.Errorf("resolve tunneled setup project: %w", err)
	}
	if mcpID != uuid.Nil {
		// The exact server, and the project's tunneled sources, whose setup
		// state the agent setup panel shows.
		if err := s.require(ctx,
			authz.MCPCheck(authz.ScopeMCPRead, mcpID.String(), project.ID.String()),
			authz.MCPCheck(authz.ScopeMCPRead, project.ID.String(), project.ID.String()),
		); err != nil {
			if errors.Is(err, ErrForbidden) {
				return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupNotFound
			}
			return GetTunneledMCPSetupHandoffOutput{}, err
		}
	}

	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, err
	}

	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, fmt.Errorf("resolve tunneled setup organization: %w", err)
	}
	if organization.Slug == "" {
		return GetTunneledMCPSetupHandoffOutput{}, ErrUnavailable
	}

	output := GetTunneledMCPSetupHandoffOutput{
		ProjectID:    project.ID.String(),
		ProjectSlug:  project.Slug,
		MCPID:        "",
		Intent:       TunneledMCPSetupIntentAdd,
		SetupURL:     s.dashboardURL.JoinPath(organization.Slug, "projects", project.Slug, "mcp", "add", "tunneled").String(),
		Instructions: slices.Clone(tunneledMCPAddInstructions),
	}
	if mcpID == uuid.Nil {
		return output, nil
	}

	target, err := platformrepo.New(s.db).GetPlatformMCPTunneledSetupTarget(ctx, platformrepo.GetPlatformMCPTunneledSetupTargetParams{
		OrganizationID: principal.OrganizationID,
		McpServerID:    mcpID,
		ProjectID:      project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupNotFound
	}
	if err != nil {
		return GetTunneledMCPSetupHandoffOutput{}, fmt.Errorf("get tunneled setup target: %w", err)
	}
	if !target.Tunneled {
		return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupNotTunneled
	}
	if !target.SourceLive {
		return GetTunneledMCPSetupHandoffOutput{}, ErrTunneledMCPSetupNotFound
	}

	route := target.ID.String()
	if target.Slug.Valid && target.Slug.String != "" {
		route = target.Slug.String
	}
	setupURL := s.dashboardURL.JoinPath(organization.Slug, "projects", project.Slug, "mcp", "x", route, "settings")
	setupURL.Fragment = tunneledMCPAgentSetupFragment
	output.MCPID = target.ID.String()
	output.Intent = TunneledMCPSetupIntentAgent
	output.SetupURL = setupURL.String()
	output.Instructions = slices.Clone(tunneledMCPAgentInstructions)
	return output, nil
}

// require maps an authorization denial to ErrForbidden and leaves every other
// failure as is.
func (s *TunneledMCPSetupHandoffService) require(ctx context.Context, checks ...authz.Check) error {
	if err := s.authz.Require(ctx, checks...); err != nil {
		if isAuthorizationDenied(err) {
			return ErrForbidden
		}
		return fmt.Errorf("authorize tunneled setup handoff: %w", err)
	}
	return nil
}
