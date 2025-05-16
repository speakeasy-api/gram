package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/gen/mcp"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
)

type toolsListResult struct {
	Tools []*toolListEntry `json:"tools"`
}

type toolListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty,omitzero"`
}

func handleToolsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *gen.ServePayload, req *rawRequest) (json.RawMessage, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := mv.ProjectID(*authCtx.ProjectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(*payload.Toolset)))
	if err != nil {
		return nil, err
	}

	tools := make([]*toolListEntry, 0, len(toolset.HTTPTools))

	for _, tool := range toolset.HTTPTools {
		tools = append(tools, &toolListEntry{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: json.RawMessage(tool.Schema),
		})
	}

	result := &result[toolsListResult]{
		ID: req.ID,
		Result: toolsListResult{
			Tools: tools,
		},
	}

	return json.Marshal(result)
}
