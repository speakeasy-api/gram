package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

type toolsListResult struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty,omitzero"`
	Meta        map[string]any  `json:"_meta,omitempty"`
}

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, productMetrics *posthog.Posthog, toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents]) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_server_count", payload.sessionID, map[string]interface{}{
			"project_id":           payload.projectID.String(),
			"organization_id":      toolset.OrganizationID,
			"authenticated":        payload.authenticated,
			"toolset":              toolset.Name,
			"toolset_slug":         toolset.Slug,
			"toolset_id":           toolset.ID,
			"mcp_domain":           requestContext.Host,
			"mcp_url":              requestContext.Host + requestContext.ReqURL,
			"mcp_enabled":          toolset.McpEnabled,
			"disable_notification": true,
			"mcp_session_id":       payload.sessionID,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_server_count event", attr.SlogError(err))
		}
	}

	var tools []*toolListEntry
	switch payload.mode {
	case ToolModeProgressive:
		tools = buildProgressiveSessionTools(toolset)
	case ToolModeStatic:
		fallthrough
	default:
		tools = buildToolListEntries(toolset.Tools)
	}

	result := &result[toolsListResult]{
		ID: req.ID,
		Result: toolsListResult{
			Tools: tools,
		},
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").Log(ctx, logger)
	}

	return bs, nil
}

func buildToolListEntries(tools []*types.Tool) []*toolListEntry {
	result := make([]*toolListEntry, 0, len(tools))
	for _, tool := range tools {
		if entry := toolToListEntry(tool); entry != nil {
			result = append(result, entry)
		}
	}
	return result
}

func toolToListEntry(tool *types.Tool) *toolListEntry {
	if tool == nil {
		return nil
	}

	var meta map[string]any
	if tool.FunctionToolDefinition != nil {
		meta = tool.FunctionToolDefinition.Meta
	}

	baseTool := conv.ToBaseTool(tool)

	return &toolListEntry{
		Name:        baseTool.Name,
		Description: baseTool.Description,
		InputSchema: json.RawMessage(baseTool.Schema),
		Meta:        meta,
	}
}

func buildProgressiveSessionTools(toolset *types.Toolset) []*toolListEntry {
	listToolRequired := len(toolset.Tools) > 50

	listTools, err := buildListToolsTool(toolset.Tools)
	if err != nil {
		println(err.Error())
		return nil
	}

	describeTools, err := buildDescribeToolsTool(toolset.Tools, listToolRequired)
	if err != nil {
		println(err.Error())
		return nil
	}

	toolsetName := ""
	toolsetDescription := ""
	if toolset != nil {
		toolsetName = toolset.Name
		toolsetDescription = conv.PtrValOrEmpty(toolset.Description, "")
	}
	contextDescription := toolsetName
	if contextDescription != "" && toolsetDescription != "" {
		contextDescription = fmt.Sprintf("%s (%s)", toolsetName, toolsetDescription)
	}
	if contextDescription == "" && toolsetDescription != "" {
		contextDescription = toolsetDescription
	}

	executeDescription := "Execute a specific tool by name, passing through the correct arguments for that tool's schema."
	if contextDescription != "" {
		executeDescription = fmt.Sprintf("Execute a specific tool from %s by name, passing through the correct arguments for that tool's schema.", contextDescription)
	}

	tools := []*toolListEntry{}
	if listToolRequired {
		tools = append(tools, listTools)
	}
	tools = append(tools, describeTools)
	tools = append(tools, &toolListEntry{
		Name:        executeToolToolName,
		Description: executeDescription,
		InputSchema: dynamicExecuteToolSchema,
		Meta:        nil,
	})
	return tools
}

func buildDescribeToolsTool(tools []*types.Tool, listToolRequired bool) (*toolListEntry, error) {
	description := "Describe a set of tools by name. Use this to get more information about a tool, such as its description and input schema. Do not call a tool without first describing it."

	if listToolRequired {
		description += " You can find what tools are available using the list_tools tool."
	} else {
		toolNames := []string{}
		for _, tool := range tools {
			toolNames = append(toolNames, tool.HTTPToolDefinition.Name)
		}
		description += fmt.Sprintf(" The available tools are: %s.", strings.Join(toolNames, ", "))
	}

	schemaJSON := `{
		"type": "object",
		"properties": {
			"tool_names": {
				"type": "array",
				"items": {
					"type": "string",
					"description": "Exact name of the tool to describe. Example: 'get_github_repo'"
				},
				"description": "Names of the tools to describe."
			}
		},
		"required": ["tool_names"],
		"additionalProperties": false
	}`
	return &toolListEntry{
		Name:        describeToolsToolName,
		Description: description,
		InputSchema: json.RawMessage(schemaJSON),
		Meta:        nil,
	}, nil
}

func buildListToolsTool(tools []*types.Tool) (*toolListEntry, error) {
	type sourceGroup struct {
		groups map[string][]string
	}

	tree := make(map[string]*sourceGroup)

	for _, tool := range tools {
		if tool.HTTPToolDefinition == nil { // TODO support other tool types
			continue
		}

		toolURN, err := conv.GetToolURN(*tool)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get tool urn")
		}

		source := toolURN.Source
		group := "no-group"
		tags := tool.HTTPToolDefinition.Tags
		if len(tags) > 0 {
			group = tags[0]
		}

		if tree[source] == nil {
			tree[source] = &sourceGroup{
				groups: make(map[string][]string),
			}
		}

		tree[source].groups[group] = append(tree[source].groups[group], tool.HTTPToolDefinition.Name)
	}

	totalTools := len(tools)
	showIndividualTools := totalTools <= 50

	var pathsDesc string
	var examplePaths []string
	for source, sg := range tree {
		pathsDesc += fmt.Sprintf("\n/%s", source)
		for group, toolNames := range sg.groups {
			groupPath := fmt.Sprintf("/%s/%s", source, group)
			if showIndividualTools {
				pathsDesc += fmt.Sprintf("\n  /%s", group)
				for _, toolName := range toolNames {
					pathsDesc += fmt.Sprintf("\n    /%s", toolName)
				}
			} else {
				pathsDesc += fmt.Sprintf("\n  /%s [%d tools]", group, len(toolNames))
			}
			if len(examplePaths) < 3 {
				examplePaths = append(examplePaths, groupPath)
			}
		}
	}

	description := fmt.Sprintf("List tools available for a given set of paths. Paths are hierarchical (source/group/tool) and can be used to filter the tools returned. Use paths like /slack/messages to get all tools in that group. Available paths:%s", pathsDesc)

	schemaJSON := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"paths": {
				"type": "array",
				"items": {
					"type": "string",
					"description": "Path to return tools for. Example: %s"
				},
				"description": "Paths to return tools for. Example: [%s]"
			}
		},
		"required": ["paths"],
		"additionalProperties": false
	}`, examplePaths[0], strings.Join(examplePaths, ", "))

	return &toolListEntry{
		Name:        listToolsToolName,
		Description: description,
		InputSchema: json.RawMessage(schemaJSON),
		Meta:        nil,
	}, nil
}

var (
	dynamicExecuteToolSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Exact name of the tool to execute."
			},
			"arguments": {
				"description": "JSON payload to forward to the tool as its arguments."
			}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
)
