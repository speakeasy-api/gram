//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	FindMCP(ctx context.Context, principal Principal, input FindMCPInput) (FindMCPOutput, error)
	GetMCP(ctx context.Context, principal Principal, input GetMCPInput) (MCP, error)
}

type PlatformContext struct {
	OrganizationID string `json:"organization_id"`
	ConnectionID   string `json:"connection_id"`
	ReadOnly       bool   `json:"read_only"`
	// Overview orients a caller that has only ever seen this server's tool
	// names. The manifest can say what one tool does; only something read at
	// the start of a conversation can say how the pieces relate, which is what
	// keeps a reply from narrating the machinery instead of the outcome.
	Overview string `json:"overview"`
}

// platformOverview is the one plain-language account of this platform a caller
// gets. It names the stages in the order a person experiences them and glosses
// the two words — project and plugin — that have no meaning outside Gram.
const platformOverview = "An administrator sets MCP servers and skills up here, then hands them to the people in their organization. " +
	"A project is where that work is kept before anyone receives it. A plugin is a bundle of MCP servers and skills you share with people. " +
	"An MCP server reaches a person in stages: added to a project, its OAuth provider connected, confirmed working, put into a plugin, and published to the people it is shared with. " +
	"A skill is a written set of instructions an agent loads when it applies; it also reaches people through a plugin or an assistant."

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

type FindMCPInput struct {
	// At most one project selector may be supplied. Without a selector, an
	// unfiltered list uses the organization's Default project; a query searches
	// the organization. The assistant policy injects project_id and removes both
	// selectors from its model-visible schema.
	ProjectID   string `json:"project_id,omitempty" jsonschema:"optional project ID; defaults to the organization's Default project when query is omitted"`
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"optional project slug; defaults to the organization's Default project when query is omitted"`
	Query       string `json:"query,omitempty" jsonschema:"optional MCP name, slug, or ID search; without a project selector, searches the organization"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by a previous unfiltered find_mcp result"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of MCPs to return; server clamps this to 100"`
	Readiness   string `json:"readiness,omitempty" jsonschema:"optional persisted readiness state filter"`
}

type MCPSource struct {
	Kind      string `json:"kind"`
	Provider  string `json:"provider,omitempty"`
	Reference string `json:"reference,omitempty"`
}

type MCPReadiness struct {
	State     string `json:"state"`
	CheckedAt string `json:"checked_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type MCPDistribution struct {
	PluginID         string `json:"plugin_id"`
	State            string `json:"state"`
	PublicationState string `json:"publication_state"`
}

type MCP struct {
	ID               string            `json:"id"`
	ProjectID        string            `json:"project_id"`
	ProjectName      string            `json:"project_name,omitempty"`
	ProjectSlug      string            `json:"project_slug,omitempty"`
	Name             string            `json:"name,omitempty"`
	Slug             string            `json:"slug,omitempty"`
	Version          string            `json:"version,omitempty"`
	Visibility       string            `json:"visibility"`
	EffectiveEnabled bool              `json:"effective_enabled"`
	Model            string            `json:"model"`
	Source           MCPSource         `json:"source"`
	Registration     *MCPRegistration  `json:"registration,omitempty"`
	Readiness        MCPReadiness      `json:"readiness"`
	Distributions    []MCPDistribution `json:"distributions"`
	Operations       []string          `json:"operations"`
	DashboardPath    string            `json:"dashboard_path,omitempty"`
}

type MCPRegistration struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	ComponentsComplete bool   `json:"components_complete"`
}

