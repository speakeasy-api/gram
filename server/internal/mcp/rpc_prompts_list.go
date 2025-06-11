package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/mv"
)

type promptsListResult struct {
	Prompts []*promptsListEntry `json:"prompts"`
}

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

type promptsListEntry struct {
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	Arguments   []promptArgument `json:"arguments"`
}

func parsePromptArgumentsFromJSONSchema(schemaStr string, logger *slog.Logger, ctx context.Context) []promptArgument {
	args := make([]promptArgument, 0)
	compiler := jsonschema.NewCompiler()
	rawSchema, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(schemaStr)))
	if err != nil {
		logger.ErrorContext(ctx, "failed to add prompt arguments schema resource", slog.String("error", err.Error()))
		return args
	}
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		logger.ErrorContext(ctx, "failed to add prompt arguments schema resource", slog.String("error", err.Error()))
		return args
	}
	schema, err := compiler.Compile("file:///schema.json")
	if err != nil {
		logger.ErrorContext(ctx, "failed to compile prompt arguments schema", slog.String("error", err.Error()))
		return args
	}

	requiredSet := make(map[string]bool)
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	for name, prop := range schema.Properties {
		desc := prop.Description
		args = append(args, promptArgument{
			Name:        name,
			Description: desc,
			Required:    requiredSet[name],
		})
	}
	return args
}

func handlePromptsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)))
	if err != nil {
		return nil, err
	}

	prompts := make([]*promptsListEntry, 0)

	for _, prompt := range toolset.PromptTemplates {
		if prompt.Kind == "prompt" {
			description := ""
			if prompt.Description != nil {
				description = *prompt.Description
			}
			args := make([]promptArgument, 0)

			if prompt.Arguments != nil && len(*prompt.Arguments) > 0 {
				args = parsePromptArgumentsFromJSONSchema(*prompt.Arguments, logger, ctx)
			}

			prompts = append(prompts, &promptsListEntry{
				Name:        string(prompt.Name),
				Description: &description,
				Arguments:   args,
			})
		}
	}

	result := &result[promptsListResult]{
		ID: req.ID,
		Result: promptsListResult{
			Prompts: prompts,
		},
	}

	return json.Marshal(result)
}
