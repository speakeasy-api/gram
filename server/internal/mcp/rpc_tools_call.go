package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contenttypes"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/rag"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/urn"
	temporal_client "go.temporal.io/sdk/client"
)

type toolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

const (
	listToolsToolName     = "list_tools"
	describeToolsToolName = "describe_tools"
	executeToolToolName   = "execute_tool"
)

func handleToolsCall(
	ctx context.Context,
	logger *slog.Logger,
	metrics *metrics,
	db *pgxpool.Pool,
	env toolconfig.EnvironmentLoader,
	payload *mcpInputs,
	req *rawRequest,
	toolProxy *gateway.ToolProxy,
	billingTracker billing.Tracker,
	billingRepository billing.Repository,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	telemSvc *tm.Service,
	featuresClient *productfeatures.Client,
	vectorToolStore *rag.ToolsetVectorStore,
	temporal temporal_client.Client,
) (json.RawMessage, error) {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "failed to parse tool call request").Log(ctx, logger)
	}

	if params.Name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "tool name is required").Log(ctx, logger)
	}

	projectID := mv.ProjectID(payload.projectID)

	toolset, err := mv.DescribeToolset(ctx, logger, db, projectID, mv.ToolsetSlug(conv.ToLower(payload.toolset)), toolsetCache)
	if err != nil {
		return nil, err
	}

	if payload.mode != ToolModeStatic {
		switch params.Name {
		case searchToolsToolName:
			return handleSearchToolsCall(ctx, logger, req.ID, params.Arguments, toolset, vectorToolStore, temporal)
		case describeToolsToolName:
			return handleDescribeToolsCall(ctx, logger, req.ID, params.Arguments, toolset)
		case executeToolToolName:
			proxyName, proxyArgs, err := processExecuteToolCall(ctx, logger, params.Arguments)
			if err != nil {
				return nil, err
			}

			// TODO: we would want some way in metrics/logging/billing to track this is a dynamic tool call
			params.Name = proxyName
			params.Arguments = proxyArgs
		}
	}

	var mcpURL string
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		mcpURL = requestContext.Host + requestContext.ReqURL
		metrics.RecordMCPToolCall(ctx, toolset.OrganizationID, mcpURL, params.Name)
	}

	toolsetHelpers := toolsets.NewToolsets(db)

	toolsetID, err := uuid.Parse(toolset.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "invalid toolset ID").Log(ctx, logger)
	}

	executor := externalmcp.BuildProxyToolExecutor(logger, toolset.Tools)

	var plan *gateway.ToolCallPlan
	var toolURN urn.Tool

	// Try proxy tool match first - captures full plan via closure
	var matchedPlan *gateway.ToolCallPlan
	resolve := func(ctx context.Context, u urn.Tool, pid uuid.UUID) (*externalmcp.ToolCallPlan, error) {
		fullPlan, err := toolsetHelpers.GetToolCallPlanByURN(ctx, u, pid)
		if err != nil {
			return nil, fmt.Errorf("get tool call plan by URN: %w", err)
		}
		matchedPlan = fullPlan
		return fullPlan.ExternalMCP, nil
	}

	planInputs, err := executor.MatchPlanInputs(ctx, params.Name, uuid.UUID(projectID), resolve)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to match proxy tool").Log(ctx, logger)
	}

	var tool *types.Tool

	if planInputs != nil {
		// Matched a proxy tool - use captured plan with updated tool name
		matchedPlan.ExternalMCP.ToolName = planInputs.ToolName
		plan = matchedPlan
		toolURN = plan.Descriptor.URN
	} else {
		// Fall through to materialized tool handling
		for _, t := range toolset.Tools {
			if conv.IsProxyTool(t) {
				continue
			}

			baseTool, err := conv.ToBaseTool(t)
			if err != nil {
				continue
			}
			if baseTool.Name == params.Name {
				tool = t
				break
			}
		}

		if tool == nil {
			return nil, oops.E(oops.CodeNotFound, errors.New("tool not found"), "tool not found").Log(ctx, logger)
		}

		urn, err := conv.GetToolURN(*tool)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get tool urn").Log(ctx, logger)
		}
		toolURN = *urn

		plan, err = toolsetHelpers.GetToolCallPlanByURN(ctx, toolURN, uuid.UUID(projectID))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed get tool call plan").Log(ctx, logger)
		}
	}

	userConfig, err := resolveUserConfiguration(ctx, logger, env, payload, plan)
	if err != nil {
		return nil, err
	}

	systemConfig, err := env.LoadSystemEnv(ctx, payload.projectID, toolsetID, string(toolURN.Kind), toolURN.Source)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load system environment").Log(ctx, logger)
	}

	descriptor := plan.Descriptor

	ctx, logger = o11y.EnrichToolCallContext(ctx, logger, descriptor.OrganizationSlug, descriptor.ProjectSlug)

	rw := &toolCallResponseWriter{
		headers:    make(http.Header),
		body:       new(bytes.Buffer),
		statusCode: http.StatusOK,
	}

	requestBodyBytes := params.Arguments
	requestBytes := int64(len(requestBodyBytes))
	var outputBytes int64
	var functionCPU *float64
	var functionMem *float64
	var functionsExecutionTime *float64

	err = checkToolUsageLimits(ctx, logger, toolset.OrganizationID, toolset.AccountType, billingRepository)
	if err != nil {
		return nil, err
	}

	logAttrs := tm.HTTPLogAttributes{}
	defer func() {
		go billingTracker.TrackToolCallUsage(context.WithoutCancel(ctx), billing.ToolCallUsageEvent{
			OrganizationID:        toolset.OrganizationID,
			RequestBytes:          requestBytes,
			OutputBytes:           outputBytes,
			ToolURN:               toolURN.String(),
			ToolName:              params.Name,
			ProjectID:             payload.projectID.String(),
			ProjectSlug:           &descriptor.ProjectSlug,
			OrganizationSlug:      &descriptor.OrganizationSlug,
			ToolsetSlug:           &payload.toolset,
			ToolsetID:             &toolset.ID,
			ResponseStatusCode:    rw.statusCode,
			MCPURL:                &mcpURL,
			MCPSessionID:          &payload.sessionID,
			ChatID:                nil,
			Type:                  plan.BillingType,
			ResourceURI:           "",
			FunctionCPUUsage:      functionCPU,
			FunctionMemUsage:      functionMem,
			FunctionExecutionTime: functionsExecutionTime,
		})

		logAttrs.RecordStatusCode(rw.statusCode)
		logAttrs.RecordRequestBody(requestBytes)
		logAttrs.RecordResponseBody(outputBytes)
		params := tm.LogParams{
			Timestamp: time.Now(),
			ToolInfo: tm.ToolInfo{
				ID:             descriptor.ID,
				URN:            descriptor.URN.String(),
				Name:           descriptor.Name,
				ProjectID:      descriptor.ProjectID,
				DeploymentID:   descriptor.DeploymentID,
				OrganizationID: descriptor.OrganizationID,
				FunctionID:     nil,
			},
			Attributes: logAttrs,
		}
		telemSvc.CreateLog(params)
	}()

	// Extract OAuth token for external MCP servers (token with no security keys = general token)
	var oauthToken string
	for _, t := range payload.oauthTokenInputs {
		if len(t.securityKeys) == 0 && t.Token != "" {
			oauthToken = t.Token
			break
		}
	}

	err = toolProxy.Do(ctx, rw, bytes.NewBuffer(params.Arguments), toolconfig.ToolCallEnv{
		UserConfig: userConfig, SystemEnv: systemConfig, OAuthToken: oauthToken}, plan, logAttrs)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed execute tool call").Log(ctx, logger)
	}

	outputBytes = int64(rw.body.Len())

	// Extract function metrics from headers (originally trailers from functions runner)
	if cpuStr := rw.headers.Get(functions.FunctionsCPUHeader); cpuStr != "" {
		if cpu, err := strconv.ParseFloat(cpuStr, 64); err == nil {
			functionCPU = &cpu
		}
	}
	if memStr := rw.headers.Get(functions.FunctionsMemoryHeader); memStr != "" {
		if mem, err := strconv.ParseFloat(memStr, 64); err == nil {
			functionMem = &mem
		}
	}
	if execTimeStr := rw.headers.Get(functions.FunctionsExecutionTimeHeader); execTimeStr != "" {
		if execTime, err := strconv.ParseFloat(execTimeStr, 64); err == nil {
			functionsExecutionTime = &execTime
		}
	}

	var meta map[string]any
	if tool != nil && tool.FunctionToolDefinition != nil {
		meta = tool.FunctionToolDefinition.Meta
	}

	// External MCP tools and MCP passthrough tools already return properly formatted responses
	if plan.Kind == gateway.ToolKindExternalMCP || isMCPPassthrough(meta) {
		bs, err := json.Marshal(result[json.RawMessage]{
			ID:     req.ID,
			Result: json.RawMessage(rw.body.Bytes()),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize MCP result").Log(ctx, logger)
		}

		return bs, nil
	}

	chunk, err := formatResult(*rw, plan.Kind)
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

func resolveUserConfiguration(
	ctx context.Context,
	logger *slog.Logger,
	env toolconfig.EnvironmentLoader,
	payload *mcpInputs,
	plan *gateway.ToolCallPlan,
) (*toolconfig.CaseInsensitiveEnv, error) {
	userConfig := toolconfig.NewCaseInsensitiveEnv()

	// IMPORTANT: we must only attach gram environments to authenticated payloads. Gram environments contain
	// secrets owned by Gram projects and should not be usable by public clients
	if payload.environment != "" && payload.authenticated {
		storedEnvVars, err := env.Load(ctx, payload.projectID, toolconfig.Slug(payload.environment))
		switch {
		case errors.Is(err, toolconfig.ErrNotFound):
			return nil, oops.E(oops.CodeBadRequest, err, "environment not found").Log(ctx, logger)
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "failed to load environment").Log(ctx, logger)
		}

		for k, v := range storedEnvVars {
			userConfig.Set(k, v)
		}
	}

	for k, v := range payload.mcpEnvVariables {
		userConfig.Set(k, v)
	}

	// Process OAuth tokens for HTTP tools
	if plan != nil && plan.Kind == gateway.ToolKindHTTP {
		for _, security := range plan.HTTP.Security {
			for _, token := range payload.oauthTokenInputs {
				if (slices.Contains(security.OAuthTypes, "authorization_code") || security.Type.Value == "openIdConnect") && (len(token.securityKeys) == 0 || slices.Contains(token.securityKeys, security.Key)) {
					for _, envVar := range security.EnvVariables {
						if strings.HasSuffix(envVar, "ACCESS_TOKEN") {
							userConfig.Set(envVar, token.Token)
						}
					}
				}
			}
		}
	}

	// Process OAuth tokens for Function tools
	if plan != nil && plan.Kind == gateway.ToolKindFunction && plan.Function.AuthInput != nil {
		for _, token := range payload.oauthTokenInputs {
			if plan.Function.AuthInput.Type == "oauth2" {
				userConfig.Set(plan.Function.AuthInput.Variable, token.Token)
			}
		}
	}

	return userConfig, nil
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

var dynamicExecuteToolSchema = json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Exact name of the tool to execute."
			},
			"arguments": {
				"description": "JSON payload to forward to the tool as its arguments."
			}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)

