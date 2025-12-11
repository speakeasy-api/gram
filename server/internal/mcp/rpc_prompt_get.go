package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/templates"
	templatesRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
)

type prompGetParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type promptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type promptGetResult struct {
	Description string          `json:"description"`
	Messages    []promptMessage `json:"messages"`
}

func handlePromptsGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	var params prompGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse get prompt request").Log(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "promp name is required").Log(ctx, logger)
	}

	tr := templatesRepo.New(db)
	prompt, err := tr.GetTemplateByName(ctx, templatesRepo.GetTemplateByNameParams{
		ProjectID: payload.projectID,
		Name:      params.Name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "prompt not found").Log(ctx, logger)
	}

	promptData, err := templates.RenderTemplate(ctx, logger, prompt.Prompt, prompt.Kind.String, prompt.Engine.String, params.Arguments)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to execute prompt").Log(ctx, logger)
	}

	description := ""
	if prompt.Description.Valid {
		description = prompt.Description.String
	}

	content, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     promptData,
		MimeType: nil,
		Data:     nil,
		Meta:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal content chunk").Log(ctx, logger)
	}

	bs, err := json.Marshal(result[promptGetResult]{
		ID: req.ID,
		Result: promptGetResult{
			Description: description,
			Messages:    []promptMessage{{Role: "user", Content: content}},
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize prompts/get result").Log(ctx, logger)
	}

	return bs, nil
}
