package platformmcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
)

type mcpScopesManagement interface {
	GetServerScopesForConfirmation(context.Context, *gen.GetServerScopesPayload) (*gen.RemoteMcpServerScopes, string, error)
	SetServerScopePinWithExpectedScopes(context.Context, *gen.SetServerScopePinPayload, []string, string) (*gen.RemoteMcpServerScopes, error)
}

// MCPScopesService preserves owning-service authorization, auditing and scope-pin locking.
type MCPScopesService struct {
	management mcpScopesManagement
	projects   ProjectReadResolver
}

// WithMCPScopes supplies the same scope management service used by the dashboard.
func (r *PostgresReader) WithMCPScopes(management *remotemcp.Service) *PostgresReader {
	if management == nil {
		r.mcpScopes = nil
		return r
	}
	r.mcpScopes = &MCPScopesService{management: management, projects: r}
	return r
}

type MCPScopesInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project ID from list_projects"`
	MCPID     string `json:"mcp_id" jsonschema:"exact remote-backed MCP server ID from get_mcp"`
}

type SetMCPScopePinInput struct {
	ProjectID       string   `json:"project_id" jsonschema:"exact project ID from list_projects"`
	MCPID           string   `json:"mcp_id" jsonschema:"exact remote-backed MCP server ID from get_mcp"`
	Scopes          []string `json:"scopes" jsonschema:"desired pinned scopes; an empty list clears the pin"`
	ExpectedScopes  []string `json:"expected_scopes" jsonschema:"pinned_scopes from the most recent get_mcp_scopes read"`
	ExpectedVersion string   `json:"expected_version" jsonschema:"confirmation_version from the preview; guards the resource URL and affected MCP membership"`
	Confirmed       bool     `json:"confirmed" jsonschema:"true only after the user confirms the exact scopes and shared-server impact"`
}

type MCPClientScopes struct {
	ClientID        string   `json:"client_id"`
	RequestedScopes []string `json:"requested_scopes"`
	Source          string   `json:"source"`
	PinWouldDecide  bool     `json:"pin_would_decide"`
}

type MCPScopesOutput struct {
	ProjectID            string            `json:"project_id"`
	MCPID                string            `json:"mcp_id"`
	PinnedScopes         []string          `json:"pinned_scopes"`
	AdvertisedScopes     []string          `json:"advertised_scopes"`
	AdvertisedKnown      bool              `json:"advertised_known"`
	DiscoveryEnabled     bool              `json:"discovery_enabled"`
	CanPin               bool              `json:"can_pin"`
	SharedServerCount    int               `json:"shared_server_count" jsonschema:"number of OTHER live MCP servers in the same project affected by this pin"`
	Clients              []MCPClientScopes `json:"clients"`
	ProposedScopes       []string          `json:"proposed_scopes"`
	ConfirmationVersion  string            `json:"confirmation_version" jsonschema:"opaque version of the resource URL and affected MCP membership; echo as expected_version when confirming"`
	ConfirmationRequired bool              `json:"confirmation_required"`
	Verified             bool              `json:"verified" jsonschema:"whether stored state was freshly read; does not prove upstream sign-in or granted permissions"`
	NextStep             string            `json:"next_step"`
}

func (s *MCPScopesService) scoped(ctx context.Context, principal Principal, input MCPScopesInput) (context.Context, error) {
	if s == nil || s.management == nil || s.projects == nil {
		return ctx, ErrUnavailable
	}
	projectID, projectErr := uuid.Parse(input.ProjectID)
	mcpID, mcpErr := uuid.Parse(input.MCPID)
	if projectErr != nil || mcpErr != nil || projectID == uuid.Nil || mcpID == uuid.Nil {
		return ctx, oops.E(oops.CodeBadRequest, nil, "an exact project_id and mcp_id are required")
	}
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || principal.UserID == "" || principal.OrganizationID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID || principal.surface() != SurfacePlatformMCP {
		return ctx, ErrForbidden
	}
	project, err := s.projects.ResolveProjectRead(ctx, principal, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return ctx, fmt.Errorf("resolve scope management project: %w", err)
	}
	if project.ID != projectID {
		return ctx, ErrForbidden
	}
	scoped := *auth
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.SetAuthContext(ctx, &scoped), nil
}

