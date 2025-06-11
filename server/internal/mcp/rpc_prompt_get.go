package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/cbroglie/mustache"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/oops"
	templates "github.com/speakeasy-api/gram/internal/templates/repo"
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

// TODO: We will probably want to refactor this into a common prompt execution utility
func executePrompt(prompt templates.PromptTemplate, args map[string]any) (string, error) {
	var data string
	var err error
	switch prompt.Engine.String {
	case "":
		data = prompt.Prompt
	case "mustache":
		data, err = mustache.Render(prompt.Prompt, args)
		if err != nil {
			return "", errors.New("failed to render template")
		}
	default:
		return "", errors.New("unsupported template engine")
	}

	return data, nil
}

func handlePromptsGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	var params prompGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse get prompt request").Log(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "promp name is required").Log(ctx, logger)
	}

	templatesRepo := templates.New(db)
	prompt, err := templatesRepo.GetTemplateByName(ctx, templates.GetTemplateByNameParams{
		ProjectID: payload.projectID,
		Name:      params.Name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "prompt not found").Log(ctx, logger)
	}

	promptData, err := executePrompt(prompt, params.Arguments)
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
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal content chunk").Log(ctx, logger)
	}

	return json.Marshal(result[promptGetResult]{
		ID: req.ID,
		Result: promptGetResult{
			Description: description,
			Messages:    []promptMessage{{Role: "user", Content: content}},
		},
	})
}
