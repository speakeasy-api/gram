package platformmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

type GetMyInstallInstructionsInput struct {
	ProjectID    string                 `json:"project_id,omitempty" jsonschema:"optional project ID that owns the target; defaults to the organization's default project for external callers; a project assistant always uses its own project"`
	Plugin       string                 `json:"plugin,omitempty" jsonschema:"exact assigned plugin ID, slug, or name; mutually exclusive with mcp_id"`
	MCPID        string                 `json:"mcp_id,omitempty" jsonschema:"exact configured MCP ID; mutually exclusive with plugin"`
	ClientFamily OnboardingClientFamily `json:"client_family" jsonschema:"client receiving the installation guidance"`
}

type InstallInstruction struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
	// Snippet is a command or config the user can paste as-is. It never
	// carries a credential: sign-in happens in the browser.
	Snippet string `json:"snippet,omitempty" jsonschema:"command or configuration to paste as-is, shown verbatim in a code block"`
}

type GetMyInstallInstructionsOutput struct {
	TargetKind   string                 `json:"target_kind"`
	TargetName   string                 `json:"target_name"`
	ClientFamily OnboardingClientFamily `json:"client_family"`
	Supported    bool                   `json:"supported"`
	Instructions []InstallInstruction   `json:"instructions"`
	Fallback     string                 `json:"fallback,omitempty"`
}

var ErrInstallTargetNotFound = errors.New("install target not found")

