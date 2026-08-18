//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const unavailableCode = "feature_unavailable"

// bothAudiences admits a tool to the external endpoint and to a project's
// managed assistant. Narrow this per tool when a capability is not fit for
// both surfaces.
var bothAudiences = []Audience{AudienceExternal, AudienceAssistant}

// externalOnly withholds a tool from the assistant. Used where a handler still
// reaches connection-scoped state, which an assistant — acting under assistant
// identity, with no OAuth connection — cannot provide. Admitting such a tool
// would advertise a capability that always fails; each site says which state
// is in the way, so the list shrinks as those paths are made connectionless.
var externalOnly = []Audience{AudienceExternal}

type Reader interface {
	ListProjects(ctx context.Context, principal Principal, input ListProjectsInput) (ListProjectsOutput, error)
	ListProjectMCPs(ctx context.Context, principal Principal, input ListProjectMCPsInput) (ListProjectMCPsOutput, error)
	GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error)
}

type PlatformContext struct {
	OrganizationID string `json:"organization_id"`
	ConnectionID   string `json:"connection_id"`
	ReadOnly       bool   `json:"read_only"`
}

type ListProjectsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"maximum number of projects to return; server clamps this to 100"`
}

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ListProjectsOutput struct {
	Projects  []Project `json:"projects"`
	Truncated bool      `json:"truncated"`
}

type ListProjectMCPsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID to inspect"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of MCPs to return; server clamps this to 100"`
}

type MCP struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Name       string `json:"name,omitempty"`
	Slug       string `json:"slug,omitempty"`
	Visibility string `json:"visibility"`
}

type ListProjectMCPsOutput struct {
	MCPs      []MCP `json:"mcps"`
	Truncated bool  `json:"truncated"`
}

type GetMCPInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID"`
}

type featureUnavailableResult struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Message string `json:"message"`
}

