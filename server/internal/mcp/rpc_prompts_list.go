package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
		logger.ErrorContext(ctx, "failed to add prompt arguments schema resource", attr.SlogError(err))
		return args
	}
	if err := compiler.AddResource("file:///schema.json", rawSchema); err != nil {
		logger.ErrorContext(ctx, "failed to add prompt arguments schema resource", attr.SlogError(err))
		return args
	}
	schema, err := compiler.Compile("file:///schema.json")
	if err != nil {
		logger.ErrorContext(ctx, "failed to compile prompt arguments schema", attr.SlogError(err))
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

func handlePromptsList(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents]) (json.RawMessage, error) {
	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	prompts := make([]*promptsListEntry, 0)

	for _, prompt := range toolset.PromptTemplates {
		// TODO: Technically this is no longer necessary--everything in PromptTemplates is an actual MCP prompt now
		if prompt.Kind == "prompt" {
			args := make([]promptArgument, 0)

			if len(prompt.Schema) > 0 {
				args = parsePromptArgumentsFromJSONSchema(prompt.Schema, logger, ctx)
			}

			prompts = append(prompts, &promptsListEntry{
				Name:        prompt.Name,
				Description: &prompt.Description,
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

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize prompts/list response").Log(ctx, logger)
	}

	return bs, nil
}