type executeToolArguments struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func processExecuteToolCall(ctx context.Context, logger *slog.Logger, argsRaw json.RawMessage) (string, json.RawMessage, error) {
	var args executeToolArguments
	if len(argsRaw) == 0 {
		return "", nil, oops.E(oops.CodeInvalid, errors.New("missing execute arguments"), "execute_tool arguments are required").Log(ctx, logger)
	}
	if err := json.Unmarshal(argsRaw, &args); err != nil {
		return "", nil, oops.E(oops.CodeBadRequest, err, "failed to parse execute_tool arguments").Log(ctx, logger)
	}

	name := strings.TrimSpace(args.Name)
	if name == "" {
		return "", nil, oops.E(oops.CodeInvalid, errors.New("missing tool name"), "name is required for execute_tool").Log(ctx, logger)
	}

	payload := args.Arguments
	if len(payload) != 0 {
		trimmed := bytes.TrimSpace(payload)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			var payloadString string
			if err := json.Unmarshal(payload, &payloadString); err != nil {
				return "", nil, oops.E(oops.CodeBadRequest, err, "failed to parse execute_tool payload").Log(ctx, logger)
			}
			payload = json.RawMessage(payloadString)
		}
		if !json.Valid(payload) {
			return "", nil, oops.E(oops.CodeBadRequest, errors.New("invalid payload"), "arguments must be valid JSON").Log(ctx, logger)
		}
	}

	return name, payload, nil
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

func formatResult(rw toolCallResponseWriter, toolKind gateway.ToolKind) (json.RawMessage, error) {
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
			Meta: map[string]any{
				MetaGramMimeType: mt,
			},
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
			Meta:     nil,
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
			Meta:     nil,
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
	Type     string         `json:"type"`
	MimeType *string        `json:"mimeType,omitempty"`
	Text     T              `json:"text,omitempty"`
	Data     D              `json:"data,omitempty"`
	Meta     map[string]any `json:"_meta,omitempty,omitzero"`
}

type toolCallResult struct {
	Content []json.RawMessage `json:"content"`
	IsError bool              `json:"isError,omitzero"`
}
