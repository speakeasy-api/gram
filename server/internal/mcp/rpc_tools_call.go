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
	"github.com/speakeasy-api/gram/internal/encryption"
	"github.com/speakeasy-api/gram/internal/environments"
	er "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/instances"
	"github.com/speakeasy-api/gram/internal/mv"
	"github.com/speakeasy-api/gram/internal/oops"
	tr "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/toolsets"
	"go.opentelemetry.io/otel/trace"
)

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolsCall(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Encryption, payload *mcpInputs, req *rawRequest) (json.RawMessage, error) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").Log(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").Log(ctx, logger)
	}

	envRepo := er.New(db)
	toolsRepo := tr.New(db)
	projectID := mv.ProjectID(payload.projectID)
	entries := environments.NewEnvironmentEntries(logger, envRepo, enc)
	toolsetHelpers := toolsets.NewToolsets(db)

	envSlug := payload.environment

	toolID, err := toolsRepo.PokeHTTPToolDefinitionByName(ctx, tr.PokeHTTPToolDefinitionByNameParams{
		ProjectID: uuid.UUID(projectID),
		Name:      params.Name,
	})
	switch {
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load tool").Log(ctx, logger)
	case errors.Is(err, sql.ErrNoRows) || toolID == uuid.Nil:
		return nil, oops.E(oops.CodeNotFound, err, "tool not found").Log(ctx, logger)
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

	if payload.environmentVariables != nil {
		var userVars map[string]string
		if err := json.Unmarshal(payload.environmentVariables, &userVars); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "failed to parse user provided environment variables").Log(ctx, logger)
		}

		// apply user provided env variable overrides
		maps.Copy(envVars, userVars)
	}

	executionPlan, err := toolsetHelpers.GetHTTPToolExecutionInfoByID(ctx, toolID, uuid.UUID(projectID))
	if err != nil {
		return nil, err
	}

	rw := &toolCallResponseWriter{
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}

	err = instances.InstanceToolProxy(ctx, tracer, logger, rw, bytes.NewBuffer(params.Arguments), envVars, executionPlan)
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
