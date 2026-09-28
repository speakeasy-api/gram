package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/templates"
	templatesRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
)

type prompGetParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type promptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type promptGetResult struct {
	Description string          `json:"description"`
	Messages    []promptMessage `json:"messages"`
}

func handlePromptsGet(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, payload *mcpInputs, req *rawRequest, scan *mcpriskscan.Evaluator) (json.RawMessage, error) {
	var params prompGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse get prompt request").LogError(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "promp name is required").LogError(ctx, logger)
	}
	var arguments map[string]any
	if len(params.Arguments) > 0 && string(params.Arguments) != "null" {
		if err := json.Unmarshal(params.Arguments, &arguments); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse prompt arguments").LogError(ctx, logger)
		}
	}

	tr := templatesRepo.New(db)
	prompt, err := tr.GetTemplateByName(ctx, templatesRepo.GetTemplateByNameParams{
		ProjectID: payload.projectID,
		Name:      params.Name,
	})
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "prompt not found").LogError(ctx, logger)
	}

	serverID := ""
	if payload.mcpServerID != nil {
		serverID = payload.mcpServerID.String()
	}
	decision := scan.Scan(ctx, mcpriskscan.NewRequest(ctx, mcpriskscan.Event{
		Surface:        mcpriskscan.SurfaceHostedMCP,
		Method:         mcpriskscan.MethodPromptsGet,
		OrganizationID: payload.organizationID,
		ProjectID:      payload.projectID.String(),
		ServerID:       serverID,
		MetaServerID:   payload.metaMcpServerID,
		ToolsetID:      "",
		ToolName:       "",
		ResourceURI:    "",
		PromptName:     params.Name,
		ChatID:         payload.chatID,
	}, mcpriskscan.BorrowPayload(params.Arguments)))
	if decision.Denied() {
		return nil, oops.E(oops.CodeForbidden, nil, "%s", decision.UserMessage)
	}

	promptData, err := templates.RenderTemplate(ctx, logger, prompt.Prompt, prompt.Kind.String, prompt.Engine.String, arguments)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to execute prompt").LogError(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal content chunk").LogError(ctx, logger)
	}

	bs, err := json.Marshal(result[promptGetResult]{
		ID: req.ID,
		Result: promptGetResult{
			Description: description,
			Messages:    []promptMessage{{Role: "user", Content: content}},
		},
		serverIdentity: serverInfoHostedToolset,
		cacheHints:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize prompts/get result").LogError(ctx, logger)
	}

	return bs, nil
}
