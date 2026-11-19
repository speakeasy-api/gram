//nolint:exhaustruct_v5 // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// getMyVibeObservabilityName is the Platform MCP tool that reports whether the
// organization's Mistral Vibe observability plugin can be installed and guides
// the install — the outcome the dashboard's plugins dialog serves for people.
const getMyVibeObservabilityName = "get_my_vibe_observability_install"

const getMyVibeObservabilityDescription = "Check whether your organization's Mistral Vibe observability plugin (the Speakeasy hooks package for Mistral Vibe) can be installed, and get its install guidance. " +
	"Returns available=true when a download ZIP can be minted (the pinned speakeasy-hooks release carries the vibe provider and the organization name fits the plugin schema), and published=true with the plugin install name when the published marketplace repository carries the package. " +
	"When available, download_url is the ZIP endpoint to fetch, and next_action says how to install it: extract the ZIP into ~/.vibe/plugins/<plugin> (user scope) or copy the <plugin>-vibe/ directory from the marketplace repository, then restart Vibe or run /reload-plugins; the package's ai.mistral.vibe/hooks.toml runs automatically. " +
	"When not available, next_action says what is missing (the hooks release carrying the vibe provider, or a name-schema-compatible organization name). The ZIP endpoint mints a fresh hooks-scoped key on each download. No API key, token, or package bytes are returned by this tool."

// GetMyVibeObservabilityInput names the project whose marketplace carries the
// package; omit it to use the organization's Default project.
type GetMyVibeObservabilityInput struct {
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"the project's slug, as list_projects returns it; omit it to use the organization's Default project"`
	// ProjectID replaces ProjectSlug before the handler runs: the ID of the project it names.
	ProjectID string `json:"-"`
}

// GetMyVibeObservabilityOutput is the availability and install guidance for
// the Vibe observability plugin.
type GetMyVibeObservabilityOutput struct {
	// Available reports whether the ZIP download can be minted right now.
	Available bool `json:"available"`
	// Published reports whether the published marketplace repo carries the
	// package; Plugin is set only then.
	Published bool   `json:"published"`
	Plugin    string `json:"plugin,omitempty" jsonschema:"install directory name under ~/.vibe/plugins/, set only when published"`
	// DownloadURL is the ZIP endpoint, set only when Available.
	DownloadURL string `json:"download_url,omitempty"`
	// NextAction tells the caller what to do with the answer.
	NextAction string `json:"next_action"`
}

// vibeObservabilityMeta: the download the guidance points at requires org
// admin, so the tool is external-only and admin-authorized, defaultable to
// the Default project like the other publish-status guidance.
var vibeObservabilityMeta = ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeDefaultable, DiscoveryScopes: discoveryOrgRead}

// GetMyVibeObservability answers get_my_vibe_observability_install.
func (s *PluginsService) GetMyVibeObservability(ctx context.Context, principal Principal, input GetMyVibeObservabilityInput) (GetMyVibeObservabilityOutput, error) {
	if !s.valid() || s.authorization == nil {
		return GetMyVibeObservabilityOutput{}, ErrUnavailable
	}
	projectID, err := s.installProjectID(ctx, principal, GetMyInstallInstructionsInput{ProjectSlug: input.ProjectSlug, ProjectID: input.ProjectID})
	if err != nil {
		return GetMyVibeObservabilityOutput{}, err
	}
	orgName, err := pluginsrepo.New(s.db).GetOrganizationName(ctx, principal.OrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Fall back to the slug from the auth context, the same fallback the
		// publish path uses.
		orgName = ""
		if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
			orgName = authCtx.OrganizationSlug
		}
	} else if err != nil {
		return GetMyVibeObservabilityOutput{}, fmt.Errorf("read organization name: %w", err)
	}

	conn, err := pluginsrepo.New(s.db).GetGitHubConnection(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		conn = pluginsrepo.PluginGithubConnection{}
	} else if err != nil {
		return GetMyVibeObservabilityOutput{}, fmt.Errorf("read github connection: %w", err)
	}

	cfg := plugins.GenerateConfig{
		OrgName:      orgName,
		HooksOrgName: naming.PublishedHooksOrgName(conn.PublishedHooksConfig),
	}
	status := plugins.VibeObservabilityStatusFor(cfg, conv.FromPGTextOrEmpty[string](conn.PublishedHooksVersion))

	out := GetMyVibeObservabilityOutput{Available: status.Available, Published: status.Published, Plugin: status.Plugin}
	switch {
	case status.Available && status.Published:
		out.DownloadURL = s.serverURL.JoinPath("rpc", "plugins.downloadObservabilityPlugin").String() + "?platform=vibe"
		out.NextAction = fmt.Sprintf("Download download_url, then extract it into ~/.vibe/plugins/%s (or copy the %s-vibe/ directory from the marketplace repository), and restart Vibe or run /reload-plugins — the package's ai.mistral.vibe/hooks.toml runs automatically.", status.Plugin, status.Plugin)
	case status.Available:
		out.DownloadURL = s.serverURL.JoinPath("rpc", "plugins.downloadObservabilityPlugin").String() + "?platform=vibe"
		out.NextAction = "Download download_url and extract it into ~/.vibe/plugins/<org>-observability (the organization's observability plugin name), then restart Vibe or run /reload-plugins. The marketplace copy lands on the next publish."
	case !plugins.VibeProviderReleased():
		out.NextAction = "The pinned speakeasy-hooks release does not carry the vibe provider yet; this opens when a release with the vibe relay is pinned. Ask an organization administrator to republish once it is."
	default:
		out.NextAction = "The organization name produces a plugin name that does not satisfy the Agent Plugins 1.0 name schema (max 64 characters, starting with a letter or digit); rename the organization to install the Vibe observability package."
	}
	return out, nil
}

func registerVibeObservabilityTool(reg *Registrar, plugins *PluginsService) {
	if plugins == nil {
		addTool(reg, &mcp.Tool{
			Name:        getMyVibeObservabilityName,
			Title:       "Get My Vibe Observability Install",
			Description: "Check whether the Mistral Vibe observability plugin can be installed. This capability is not available right now.",
			Annotations: readOnlyAnnotations(),
		}, vibeObservabilityMeta, unavailableTool("vibe_observability"))
		return
	}
	addTool(reg, &mcp.Tool{
		Name:        getMyVibeObservabilityName,
		Title:       "Get My Vibe Observability Install",
		Description: getMyVibeObservabilityDescription,
		Annotations: readOnlyAnnotations(),
	}, vibeObservabilityMeta, func(ctx context.Context, _ *mcp.CallToolRequest, input GetMyVibeObservabilityInput) (*mcp.CallToolResult, GetMyVibeObservabilityOutput, error) {
		return principalToolCall(ctx, installInstructionToolResult, func(principal Principal) (GetMyVibeObservabilityOutput, error) {
			return plugins.GetMyVibeObservability(ctx, principal, input)
		})
	})
}