type operationBudgetResult struct {
	Code    string `json:"code"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
}

// newServer composes the Platform MCP tools for one deployment. It returns the
// registrar alongside the server so another admitted audience — the project
// assistant — can be composed from the same registration pass rather than from
// a second list that would drift.
func newServer(reader Reader, catalog Catalog, registrations *RegistrationService, cursorKeyMaterial string, setupResources []SetupResource, feedback *FeedbackService, onboarding *OnboardingService, distributions *DistributionService, candidate CatalogDescriptor) (*mcp.Server, *Registrar) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "speakeasy-aicp-platform-mcp",
		Title:   "Speakeasy AICP Platform MCP",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Use this server to inspect the selected organization and help distribute reviewed MCP servers to an explicit project. List reviewed catalogue options and eligible projects, then ask the user to choose one of each before mutating. Inspect the chosen candidate and collect only its declared non-secret configuration values. Normal non-secret URLs may be discussed and returned. Register it privately. If readiness says an upstream identity provider is missing, ask the user to explicitly confirm and then call attach_platform_mcp_identity_provider; the server derives the provider from the persisted reviewed MCP source and returns its non-secret provider_url plus an Inspect authorization_url for the user to use Connect or Authorize. Immediately present authorization_url as the exact clickable link—never say a link is above or ask the user to confirm an unspecified authorization action. Never request or accept OAuth codes, tokens, client secrets, passwords, API keys, or secret headers in chat. The registration dashboard_setup_url is the Authentication settings fallback, not the authorization page. Force a fresh readiness check after user authorization and add the ready server to the project's existing Default plugin. For the guided flow, use register_platform_mcp_for_project, get_platform_mcp_onboarding_status, attach_platform_mcp_identity_provider when confirmed, and add_platform_mcp_to_default_plugin.",
		PageSize:     32,
	})

	reg := newRegistrar(server)

	registerReadTools(reg, reader)
	registerSetupResources(reg, setupResources)
	if catalog == nil || registrations == nil || !registrations.budgets.Catalog.valid() {
		registerUnavailableCatalogTools(reg)
	} else if cursorCodec, err := newCatalogCursorCodec(cursorKeyMaterial); err != nil {
		registerUnavailableCatalogTools(reg)
	} else {
		registerCatalogTools(reg, catalog, registrations.budgets.Catalog, cursorCodec, onboarding)
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Registration.valid() {
		registerUnavailableCatalogRegistrationTool(reg)
	} else {
		registerCatalogRegistrationTool(reg, registrations)
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Handoff.valid() {
		registerUnavailableSetupHandoffTool(reg)
	} else {
		registerSetupHandoffTool(reg, registrations)
	}
	if registrations == nil || registrations.readiness == nil || !registrations.budgets.Repair.valid() {
		registerUnavailableReadinessTools(reg)
	} else {
		registerReadinessTools(reg, registrations.readiness)
	}
	if onboarding == nil || distributions == nil || catalog == nil || registrations == nil || registrations.store == nil {
		registerUnavailableTools(reg)
	} else {
		registerOnboardingLifecycleTools(reg, onboarding, registrations, distributions)
	}
	if feedback == nil {
		addTool(reg, &mcp.Tool{
			Name:        "send_platform_mcp_feedback",
			Title:       "Send Platform MCP Feedback",
			Description: "Send bounded Platform MCP feedback. Feedback is not enabled in the current rollout.",
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, unavailableTool("platform_mcp_feedback"))
	} else {
		registerFeedbackTool(reg, feedback)
	}
	return server, reg
}

func registerReadTools(reg *Registrar, reader Reader) {
	registerGetPlatformContextTool(reg)
	registerListProjectsTool(reg, reader)
	registerListProjectMCPsTool(reg, reader)
	registerGetMCPTool(reg, reader)
}

// Each stub declares the audiences its live counterpart declares, so a tool
// does not appear on and disappear from a surface as the rollout flips.
func registerUnavailableCatalogTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"search_mcp_catalog", "Search MCP Catalog", "Search reviewed catalog MCP candidates. Catalog access is not enabled in the current rollout."},
		{"inspect_mcp_candidate", "Inspect MCP Candidate", "Inspect one reviewed catalog MCP candidate. Catalog access is not enabled in the current rollout."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("catalog"))
	}
}

func registerUnavailableCatalogRegistrationTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "register_catalog_mcp",
		Title:       "Register Catalog MCP",
		Description: "Register an approved catalog MCP in a project. Registration is not available in the current preview.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("catalog_registration"))
}

func registerUnavailableSetupHandoffTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Get Setup Handoff",
		Description: "Create a secure setup handoff. Provider setup is not available in the current preview.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("setup_handoff"))
}

func registerUnavailableTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		feature     string
	}{

		{"distribute_mcp_to_default_plugin", "Distribute MCP to Default Plugin", "Distribute a configured MCP to the default plugin. Distribution is not available in the current preview.", "plugin_distribution"},
		{"remove_mcp_from_default_plugin", "Remove MCP from Default Plugin", "Remove an MCP from the default plugin. Distribution changes are not available in the current preview.", "plugin_distribution"},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool(tool.feature))
	}
}

func registerUnavailableReadinessTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"get_mcp_readiness", "Get MCP Readiness", "Check configured MCP readiness. Readiness checks are not available in the current preview."},
		{"get_mcp_repair_plan", "Get MCP Repair Plan", "Get a safe MCP repair plan. Repair planning is not available in the current preview."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_readiness"))
	}
}

func operationBudgetToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrReadinessRegistrationNotFound):
		result = operationBudgetResult{Code: "registration_not_found", Message: "This registration ID is not available for the selected project and authenticated connection. Use the ID returned by register_platform_mcp_for_project or get_platform_mcp_onboarding_status."}
	case errors.Is(err, ErrRegistrationInvalid), errors.Is(err, ErrReadinessInvalid), errors.Is(err, ErrCatalogConfigurationRejected), errors.Is(err, ErrCatalogRejected), errors.Is(err, ErrCatalogCursorInvalid):
		result = operationBudgetResult{Code: "invalid_request", Message: "The requested Platform MCP operation is invalid or no longer matches the reviewed catalogue. Re-read the supported tool result and do not retry unchanged input."}
	case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrReadinessRateLimited):
		result = operationBudgetResult{Code: "rate_limited", Message: "This Platform MCP operation is temporarily rate limited. Retry after a short delay."}
	case errors.Is(err, ErrCatalogUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Reason: "catalog_unavailable", Message: "The reviewed MCP Catalogue is temporarily unavailable. Retry the catalogue search after a short delay; other Platform MCP tools may remain available."}
	case errors.Is(err, ErrOperationBudgetUnavailable), errors.Is(err, ErrRegistrationUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Message: "This Platform MCP operation is temporarily unavailable."}
	case errors.Is(err, ErrRegistrationCap):
		result = operationBudgetResult{Code: "conflict", Reason: "active_registration_cap", Message: "This project has reached its active Platform MCP registration limit."}
	case errors.Is(err, ErrRegistrationConflict):
		result = operationBudgetResult{Code: "conflict", Message: "This Platform MCP registration conflicts with the current project state."}
	case errors.Is(err, ErrTargetIneligible):
		result = operationBudgetResult{Code: "ineligible_project", Message: "This project is not eligible for Platform MCP registration because it already has an active legacy toolset-backed MCP."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}

func unavailableTool(feature string) mcp.ToolHandlerFor[map[string]any, featureUnavailableResult] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, featureUnavailableResult, error) {
		result := featureUnavailableResult{
			Code:    unavailableCode,
			Feature: feature,
			Message: "This Platform MCP capability is not enabled for the current rollout.",
		}
		content, err := json.Marshal(result)
		if err != nil {
			return nil, featureUnavailableResult{}, fmt.Errorf("encode unavailable result: %w", err)
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(content)}},
			IsError: true,
		}, result, nil
	}
}

func principalFromToolContext(ctx context.Context) (Principal, error) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		return Principal{}, ErrUnauthorized
	}
	return principal, nil
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return min(limit, 100)
}