func (s *MCPScopesService) get(ctx context.Context, principal Principal, input MCPScopesInput) (MCPScopesOutput, error) {
	scoped, err := s.scoped(ctx, principal, input)
	if err != nil {
		return MCPScopesOutput{}, err
	}
	state, version, err := s.management.GetServerScopesForConfirmation(scoped, &gen.GetServerScopesPayload{McpServerID: input.MCPID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return MCPScopesOutput{}, fmt.Errorf("read MCP scopes: %w", err)
	}
	return mcpScopesOutput(input, state, version), nil
}

func (s *MCPScopesService) set(ctx context.Context, principal Principal, input SetMCPScopePinInput) (MCPScopesOutput, error) {
	target := MCPScopesInput{ProjectID: input.ProjectID, MCPID: input.MCPID}
	scoped, err := s.scoped(ctx, principal, target)
	if err != nil {
		return MCPScopesOutput{}, err
	}
	if len(input.Scopes) > 100 {
		return MCPScopesOutput{}, oops.E(oops.CodeBadRequest, nil, "at most 100 scopes may be pinned")
	}
	scopes := make([]string, 0, len(input.Scopes))
	for _, value := range input.Scopes {
		value = strings.TrimSpace(value)
		if len(value) > 256 {
			return MCPScopesOutput{}, oops.E(oops.CodeBadRequest, nil, "each pinned scope must be at most 256 bytes")
		}
		if value != "" && !slices.Contains(scopes, value) {
			scopes = append(scopes, value)
		}
	}
	if len(scopes) > 100 {
		return MCPScopesOutput{}, oops.E(oops.CodeBadRequest, nil, "at most 100 scopes may be pinned")
	}
	current, err := s.get(scoped, principal, target)
	if err != nil {
		return MCPScopesOutput{}, err
	}
	if len(scopes) > 0 && (!current.CanPin || !current.DiscoveryEnabled) {
		return MCPScopesOutput{}, oops.E(oops.CodeFailedPrecondition, nil, "scope pinning is unavailable or you lack write access to every affected MCP server")
	}
	if !input.Confirmed {
		current.ProposedScopes = scopes
		current.ConfirmationRequired = true
		current.NextStep = "Confirm the exact pinned scopes and the number of other affected MCP servers; then repeat with confirmed true, expected_scopes and expected_version from this read."
		return current, nil
	}
	if input.ExpectedVersion == "" || input.ExpectedVersion != current.ConfirmationVersion {
		return MCPScopesOutput{}, oops.E(oops.CodeConflict, nil, "the affected MCP servers or resource changed; read get_mcp_scopes and confirm again")
	}
	// When discovery is disabled CanPin cannot attest to write access. Let the
	// owning service authorize even an already-empty clear in that case.
	if current.CanPin && slices.Equal(current.PinnedScopes, scopes) {
		return current, nil
	}
	if !slices.Equal(current.PinnedScopes, input.ExpectedScopes) {
		return MCPScopesOutput{}, oops.E(oops.CodeConflict, nil, "the pinned scopes changed; read get_mcp_scopes and confirm again")
	}
	state, err := s.management.SetServerScopePinWithExpectedScopes(scoped, &gen.SetServerScopePinPayload{McpServerID: input.MCPID, Scopes: scopes, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}, input.ExpectedScopes, input.ExpectedVersion)
	if err != nil {
		return MCPScopesOutput{}, fmt.Errorf("set MCP scope pin: %w", err)
	}
	verified, err := s.get(scoped, principal, target)
	if err == nil {
		return verified, nil
	}
	output := mcpScopesOutput(target, state, "")
	output.Verified = false
	output.NextStep = "The pin was saved, but the verification read failed. Call get_mcp_scopes before making another change."
	return output, nil
}

func mcpScopesOutput(input MCPScopesInput, state *gen.RemoteMcpServerScopes, version string) MCPScopesOutput {
	clients := make([]MCPClientScopes, 0, len(state.Clients))
	for _, client := range state.Clients {
		clients = append(clients, MCPClientScopes{ClientID: client.ClientID, RequestedScopes: append([]string{}, client.RequestedScopes...), Source: client.ScopeSource, PinWouldDecide: client.PinWouldDecide})
	}
	return MCPScopesOutput{ProjectID: input.ProjectID, MCPID: input.MCPID, PinnedScopes: append([]string{}, state.PinnedScopes...), AdvertisedScopes: append([]string{}, state.AdvertisedScopes...), AdvertisedKnown: state.AdvertisedScopesKnown, DiscoveryEnabled: state.DiscoveryEnabled, CanPin: state.CanPin, SharedServerCount: state.SharedServerCount, Clients: clients, ProposedScopes: []string{}, ConfirmationVersion: version, ConfirmationRequired: false, Verified: true, NextStep: "Pinned scopes are requests, not a ceiling on granted access. Inspect each client's effective scopes before changing the pin."}
}

func registerMCPScopesTools(reg *Registrar, service *MCPScopesService) {
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil, Name: "get_mcp_scopes", Title: "Inspect MCP Sign-in Scopes", Description: "Read the pinned, advertised and effective sign-in scopes of one exact remote-backed MCP in an explicit project. Shows the number of other MCP servers affected by the shared pin. Read this before proposing a change. Never returns credentials or tokens.", Annotations: readOnlyAnnotations()}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: discoveryMCPRead, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input MCPScopesInput) (*mcp.CallToolResult, MCPScopesOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCPScopesOutput{}, err
		}
		output, err := service.get(ctx, principal, input)
		return nil, output, err
	})
	addTool(reg, &mcp.Tool{Meta: nil, InputSchema: nil, OutputSchema: nil, Icons: nil, Name: "set_mcp_scope_pin", Title: "Set MCP Sign-in Scope Pin", Description: "Set or clear the exact scope pin shared by every remote-backed MCP at the same upstream URL in the project. First call with confirmed false to inspect current scopes and shared-server impact; obtain explicit confirmation, then repeat with confirmed true and the read's pinned_scopes as expected_scopes and confirmation_version as expected_version. Concurrent scope, resource URL or affected-server changes are refused atomically. Setting already-current scopes is a no-op. Requires write access to every affected MCP. A pin is a requested scope list, not a cap; client or challenge scopes may take precedence. Verify the returned effective client scopes.", Annotations: &mcp.ToolAnnotations{IdempotentHint: true, DestructiveHint: new(true), OpenWorldHint: new(false), ReadOnlyHint: false, Title: ""}}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, DiscoveryScopes: discoveryMCPWrite, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input SetMCPScopePinInput) (*mcp.CallToolResult, MCPScopesOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, MCPScopesOutput{}, err
		}
		output, err := service.set(ctx, principal, input)
		return nil, output, err
	})
}
