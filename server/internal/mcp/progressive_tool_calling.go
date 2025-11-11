package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

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
			if tool.HTTPToolDefinition == nil {
				continue
			}
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

type listToolsArguments struct {
	Paths []string `json:"paths"`
}

func handleListToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	toolset *types.Toolset,
) (json.RawMessage, error) {
	var args listToolsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse list_tools arguments").Log(ctx, logger)
		}
	}

	if len(args.Paths) == 0 {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing paths"), "paths are required").Log(ctx, logger)
	}

	// Build a map of tools by their hierarchical path (source/group/tool)
	toolsByPath, err := buildToolsByPath(toolset.Tools)
	if err != nil {
		return nil, err
	}

	// Match tools based on requested paths
	matchedTools := make(map[string]*types.Tool)
	for _, path := range args.Paths {
		path = strings.TrimSpace(path)
		for toolPath, tool := range toolsByPath {
			if strings.HasPrefix(toolPath, path) {
				matchedTools[tool.HTTPToolDefinition.Name] = tool
			}
		}
	}

	// Convert to list entries
	entries := make([]*toolListEntry, 0, len(matchedTools))
	for _, tool := range matchedTools {
		if entry := toolToListEntry(tool); entry != nil {
			entry.Meta = nil
			entry.InputSchema = nil
			entries = append(entries, entry)
		}
	}

	payload, err := json.Marshal(toolsListResult{Tools: entries})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool list result").Log(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     string(payload),
		MimeType: nil,
		Data:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool list chunk").Log(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize list_tools response").Log(ctx, logger)
	}

	return response, nil
}

// buildToolsByPath creates a map of hierarchical paths to tools for filtering/matching
func buildToolsByPath(tools []*types.Tool) (map[string]*types.Tool, error) {
	toolsByPath := make(map[string]*types.Tool)
	for _, tool := range tools {
		if tool.HTTPToolDefinition == nil {
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

		// Store tool at multiple path levels for matching
		toolsByPath[fmt.Sprintf("/%s/%s/%s", source, group, tool.HTTPToolDefinition.Name)] = tool
		toolsByPath[fmt.Sprintf("/%s/%s", source, group)] = tool
		toolsByPath[fmt.Sprintf("/%s", source)] = tool
	}
	return toolsByPath, nil
}

type describeToolsArguments struct {
	ToolNames []string `json:"tool_names"`
}

func handleDescribeToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	reqID msgID,
	argsRaw json.RawMessage,
	toolset *types.Toolset,
) (json.RawMessage, error) {
	var args describeToolsArguments
	if len(argsRaw) > 0 {
		if err := json.Unmarshal(argsRaw, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse describe_tools arguments").Log(ctx, logger)
		}
	}

	if len(args.ToolNames) == 0 {
		return nil, oops.E(oops.CodeInvalid, errors.New("missing tool_names"), "tool_names are required").Log(ctx, logger)
	}

	// Build a map of tools by name for quick lookup
	toolsByName := make(map[string]*types.Tool)
	for _, tool := range toolset.Tools {
		baseTool := conv.ToBaseTool(tool)
		toolsByName[baseTool.Name] = tool
	}

	// Find requested tools
	entries := make([]*toolListEntry, 0, len(args.ToolNames))
	notFound := make([]string, 0)
	for _, name := range args.ToolNames {
		name = strings.TrimSpace(name)
		if tool, exists := toolsByName[name]; exists {
			if entry := toolToListEntry(tool); entry != nil {
				entries = append(entries, entry)
			}
		} else {
			notFound = append(notFound, name)
		}
	}

	if len(notFound) > 0 {
		logger.WarnContext(ctx, "some tools not found", attr.SlogExpected(notFound))
	}

	payload, err := json.Marshal(toolsListResult{Tools: entries})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool description result").Log(ctx, logger)
	}

	chunk, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     string(payload),
		MimeType: nil,
		Data:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tool description chunk").Log(ctx, logger)
	}

	response, err := json.Marshal(result[toolCallResult]{
		ID: reqID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize describe_tools response").Log(ctx, logger)
	}

	return response, nil
}
