package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/mv"
)

type toolsListResult struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty,omitzero"`
}

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)))
	if err != nil {
		return nil, err
	}

	tools := make([]*toolListEntry, 0)

	for _, tool := range toolset.HTTPTools {
		tools = append(tools, &toolListEntry{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: json.RawMessage(tool.Schema),
		})
	}

	for _, prompt := range toolset.PromptTemplates {
		promptArgs := "{}"
		if prompt.Arguments != nil {
			promptArgs = *prompt.Arguments
		}
		if prompt.Kind == "higher_order_tool" {
			desc := ""
			if prompt.Description != nil {
				desc = *prompt.Description
			}
			tools = append(tools, &toolListEntry{
				Name:        string(prompt.Name),
				Description: desc,
				InputSchema: json.RawMessage(promptArgs),
			})
		}
	}

	result := &result[toolsListResult]{
		ID: req.ID,
		Result: toolsListResult{
			Tools: tools,
		},
	}

	return json.Marshal(result)
}
