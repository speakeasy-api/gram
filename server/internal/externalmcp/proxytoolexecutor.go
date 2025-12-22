package externalmcp

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ToolCallEnv is an alias for toolconfig.ToolCallEnv.
type ToolCallEnv = toolconfig.ToolCallEnv

// ProxyToolNameDelimiter separates the slug from the tool name in proxy tool names.
const ProxyToolNameDelimiter = "--"

// ProxyToolEntry contains metadata needed for matching incoming tool calls to proxy tools.
type ProxyToolEntry struct {
	SourceSlug string   // e.g., "slack" - used for matching
	URN        urn.Tool // URN of the proxy tool (e.g., tools:ext-mcp:slack:proxy)
}

// PlanResolver resolves a tool URN to a ToolCallPlan.
type PlanResolver func(ctx context.Context, toolURN urn.Tool, projectID uuid.UUID) (*ToolCallPlan, error)

// SystemEnvLoader loads system environment variables for a given tool URN.
type SystemEnvLoader func(ctx context.Context, toolURN urn.Tool) (*toolconfig.CaseInsensitiveEnv, error)

// ToolCallPlan contains the execution plan for calling a tool on an external MCP server.
type ToolCallPlan struct {
	RemoteURL         string
	ToolName          string // The tool name to call on the MCP server
	Slug              string
	RequiresOAuth     bool
	TransportType     externalmcptypes.TransportType
	HeaderDefinitions []HeaderDefinition
}

// HeaderDefinition maps an environment variable name to an HTTP header name.
type HeaderDefinition struct {
	Name       string // Prefixed environment variable name (e.g., "SLACK_X_API_KEY")
	HeaderName string // HTTP header to send (e.g., "X-Api-Key")
}

// ProxyToolExecutor provides matching for external MCP tool names.
// The executor matches tool names against MCP server slugs and returns the
// external tool name. Actual execution happens elsewhere using the returned
// URN to get a plan and create a client.
type ProxyToolExecutor struct {
	logger  *slog.Logger
	entries []ProxyToolEntry
}

// HasEntries returns true if this executor has any proxy tool entries.
func (e *ProxyToolExecutor) HasEntries() bool {
	return len(e.entries) > 0
}

// MatchPlanInputs checks if the given tool name belongs to any proxy tool in this executor.
// If matched, resolves the ToolCallPlan inputs and sets ToolName to the external tool name.
// Returns nil if no match (not an error). Returns error if resolver fails.
// Expected format: <slug>--<toolName>
func (e *ProxyToolExecutor) MatchPlanInputs(ctx context.Context, toolName string, projectID uuid.UUID, resolve PlanResolver) (*ToolCallPlan, error) {
	parts := strings.SplitN(toolName, ProxyToolNameDelimiter, 2)
	if len(parts) != 2 {
		return nil, nil // No match
	}

	slug := parts[0]
	externalToolName := parts[1]

	for _, entry := range e.entries {
		if entry.SourceSlug == slug {
			plan, err := resolve(ctx, entry.URN, projectID)
			if err != nil {
				return nil, err
			}
			plan.ToolName = externalToolName
			return plan, nil
		}
	}

	return nil, nil // No match
}

// DoList lists tools from all proxy tools in this executor.
// For each entry, loads system env, resolves the plan, connects to the MCP server,
// lists tools, and prefixes tool names with the source slug.
func (e *ProxyToolExecutor) DoList(
	ctx context.Context,
	projectID uuid.UUID,
	userConfig *toolconfig.CaseInsensitiveEnv,
	oauthToken string,
	loadSystemEnv SystemEnvLoader,
	resolve PlanResolver,
) ([]Tool, error) {
	var allTools []Tool

	for _, entry := range e.entries {
		tools, err := e.listToolsForEntry(ctx, projectID, userConfig, oauthToken, entry, loadSystemEnv, resolve)
		if err != nil {
			e.logger.ErrorContext(ctx, "failed to list tools for proxy entry",
				attr.SlogExternalMCPSlug(entry.SourceSlug),
				attr.SlogError(err),
			)
			continue
		}

		// Prefix tool names with slug
		for i := range tools {
			tools[i].Name = entry.SourceSlug + ProxyToolNameDelimiter + tools[i].Name
		}

		allTools = append(allTools, tools...)
	}

	return allTools, nil
}

// listToolsForEntry lists tools for a single proxy entry.
func (e *ProxyToolExecutor) listToolsForEntry(
	ctx context.Context,
	projectID uuid.UUID,
	userConfig *toolconfig.CaseInsensitiveEnv,
	oauthToken string,
	entry ProxyToolEntry,
	loadSystemEnv SystemEnvLoader,
	resolve PlanResolver,
) ([]Tool, error) {
	plan, err := resolve(ctx, entry.URN, projectID)
	if err != nil {
		return nil, err
	}

	systemEnv, err := loadSystemEnv(ctx, entry.URN)
	if err != nil {
		return nil, err
	}

	// Only pass OAuth token if this plan requires it
	var tokenForHeaders string
	if plan.RequiresOAuth {
		tokenForHeaders = oauthToken
	}

	headers := BuildHeaders(systemEnv, userConfig, plan.HeaderDefinitions, tokenForHeaders)

	client, err := NewClient(ctx, e.logger, plan.RemoteURL, plan.TransportType, &ClientOptions{
		Authorization: "",
		Headers:       headers,
	})
	if err != nil {
		return nil, err
	}
	defer o11y.LogDefer(ctx, e.logger, client.Close)

	return client.ListTools(ctx)
}

// BuildProxyToolExecutor creates a ProxyToolExecutor from a list of tools.
// Filters internally to only include external MCP tools with Type "proxy".
func BuildProxyToolExecutor(logger *slog.Logger, tools []*types.Tool) *ProxyToolExecutor {
	var entries []ProxyToolEntry

	for _, tool := range tools {
		if tool == nil || tool.ExternalMcpToolDefinition == nil {
			continue
		}
		def := tool.ExternalMcpToolDefinition
		if def.Type == nil || *def.Type != "proxy" {
			continue
		}

		toolURN, err := urn.ParseTool(def.ToolUrn)
		if err != nil {
			continue
		}

		entries = append(entries, ProxyToolEntry{
			SourceSlug: def.Slug,
			URN:        toolURN,
		})
	}

	return &ProxyToolExecutor{
		logger:  logger,
		entries: entries,
	}
}
