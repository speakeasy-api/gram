package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

type GetMyInstallInstructionsInput struct {
	ProjectID    string                 `json:"project_id" jsonschema:"explicit project ID that owns the target"`
	Plugin       string                 `json:"plugin,omitempty" jsonschema:"exact assigned plugin ID, slug, or name; mutually exclusive with mcp_id"`
	MCPID        string                 `json:"mcp_id,omitempty" jsonschema:"exact configured MCP ID; mutually exclusive with plugin"`
	ClientFamily OnboardingClientFamily `json:"client_family" jsonschema:"client receiving the installation guidance"`
}

type InstallInstruction struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url,omitempty"`
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
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil || (strings.TrimSpace(input.Plugin) == "") == (strings.TrimSpace(input.MCPID) == "") {
		return GetMyInstallInstructionsOutput{}, ErrPluginProjectNotFound
	}
	if strings.TrimSpace(input.Plugin) != "" {
		plugin, err := s.GetAssignedPlugin(ctx, principal, GetPluginInput{ProjectID: projectID.String(), Plugin: input.Plugin})
		if err != nil {
			return GetMyInstallInstructionsOutput{}, err
		}
		return s.pluginInstallInstructions(ctx, input.ClientFamily, plugin.Plugin.Name), nil
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
	return s.standaloneMCPInstallInstructions(input.ClientFamily, name, row.EndpointSlug), nil
}

func (s *PluginsService) pluginInstallInstructions(ctx context.Context, client OnboardingClientFamily, name string) GetMyInstallInstructionsOutput {
	deviceAgentURL := ""
	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && s.dashboardURL != nil && authCtx.OrganizationSlug != "" {
		deviceAgentURL = s.dashboardURL.JoinPath(authCtx.OrganizationSlug, "device-agent").String()
	}
	output := GetMyInstallInstructionsOutput{
		TargetKind: "plugin", TargetName: name, ClientFamily: client,
		Supported: true, Instructions: []InstallInstruction{
			{Title: "Use the Speakeasy device agent", Description: "Assigned published plugins are delivered through the Speakeasy device agent. Installation never grants MCP access by itself.", URL: deviceAgentURL},
			{Title: "Refresh your AI client", Description: "After the device agent syncs, restart or reload your AI client so it discovers the assigned plugin.", URL: ""},
		},
		Fallback: "",
	}
	if client == OnboardingClientClaudeCowork || client == OnboardingClientOther {
		output.Supported = false
		output.Instructions = []InstallInstruction{}
		output.Fallback = "This client does not support the assigned-plugin delivery path. Ask an organization administrator for a supported installation option."
	}
	return output
}

func (s *PluginsService) standaloneMCPInstallInstructions(client OnboardingClientFamily, name, slug string) GetMyInstallInstructionsOutput {
	output := GetMyInstallInstructionsOutput{
		TargetKind: "mcp", TargetName: name, ClientFamily: client,
		Supported: false, Instructions: []InstallInstruction{}, Fallback: "",
	}
	if slug == "" || s.serverURL == nil {
		output.Fallback = "This MCP server does not have a public installation endpoint. Use the dashboard or ask an organization administrator for the supported setup path."
		return output
	}
	endpoint := s.serverURL.JoinPath("mcp", slug).String()
	switch client {
	case OnboardingClientClaudeCode, OnboardingClientClaudeCowork, OnboardingClientCodex, OnboardingClientCursor, OnboardingClientOpencode, OnboardingClientOther:
		output.Supported = true
		output.Instructions = []InstallInstruction{
			{Title: "Add a remote Streamable HTTP MCP server", Description: "Add this endpoint in your client's MCP settings. The client will open browser sign-in when it first connects.", URL: endpoint},
			{Title: "Complete sign-in", Description: "Finish the browser authorization flow, then return to the client and retry the connection.", URL: ""},
		}
	default:
		output.Fallback = "This client does not have reviewed installation guidance. Add the server as a remote Streamable HTTP MCP only if the client supports browser OAuth."
	}
	return output
}
