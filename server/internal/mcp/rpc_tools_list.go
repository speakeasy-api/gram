package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
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
}

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, productMetrics *posthog.Posthog) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)))
	if err != nil {
		return nil, err
	}

	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		if err := productMetrics.CaptureEvent(ctx, "mcp_server_count", payload.projectID.String(), map[string]interface{}{
			"project_id":          payload.projectID.String(),
			"organization_id":     toolset.OrganizationID,
			"authenticated":       payload.authenticated,
			"toolset":             toolset.Name,
			"toolset_slug":        toolset.Slug,
			"toolset_id":          toolset.ID,
			"mcp_domain":          requestContext.Host,
			"mcp_url":             requestContext.Host + requestContext.ReqURL,
			"disable_noification": true,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to capture mcp_server_count event", attr.SlogError(err))
		}
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
		promptArgs := mv.DefaultEmptyToolSchema
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

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/list response").Log(ctx, logger)
	}

	return bs, nil
}