func (s *PluginsService) GetMyInstallInstructions(ctx context.Context, principal Principal, input GetMyInstallInstructionsInput) (GetMyInstallInstructionsOutput, error) {
	if !s.valid() || s.authorization == nil || !validOnboardingClient(input.ClientFamily) {
		return GetMyInstallInstructionsOutput{}, ErrUnavailable
	}
	if (strings.TrimSpace(input.Plugin) == "") == (strings.TrimSpace(input.MCPID) == "") {
		return GetMyInstallInstructionsOutput{}, ErrPluginProjectNotFound
	}
	projectID, err := s.installProjectID(ctx, principal, input.ProjectID)
	if err != nil {
		return GetMyInstallInstructionsOutput{}, err
	}
	if strings.TrimSpace(input.Plugin) != "" {
		plugin, err := s.GetAssignedPlugin(ctx, principal, GetPluginInput{ProjectID: projectID.String(), Plugin: input.Plugin, MembershipLimit: 0, MembershipCursor: ""})
		if err != nil {
			return GetMyInstallInstructionsOutput{}, err
		}
		return s.pluginInstallInstructions(ctx, principal.OrganizationID, input.ClientFamily, plugin.Plugin.Name)
	}

	mcpID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
	if err != nil {
		return GetMyInstallInstructionsOutput{}, ErrInstallTargetNotFound
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetMyInstallInstructionsOutput{}, err
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPInstallTarget(ctx, platformrepo.GetPlatformMCPInstallTargetParams{
		OrganizationID: principal.OrganizationID,
		McpServerID:    mcpID,
		ProjectID:      projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetMyInstallInstructionsOutput{}, ErrInstallTargetNotFound
	}
	if err != nil {
		return GetMyInstallInstructionsOutput{}, fmt.Errorf("get installable platform mcp server: %w", err)
	}
	if err := s.authorization.Require(ctx, authz.MCPCheck(authz.ScopeMCPConnect, mcpID.String(), projectID.String())); err != nil {
		if isAuthorizationDenied(err) {
			return GetMyInstallInstructionsOutput{}, ErrInstallTargetNotFound
		}
		return GetMyInstallInstructionsOutput{}, err
	}
	name := row.Name.String
	if name == "" {
		name = row.Slug.String
	}
	return s.standaloneMCPInstallInstructions(ctx, principal.OrganizationID, input.ClientFamily, name, row.EndpointSlug)
}

// memberLinkBases returns the server and dashboard base URLs of links given to
// a member of organizationID, as organizationBaseURLs chooses them. The stored
// default host is optional: when reading it fails, links fall back to the
// request's platform host or the configured URLs.
func (s *PluginsService) memberLinkBases(ctx context.Context, organizationID string) (serverURL, dashboardURL *url.URL) {
	defaultHost := pgtype.Text{String: "", Valid: false}
	if s.orgHosts != nil && storedHostApplies(ctx) {
		org, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
		if err != nil {
			s.orgHostsLogger.WarnContext(ctx, "read organization default host for member links", attr.SlogError(err))
		} else {
			defaultHost = org.DefaultHost
		}
	}
	return organizationBaseURLs(ctx, s.orgHosts, defaultHost, s.serverURL, s.dashboardURL)
}

// installProjectID parses the named project ID, or resolves the organization's
// default project when none is named.
func (s *PluginsService) installProjectID(ctx context.Context, principal Principal, projectID string) (uuid.UUID, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		parsed, err := uuid.Parse(projectID)
		if err != nil {
			return uuid.Nil, ErrPluginProjectNotFound
		}
		return parsed, nil
	}
	row, err := platformrepo.New(s.db).ResolvePlatformMCPProjectBySlug(ctx, platformrepo.ResolvePlatformMCPProjectBySlugParams{
		OrganizationID: principal.OrganizationID,
		Slug:           defaultProjectSlug,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrDefaultProjectNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve platform mcp install default project: %w", err)
	}
	return row.ID, nil
}

func (s *PluginsService) pluginInstallInstructions(ctx context.Context, organizationID string, client OnboardingClientFamily, name string) (GetMyInstallInstructionsOutput, error) {
	deviceAgentURL := ""
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && authCtx.ActiveOrganizationID == organizationID && authCtx.OrganizationSlug != "" {
		if _, dashboardURL := s.memberLinkBases(ctx, organizationID); dashboardURL != nil {
			deviceAgentURL = dashboardURL.JoinPath(authCtx.OrganizationSlug, "device-agent").String()
		}
	}
	output := GetMyInstallInstructionsOutput{
		TargetKind: "plugin", TargetName: name, ClientFamily: client,
		Supported: true, Instructions: []InstallInstruction{
			{Title: "Use the Speakeasy device agent", Description: "Assigned published plugins are delivered through the Speakeasy device agent. Installation never grants MCP access by itself.", URL: deviceAgentURL, Snippet: ""},
			{Title: "Refresh your AI client", Description: "After the device agent syncs, restart or reload your AI client so it discovers the assigned plugin.", URL: "", Snippet: ""},
		},
		Fallback: "",
	}
	if client == OnboardingClientClaudeCowork || client == OnboardingClientOther {
		output.Supported = false
		output.Instructions = []InstallInstruction{}
		output.Fallback = "This client does not support the assigned-plugin delivery path. Ask an organization administrator for a supported installation option."
	}
	return output, nil
}

func (s *PluginsService) standaloneMCPInstallInstructions(ctx context.Context, organizationID string, client OnboardingClientFamily, name, slug string) (GetMyInstallInstructionsOutput, error) {
	output := GetMyInstallInstructionsOutput{
		TargetKind: "mcp", TargetName: name, ClientFamily: client,
		Supported: false, Instructions: []InstallInstruction{}, Fallback: "",
	}
	if slug == "" || s.serverURL == nil {
		output.Fallback = "This MCP server does not have a public installation endpoint. Use the dashboard or ask an organization administrator for the supported setup path."
		return output, nil
	}
	serverURL, _ := s.memberLinkBases(ctx, organizationID)
	endpoint := serverURL.JoinPath("mcp", slug).String()
	switch client {
	case OnboardingClientClaudeCode, OnboardingClientClaudeCowork, OnboardingClientCodex, OnboardingClientCursor, OnboardingClientOpencode, OnboardingClientOther:
		output.Supported = true
		output.Instructions = append(clientInstallSteps(client, slug, endpoint),
			InstallInstruction{Title: "Complete sign-in", Description: "Finish the browser authorization flow, then return to the client and retry the connection.", URL: "", Snippet: ""})
	default:
		output.Fallback = "This client does not have reviewed installation guidance. Add the server as a remote Streamable HTTP MCP only if the client supports browser OAuth."
	}
	return output, nil
}

// clientInstallSteps returns the shortest working way to add one remote MCP
// endpoint in a client: a one-click link where the client has one, and a
// command or config to paste otherwise. key names the server in the client's
// config and must be a URL-safe slug.
func clientInstallSteps(client OnboardingClientFamily, key, endpoint string) []InstallInstruction {
	addEndpoint := InstallInstruction{Title: "Add a remote Streamable HTTP MCP server", Description: "Add this endpoint in your client's MCP settings. The client will open browser sign-in when it first connects.", URL: endpoint, Snippet: ""}
	switch client {
	case OnboardingClientCursor:
		config := base64.StdEncoding.EncodeToString(mustJSON(map[string]string{"url": endpoint}))
		link := "cursor://anysphere.cursor-deeplink/mcp/install?" + url.Values{"name": {key}, "config": {config}}.Encode()
		return []InstallInstruction{
			{Title: "Install in Cursor", Description: "Open this link to add the server to Cursor in one step.", URL: link, Snippet: ""},
			{Title: "Or add it to ~/.cursor/mcp.json", Description: "Merge this into the mcpServers object of your Cursor MCP config.", URL: endpoint, Snippet: string(mustJSONIndent(map[string]any{"mcpServers": map[string]any{key: map[string]string{"url": endpoint}}}))},
		}
	case OnboardingClientClaudeCode:
		return []InstallInstruction{
			{Title: "Add it with the Claude Code CLI", Description: "Run this in a terminal, then run /mcp in Claude Code to sign in.", URL: endpoint, Snippet: fmt.Sprintf("claude mcp add --transport http %s %s", key, endpoint)},
		}
	case OnboardingClientCodex:
		return []InstallInstruction{
			{Title: "Add it to ~/.codex/config.toml", Description: "Add this block, then run the login command to sign in.", URL: endpoint, Snippet: fmt.Sprintf("[mcp_servers.%s]\nurl = %q", key, endpoint)},
			{Title: "Sign in from Codex", Description: "Run this in a terminal to open browser sign-in.", URL: "", Snippet: "codex mcp login " + key},
		}
	case OnboardingClientOpencode:
		return []InstallInstruction{
			{Title: "Add it to opencode.json", Description: "Merge this into the mcp object of your OpenCode config.", URL: endpoint, Snippet: string(mustJSONIndent(map[string]any{"mcp": map[string]any{key: map[string]any{"type": "remote", "url": endpoint, "enabled": true}}}))},
		}
	case OnboardingClientOther:
		addEndpoint.Snippet = string(mustJSONIndent(map[string]any{"mcpServers": map[string]any{key: map[string]string{"url": endpoint}}}))
		addEndpoint.Description = "Add this endpoint in your client's MCP settings. Many clients accept this JSON shape. The client will open browser sign-in when it first connects."
		return []InstallInstruction{addEndpoint}
	default:
		return []InstallInstruction{addEndpoint}
	}
}

// mustJSONIndent encodes an install config built from strings and maps, which
// cannot fail to encode.
func mustJSONIndent(value any) []byte {
	payload, _ := json.MarshalIndent(value, "", "  ")
	return payload
}
