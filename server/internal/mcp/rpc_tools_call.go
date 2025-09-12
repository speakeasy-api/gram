package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	metrics *metrics,
	db *pgxpool.Pool,
	env gateway.EnvironmentLoader,
	payload *mcpInputs,
	req *rawRequest,
	toolProxy *gateway.ToolProxy,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
) (json.RawMessage, error) {
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

	var mcpURL string
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		mcpURL = requestContext.Host + requestContext.ReqURL
		metrics.RecordMCPToolCall(ctx, toolset.OrganizationID, mcpURL, params.Name)
	}

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

		requestBytes := int64(len(params.Arguments))
		outputBytes := int64(len(promptData))

		err = checkToolUsageLimits(ctx, logger, toolset.OrganizationID, toolset.AccountType, billingRepository)
		if err != nil {
			return nil, err
		}

		go billingTracker.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:   toolset.OrganizationID,
			RequestBytes:     requestBytes,
			OutputBytes:      outputBytes,
			ToolID:           higherOrderTool.ID,
			ToolName:         string(higherOrderTool.Name),
			Type:             billing.ToolCallTypeHigherOrder,
			ProjectID:        payload.projectID.String(),
			ToolsetSlug:      &payload.toolset,
			MCPURL:           &mcpURL,
			ProjectSlug:      nil, // This data is only there for debugging, but we don't have it here
			OrganizationSlug: nil,
			ChatID:           nil,
		})

		return formatHigherOrderToolResult(ctx, logger, req, promptData)
	}

	// Transform environment entries into a map
	envVars := make(map[string]string)

	// IMPORTANT: MCP servers accessed in a public manner or not gram authenticated, there is no concept of using stored environments for them
	if envSlug != "" && payload.authenticated {
		storedEnvVars, err := env.Load(ctx, payload.projectID, gateway.Slug(envSlug))
		switch {
		case errors.Is(err, gateway.ErrNotFound):
			return nil, oops.E(oops.CodeBadRequest, err, "environment not found").Log(ctx, logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, logger)
		}

		if len(storedEnvVars) > 0 {
			maps.Copy(envVars, storedEnvVars)
		}
	}

	if len(payload.mcpEnvVariables) > 0 {
		// apply user provided env variable overrides
		maps.Copy(envVars, payload.mcpEnvVariables)
	}

	executionPlan, err := toolsetHelpers.GetHTTPToolExecutionInfoByID(ctx, uuid.MustParse(*toolID), uuid.UUID(projectID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed get tool execution plan").Log(ctx, logger)
	}
	ctx, logger = o11y.EnrichToolCallContext(ctx, logger, executionPlan.OrganizationSlug, executionPlan.ProjectSlug)

	// map provided oauth tokens into the relevant security env variables
	for _, security := range executionPlan.Tool.Security {
		for _, token := range payload.oauthTokenInputs {
			if slices.Contains(security.OAuthTypes, "authorization_code") && (len(token.securityKeys) == 0 || slices.Contains(token.securityKeys, security.Key)) {
				for _, envVar := range security.EnvVariables {
					if strings.HasSuffix(envVar, "ACCESS_TOKEN") {
						envVars[envVar] = token.Token
					}
				}
			}
		}
	}

	rw := &toolCallResponseWriter{
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}

	requestBodyBytes := params.Arguments
	requestBytes := int64(len(requestBodyBytes))
	var outputBytes int64

	err = checkToolUsageLimits(ctx, logger, toolset.OrganizationID, toolset.AccountType, billingRepository)
	if err != nil {
		return nil, err
	}

	defer func() {
		go billingTracker.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:   toolset.OrganizationID,
			RequestBytes:     requestBytes,
			OutputBytes:      outputBytes,
			ToolID:           *toolID,
			ToolName:         params.Name,
			ProjectID:        payload.projectID.String(),
			ProjectSlug:      &executionPlan.ProjectSlug,
			OrganizationSlug: &executionPlan.OrganizationSlug,
			ToolsetSlug:      &payload.toolset,
			MCPURL:           &mcpURL,
			ChatID:           nil,
			Type:             billing.ToolCallTypeHTTP,
		})

	}()

	err = toolProxy.Do(ctx, rw, bytes.NewBuffer(params.Arguments), envVars, executionPlan.Tool)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed execute tool call").Log(ctx, logger)
	}

	// Track tool call usage
	outputBytes = int64(rw.body.Len())
	chunk, err := formatResult(*rw)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed format tool call result").Log(ctx, logger)
	}

	bs, err := json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content: []json.RawMessage{chunk},
			IsError: rw.statusCode < 200 || rw.statusCode >= 300,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize tools/call result").Log(ctx, logger)
	}

	return bs, nil
}

func checkToolUsageLimits(ctx context.Context, logger *slog.Logger, orgID string, accountType string, billingRepository billing.Repository) error {
	if accountType != string(billing.TierFree) {
		return nil
	}

	// we only use cached data here, we do not want to make a call to an external system in the tool call hotpath
	periodUsage, err := billingRepository.GetStoredPeriodUsage(ctx, orgID)
	// we will not fail here right now, but this cache should always be available
	if err != nil {
		logger.ErrorContext(ctx, "failed to get stored period usage", attr.SlogError(err), attr.SlogOrganizationID(orgID))
		return nil
	}

	hardToolCallsLimit := 2 * periodUsage.MaxToolCalls
	if hardToolCallsLimit == 0 {
		hardToolCallsLimit = 2000 // Just in case
	}

	if periodUsage.ToolCalls >= hardToolCallsLimit {
		return oops.E(oops.CodeForbidden, errors.New("tool usage limit reached"), "tool usage limit reached").Log(ctx, logger)
	}

	return nil
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
	n, err := w.body.Write(p)
	if err != nil {
		return n, fmt.Errorf("write response body: %w", err)
	}

	return n, nil
}

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

	bs, err := json.Marshal(result[toolCallResult]{
		ID: req.ID,
		Result: toolCallResult{
			Content: []json.RawMessage{content},
			IsError: false,
		},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to marshal custom tool call result").Log(ctx, logger)
	}

	return bs, nil
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
	case strings.HasPrefix(mt, "text/"), contenttypes.IsJSON(mt), contenttypes.IsYAML(mt):
		bs, err := json.Marshal(contentChunk[string, json.RawMessage]{
			Type:     "text",
			Text:     string(body),
			MimeType: nil,
			Data:     nil,
		})
		if err != nil {
			return nil, fmt.Errorf("serialize text content: %w", err)
		}

		return bs, nil
	case strings.HasPrefix(mt, "image/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		bs, err := json.Marshal(contentChunk[json.RawMessage, string]{
			Type:     "image",
			Data:     encoded,
			MimeType: &mt,
			Text:     nil,
		})
		if err != nil {
			return nil, fmt.Errorf("serialize image content: %w", err)
		}

		return bs, nil
	case strings.HasPrefix(mt, "audio/"):
		encoded := base64.StdEncoding.EncodeToString(body)
		bs, err := json.Marshal(contentChunk[json.RawMessage, string]{
			Type:     "audio",
			Data:     encoded,
			MimeType: &mt,
			Text:     nil,
		})
		if err != nil {
			return nil, fmt.Errorf("serialize audio content: %w", err)
		}

		return bs, nil
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
