package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments"
	er "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/instances"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/internal/toolsets"
	"go.opentelemetry.io/otel/trace"
)

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolsCall(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, metrics *o11y.MetricsHandler, db *pgxpool.Pool, enc *encryption.Encryption, payload *mcpInputs, req *rawRequest, chatClient *openrouter.ChatClient) (json.RawMessage, error) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").Log(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").Log(ctx, logger)
	}

	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)))
	if err != nil {
		return nil, err
	}

	envRepo := er.New(db)
	entries := environments.NewEnvironmentEntries(logger, envRepo, enc)
	toolsetHelpers := toolsets.NewToolsets(db)
	envSlug := payload.environment
	var higherOrderTool *types.PromptTemplate
	var toolID *string

	for _, tool := range toolset.HTTPTools {
		if tool.Name == params.Name {
			toolID = &tool.ID
			break
		}
	}

	if toolID == nil {
		for _, prompt := range toolset.PromptTemplates {
			if string(prompt.Name) == params.Name {
				higherOrderTool = prompt
				break
			}
		}
	}

	if higherOrderTool == nil && toolID == nil {
		return nil, oops.E(oops.CodeNotFound, errors.New("tool not found"), "tool not found").Log(ctx, logger)
	}

	if higherOrderTool != nil {
		var args map[string]any
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse higher order tool arguments").Log(ctx, logger)
		}

		promptData, err := executePrompt(higherOrderTool.Engine, higherOrderTool.Prompt, args)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to execute prompt").Log(ctx, logger)
		}

		return formatHigherOrderToolResult(ctx, logger, req, promptData)
	}

	// Transform environment entries into a map
	envVars := make(map[string]string)
	if envSlug != "" {
		envModel, err := envRepo.GetEnvironmentBySlug(ctx, er.GetEnvironmentBySlugParams{
			ProjectID: uuid.UUID(projectID),
			Slug:      strings.ToLower(envSlug),
		})
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return nil, oops.E(oops.CodeBadRequest, err, "environment not found").Log(ctx, logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, logger)
		}

		environmentEntries, err := entries.ListEnvironmentEntries(ctx, envModel.ID, false)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment entries").Log(ctx, logger)
		}

		for _, entry := range environmentEntries {
			envVars[entry.Name] = entry.Value
		}
	}

	if len(payload.mcpEnvVariables) > 0 {
		// apply user provided env variable overrides
		maps.Copy(envVars, payload.mcpEnvVariables)
	}

	executionPlan, err := toolsetHelpers.GetHTTPToolExecutionInfoByID(ctx, uuid.MustParse(*toolID), uuid.UUID(projectID))
	if err != nil {
		return nil, err
	}

	rw := &toolCallResponseWriter{
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}

	err = instances.InstanceToolProxy(ctx, tracer, logger, metrics, rw, bytes.NewBuffer(params.Arguments), envVars, executionPlan, instances.ToolCallSourceMCP, chatClient)
	if err != nil {
		return nil, err
	}

	chunk, err := formatResult(*rw)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to format tool call result").Log(ctx, logger)
	}

	return json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: rw.statusCode < 200 || rw.statusCode >= 300,
		},
	})
}

type toolCallResponseWriter struct {
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (w *toolCallResponseWriter) Header() http.Header {
	return w.headers
}

func (w *toolCallResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *toolCallResponseWriter) Write(p []byte) (int, error) {
	return w.body.Write(p)
}

var jsonRE = regexp.MustCompile(`\bjson\b`)
var yamlRE = regexp.MustCompile(`\byaml\b`)

func formatHigherOrderToolResult(ctx context.Context, logger *slog.Logger, req *rawRequest, promptData string) (json.RawMessage, error) {
	content, err := json.Marshal(contentChunk[string, json.RawMessage]{
		Type:     "text",
		Text:     promptData,
		MimeType: nil,
		Data:     nil,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal content chunk").Log(ctx, logger)
	}

	return json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content: []json.RawMessage{content},
			IsError: false,
		},
	})
}

func formatResult(rw toolCallResponseWriter) (json.RawMessage, error) {
	body := rw.body.Bytes()
	if len(body) == 0 {
		return nil, nil
	}

	ct := rw.headers.Get("content-type")
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return nil, fmt.Errorf("failed to parse content type %q: %w", ct, err)
	}

	switch {
	case strings.HasPrefix(mt, "text/"), jsonRE.MatchString(mt), yamlRE.MatchString(mt):
		return json.Marshal(contentChunk[string, json.RawMessage]{
			Type:     "text",
			Text:     string(body),
			MimeType: nil,
			Data:     nil,
		})
	case strings.HasPrefix(mt, "image/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		return json.Marshal(contentChunk[json.RawMessage, string]{
			Type:     "image",
			Data:     encoded,
			MimeType: &mt,
			Text:     nil,
		})
	case strings.HasPrefix(mt, "audio/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		return json.Marshal(contentChunk[json.RawMessage, string]{
			Type:     "audio",
			Data:     encoded,
			MimeType: &mt,
			Text:     nil,
		})
	default:
		return nil, fmt.Errorf("unsupported content type %q", ct)
	}
}

type contentChunk[T any, D any] struct {
	Type     string  `json:"type"`
	MimeType *string `json:"mimeType,omitempty"`
	Text     T       `json:"text,omitempty"`
	Data     D       `json:"data,omitempty"`
}

type toolCallResult struct {
	Content []json.RawMessage `json:"content"`
	IsError bool              `json:"isError,omitzero"`
}