type FindMCPOutput struct {
	MCPs       []MCP  `json:"mcps"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type GetMCPInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID that owns the MCP"`
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
func newServer(reader Reader, catalog Catalog, registrations *RegistrationService, cursorKeyMaterial string, setupResources []SetupResource, feedback *FeedbackService, onboarding *OnboardingService, distributions *DistributionService, skills *SkillsService, diagnostics *DiagnosticsService, plugins *PluginsService, candidate CatalogDescriptor) (*mcp.Server, *Registrar) {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "platform-mcp",
		Title:   "Platform MCP",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: strings.Join([]string{
			"# What this server is",
			"Gram's AI Control Plane lets an administrator put MCP servers and skills in front of the people in their organization. An MCP server reaches a person in stages: added to a project, its OAuth provider connected, confirmed working, put into a plugin — the bundle of MCP servers and skills you share with people — and published to them. Someone asking to \"set up Slack for support\" is asking for all five. Name the stage you are at in those words.",
			"# How to talk about it",
			"You are speaking to an administrator who knows what MCP and OAuth are, but has never seen how this platform is built. Industry terms — MCP server, Streamable HTTP, OAuth, identity provider, dynamic client registration — need no explanation. This platform's own words do: the first time a reply uses plugin, registration, distribution, publication, readiness, catalogue, skill, or version, say what it means in the same sentence.",
			"Never say these aloud: tool names, IDs, cursors, receipts, version tokens, idempotency keys, error codes, rollout or preview status, or how this server routes between its callers. Those are mechanism. Report what changed for the administrator's people and what to do next.",
			"Keep the words that collide apart. \"Connect\" is linking an MCP server to its OAuth provider; \"add to a plugin\" is plugin membership; do not call either one attaching. Say an MCP server was \"added to the project\" rather than \"registered\", so it is not mistaken for OAuth dynamic client registration. When a diagnosis blames the calling MCP client, say \"the app making the calls\", never bare \"client\".",
			"# Rules",
			"Use this server to inspect the selected organization and manage reviewed MCP servers in an explicit project. List reviewed catalogue options and eligible projects, then ask the user to choose one of each before mutating. Inspect the chosen candidate and collect only its declared non-secret configuration values. Normal non-secret URLs may be discussed and returned. Register it privately.",
			"Use get_mcp_readiness with the returned registration ID to inspect persisted readiness. If readiness says an upstream identity provider is missing, ask the user to explicitly confirm and then call attach_platform_mcp_identity_provider; the server derives the provider from the persisted reviewed MCP source and returns its non-secret provider_url plus an Inspect authorization_url for the user to use Connect or Authorize. Immediately present authorization_url as the exact clickable link—never say a link is above or ask the user to confirm an unspecified authorization action.",
			"Never request or accept OAuth codes, tokens, client secrets, passwords, API keys, or secret headers in chat. The registration dashboard_setup_url is the Authentication settings fallback, not the authorization page. Force a fresh readiness check after user authorization.",
			"Setup also decides which MCP clients may sign in to the new server: read get_mcp_client_admission, explain in plain words which apps that lets in, and only change it with set_mcp_client_admission after the user explicitly confirms.",
			"Registration never distributes an MCP: use list_plugins to show the project's plugins, ask the user which one should carry it, then call distribute_mcp_to_plugin naming that plugin exactly. There is no implicit default.",
		}, "\n\n"),
		PageSize: 32,
	})

	reg := newRegistrar(server)

	registerReadTools(reg, reader, cursorKeyMaterial)
	registerSetupResources(reg, setupResources, time.Now)
	if registrations == nil || !registrations.budgets.Docs.valid() {
		registerUnavailableSearchDocsTool(reg)
	} else {
		// The search index reads the same pinned corpus the resources are
		// registered from, so a citation's URI always resolves to a resource
		// this deployment actually serves.
		registerSearchDocsTool(reg, NewMemoryDocsIndex(setupResources, time.Now), registrations.budgets.Docs)
	}
	registerReadDocTool(reg)
	if registrations == nil || !registrations.budgets.Catalog.valid() {
		registerUnavailableCatalogTools(reg)
		registerUnavailableCandidateInspectionTool(reg)
	} else if catalog == nil && (registrations.directRemoteInspector == nil || registrations.gate == nil) {
		registerUnavailableCatalogTools(reg)
		registerUnavailableCandidateInspectionTool(reg)
	} else {
		registerCandidateInspectionTool(reg, catalog, registrations.directRemoteInspector, registrations.gate, registrations.budgets.Catalog)
		if catalog == nil {
			registerUnavailableCatalogTools(reg)
		} else if cursorCodec, err := newCatalogCursorCodec(cursorKeyMaterial); err != nil {
			registerUnavailableCatalogTools(reg)
		} else {
			registerCatalogTools(reg, catalog, registrations.budgets.Catalog, cursorCodec, onboarding)
		}
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Registration.valid() {
		registerUnavailableCatalogRegistrationTool(reg)
		registerUnavailableRemoteRegistrationTool(reg)
		registerUnavailableIdentityProviderTool(reg)
	} else {
		registerCatalogRegistrationTool(reg, registrations, onboarding)
		if registrations.directRemoteInspector == nil {
			registerUnavailableRemoteRegistrationTool(reg)
		} else {
			registerRemoteRegistrationTool(reg, registrations, onboarding)
		}
		registerIdentityProviderTool(reg, registrations)
	}
	if registrations == nil || registrations.lifecycleMetadata == nil {
		registerUnavailableLifecycleMetadataTool(reg)
	} else {
		registerLifecycleMetadataTool(reg, registrations)
	}
	if registrations == nil || registrations.lifecycleVisibility == nil {
		registerUnavailableLifecycleVisibilityTools(reg)
	} else {
		registerLifecycleVisibilityTools(reg, registrations)
	}
	if registrations == nil || registrations.store == nil || !registrations.budgets.Handoff.valid() {
		registerUnavailableSetupHandoffTool(reg)
	} else {
		registerSetupHandoffTool(reg, registrations)
	}
	if registrations == nil || !registrations.clientAdmission.valid() || !registrations.budgets.LifecycleMetadata.valid() {
		registerUnavailableClientAdmissionTools(reg)
	} else {
		registerClientAdmissionTools(reg, registrations)
	}
	if registrations == nil || registrations.readiness == nil || !registrations.budgets.Repair.valid() {
		registerUnavailableReadinessTools(reg)
	} else {
		registerReadinessTools(reg, registrations.readiness)
	}
	// Exact-plugin distribution is live once the workflow services it writes
	// through are composed; without them the canonical descriptors stay visible
	// as stubs rather than disappearing from the manifest.
	if onboarding == nil || distributions == nil {
		registerUnavailableTools(reg)
	} else {
		registerDistributionTools(reg, onboarding, distributions)
	}
	if !diagnostics.valid() {
		registerUnavailableDiagnosticsTools(reg)
	} else {
		registerDiagnosticsTools(reg, diagnostics)
	}
	if !diagnostics.drilldownValid() {
		registerUnavailableDrilldownTools(reg)
	} else {
		registerDrilldownTools(reg, diagnostics)
	}
	if !skills.valid() {
		registerUnavailableSkillsTools(reg)
	} else {
		registerSkillsTools(reg, skills)
	}
	if !plugins.valid() {
		registerUnavailablePluginTools(reg)
	} else {
		registerPluginTools(reg, plugins)
	}
	if feedback == nil {
		addTool(reg, &mcp.Tool{
			Name:        "send_platform_mcp_feedback",
			Title:       "Send Feedback About This Platform",
			Description: "Send feedback about this platform. This is not switched on for your organization yet.",
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, unavailableTool("platform_mcp_feedback"))
	} else {
		registerFeedbackTool(reg, feedback)
	}
	return server, reg
}

func registerReadTools(reg *Registrar, reader Reader, cursorKeyMaterial string) {
	registerGetPlatformContextTool(reg)
	registerListProjectsTool(reg, reader)
	registerFindMCPTool(reg, reader, cursorKeyMaterial)
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
		{"search_mcp_catalog", "Search Reviewed MCP Servers", "Search the reviewed MCP servers available to add. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("catalog"))
	}
}

func registerUnavailableCandidateInspectionTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "inspect_mcp_candidate",
		Title:       "Inspect an MCP Server",
		Description: "Look at one MCP server before adding it. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, unavailableTool("candidate_inspection"))
}

func registerUnavailableCatalogRegistrationTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "register_catalog_mcp",
		Title:       "Add a Reviewed MCP Server to a Project",
		Description: "Add a reviewed MCP server to a project. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("catalog_registration"))
}

func registerUnavailableRemoteRegistrationTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "register_remote_mcp",
		Title:       "Add Your Own MCP Server to a Project",
		Description: "Add an MCP server of your own to a project. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("direct_remote_registration"))
}

func registerUnavailableLifecycleMetadataTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "update_mcp_metadata",
		Title:       "Rename an MCP Server",
		Description: "Rename one MCP server. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_lifecycle_metadata"))
}

func registerUnavailableSetupHandoffTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "get_setup_handoff",
		Title:       "Open Setup in the Dashboard",
		Description: "Open the dashboard to finish setting up an MCP server. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("setup_handoff"))
}

func registerUnavailableTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
		feature     string
	}{

		{"distribute_mcp_to_plugin", "Add an MCP Server to a Plugin", "Give an MCP server to one plugin, so the people it reaches get it. This is not switched on for your organization yet.", "plugin_distribution"},
		{"remove_mcp_from_plugin", "Remove an MCP Server from a Plugin", "Take an MCP server back out of one plugin. This is not switched on for your organization yet.", "plugin_distribution"},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
		}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool(tool.feature))
	}
}

func registerUnavailableReadinessTools(reg *Registrar) {
	for _, tool := range []struct {
		name        string
		title       string
		description string
	}{
		{"get_mcp_readiness", "Check If an MCP Server Is Working", "Check whether an MCP server is working. This is not switched on for your organization yet."},
		{"get_mcp_repair_plan", "What to Fix on an MCP Server", "See what to fix on an MCP server that is not working. This is not switched on for your organization yet."},
	} {
		addTool(reg, &mcp.Tool{
			Name:        tool.name,
			Title:       tool.title,
			Description: tool.description,
			Annotations: readOnlyAnnotations(),
		}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeExplicit}, unavailableTool("mcp_readiness"))
	}
}

func operationBudgetToolResult(err error) (*mcp.CallToolResult, bool) {
	var result operationBudgetResult
	switch {
	case errors.Is(err, ErrReadinessRegistrationNotFound):
		result = operationBudgetResult{Code: "registration_not_found", Message: "That MCP server is not one this project and caller can act on. Use the one returned when it was added to the project."}
	case errors.Is(err, ErrRegistrationInvalid), errors.Is(err, ErrLifecycleMetadataInvalid), errors.Is(err, ErrLifecycleVisibilityInvalid), errors.Is(err, ErrReadinessInvalid), errors.Is(err, ErrCatalogConfigurationRejected), errors.Is(err, ErrCatalogRejected), errors.Is(err, ErrCatalogCursorInvalid):
		result = operationBudgetResult{Code: "invalid_request", Message: "That request is not valid, or no longer matches how things are set up now. Read the current state again; do not retry the same input."}
	case errors.Is(err, ErrOperationRateLimited), errors.Is(err, ErrReadinessRateLimited):
		result = operationBudgetResult{Code: "rate_limited", Message: "That was asked for too often just now. Try again shortly."}
	case errors.Is(err, ErrCatalogUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Reason: "catalog_unavailable", Message: "The reviewed catalogue of MCP servers is temporarily unavailable. Try searching it again shortly; everything else still works."}
	case errors.Is(err, ErrDirectRemoteRejected):
		result = operationBudgetResult{Code: "invalid_request", Reason: "remote_url_rejected", Message: "That MCP server URL is unsafe, unsupported, or did not answer a Streamable HTTP check. Use an HTTPS URL with no credentials, query parameters, or fragments."}
	case errors.Is(err, ErrDirectRemoteUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Reason: "remote_inspection_unavailable", Message: "That MCP server could not be checked safely right now. Try again shortly."}
	case errors.Is(err, ErrLifecycleVisibilityUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Reason: "unsupported_lifecycle_target", Message: "This MCP server was not set up through this platform, so it cannot be turned on or off from here. Manage it in the dashboard instead."}
	case errors.Is(err, ErrOperationBudgetUnavailable), errors.Is(err, ErrRegistrationUnavailable):
		result = operationBudgetResult{Code: unavailableCode, Message: "That is temporarily unavailable. Try again shortly."}
	case errors.Is(err, ErrRegistrationCap):
		result = operationBudgetResult{Code: "conflict", Reason: "active_registration_cap", Message: "This project already holds as many MCP servers as it can. Remove one, or use another project."}
	case errors.Is(err, ErrRegistrationConflict):
		result = operationBudgetResult{Code: "conflict", Message: "That MCP server conflicts with something already set up in this project."}
	case errors.Is(err, ErrTargetIneligible):
		result = operationBudgetResult{Code: "ineligible_project", Message: "This project already has an older-style MCP server set up, so it cannot use this flow. Pick a different project."}
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
			Message: "This is not switched on for your organization yet.",
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
