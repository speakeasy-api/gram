package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/templates"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/serialization"
)

type ToolCallSource string

const (
	ToolCallSourceDirect ToolCallSource = "direct"
	ToolCallSourceMCP    ToolCallSource = "mcp"
)

var proxiedHeaders = []string{
	"Cache-Control",
	"Content-Language",
	"Content-Length",
	"Content-Type",
	"Expires",
	"Last-Modified",
	"Pragma",
	functions.FunctionsCPUHeader,
	functions.FunctionsMemoryHeader,
	functions.FunctionsExecutionTimeHeader,
}

type FilterRequest struct {
	Type   string `json:"type"`
	Filter string `json:"filter"`
}

type ToolCallBody struct {
	PathParameters       map[string]any    `json:"pathParameters"`
	QueryParameters      map[string]any    `json:"queryParameters"`
	HeaderParameters     map[string]any    `json:"headerParameters"`
	Body                 json.RawMessage   `json:"body"`
	ResponseFilter       *FilterRequest    `json:"responseFilter"`
	EnvironmentVariables map[string]string `json:"environmentVariables"`
	GramRequestSummary   string            `json:"gram-request-summary"`
}

type toolcallErrorSchema struct {
	Error string `json:"error"`
}

type InstanceToolProxyConfig struct {
	Source ToolCallSource
	Logger *slog.Logger
	Tracer trace.Tracer
	Meter  metric.Meter
	Cache  cache.Cache
}

type ToolProxy struct {
	source     ToolCallSource
	logger     *slog.Logger
	tracer     trace.Tracer
	metrics    *metrics
	encryption *encryption.Client
	cache      cache.Cache
	policy     *guardian.Policy
	functions  functions.ToolCaller
}

func NewToolProxy(
	logger *slog.Logger,
	tracerProivder trace.TracerProvider,
	meterProvider metric.MeterProvider,
	source ToolCallSource,
	enc *encryption.Client,
	cache cache.Cache,
	policy *guardian.Policy,
	funcCaller functions.ToolCaller,
) *ToolProxy {
	tracer := tracerProivder.Tracer("github.com/speakeasy-api/gram/server/internal/gateway")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/gateway")

	return &ToolProxy{
		source:     source,
		logger:     logger,
		tracer:     tracer,
		metrics:    newMetrics(meter, logger),
		encryption: enc,
		cache:      cache,
		policy:     policy,
		functions:  funcCaller,
	}
}

func (tp *ToolProxy) Do(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	plan *ToolCallPlan,
	attrs tm.HTTPLogAttributes,
) (err error) {
	ctx, span := tp.tracer.Start(ctx, "gateway.toolCall", trace.WithAttributes(
		attr.ToolName(plan.Descriptor.Name),
		attr.ToolID(plan.Descriptor.ID),
		attr.ProjectID(plan.Descriptor.ProjectID),
		attr.DeploymentID(plan.Descriptor.DeploymentID),
		attr.ToolCallSource(string(tp.source)),
	))
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	logger := tp.logger.With(
		attr.SlogProjectID(plan.Descriptor.ProjectID),
		attr.SlogDeploymentID(plan.Descriptor.DeploymentID),
		attr.SlogToolID(plan.Descriptor.ID),
		attr.SlogToolName(plan.Descriptor.Name),
		attr.SlogToolCallSource(string(tp.source)),
	)

	switch plan.Kind {
	case "":
		return oops.E(oops.CodeInvariantViolation, nil, "tool kind is not set").Log(ctx, tp.logger)
	case ToolKindFunction:
		return tp.doFunction(ctx, logger, w, requestBody, env, plan.Descriptor, plan.Function, attrs)
	case ToolKindHTTP:
		return tp.doHTTP(ctx, logger, w, requestBody, env, plan.Descriptor, plan.HTTP, attrs)
	case ToolKindPrompt:
		return tp.doPrompt(ctx, logger, w, requestBody, env, plan.Descriptor, plan.Prompt)
	case ToolKindExternalMCP:
		return tp.doExternalMCP(ctx, logger, w, requestBody, env, plan.ExternalMCP)
	default:
		return fmt.Errorf("tool type not supported: %s", plan.Kind)
	}
}

func (tp *ToolProxy) doFunction(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	descriptor *ToolDescriptor,
	plan *FunctionToolCallPlan,
	attrs tm.HTTPLogAttributes,
) error {
	span := trace.SpanFromContext(ctx)
	invocationID, err := uuid.NewV7()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to generate function invocation ID").Log(ctx, logger)
	}

	projectID, err := uuid.Parse(descriptor.ProjectID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid project id received for function tool call").Log(ctx, logger)
	}
	deploymentID, err := uuid.Parse(descriptor.DeploymentID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid deployment id received for function tool call").Log(ctx, logger)
	}
	functionID, err := uuid.Parse(plan.FunctionID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function id received for function tool call").Log(ctx, logger)
	}
	accessID, err := uuid.Parse(plan.FunctionsAccessID)
	if err != nil {
		return oops.E(oops.CodeInvariantViolation, err, "invalid function access id received for function tool call").Log(ctx, logger)
	}

	var input json.RawMessage
	if err := json.NewDecoder(requestBody).Decode(&input); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, logger)
	}

	payloadEnv := make(map[string]string)

	// Start with system environment variables (uppercase keys)
	for k, v := range env.SystemEnv.All() {
		payloadEnv[strings.ToUpper(k)] = v
	}

	// For each variable required by the function, allow user config to merge/override
	for key := range plan.Variables {
		if val := env.UserConfig.Get(key); val != "" {
			payloadEnv[key] = val
		}
	}

	if plan.AuthInput != nil {
		if val := env.UserConfig.Get(plan.AuthInput.Variable); val != "" {
			payloadEnv[plan.AuthInput.Variable] = val
		}
	}

	req, err := tp.functions.ToolCall(ctx, functions.RunnerToolCallRequest{
		RunnerBaseRequest: functions.RunnerBaseRequest{
			InvocationID:      invocationID,
			OrganizationID:    descriptor.OrganizationID,
			OrganizationSlug:  descriptor.OrganizationSlug,
			ProjectID:         projectID,
			ProjectSlug:       descriptor.ProjectSlug,
			DeploymentID:      deploymentID,
			FunctionsID:       functionID,
			FunctionsAccessID: accessID,
			Input:             input,
			Environment:       payloadEnv,
		},
		ToolURN:  descriptor.URN,
		ToolName: descriptor.Name,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to create function tool call request").Log(ctx, logger)
	}

	var responseStatusCode int
	defer func() {
		rawct := w.Header().Get("content-type")
		ct, _, err := mime.ParseMediaType(rawct)
		if err != nil {
			ct = rawct
		}
		ct = ct[:min(len(ct), 100)]

		logger.InfoContext(ctx, "function tool call",
			attr.SlogHTTPResponseStatusCode(responseStatusCode),
			attr.SlogHTTPRequestMethod(req.Method),
			attr.SlogHTTPResponseHeaderContentType(ct),
		)
		// Record metrics for the tool call, some cardinality is introduced with org and tool name we will keep an eye on it
		tp.metrics.RecordToolCall(ctx, descriptor.OrganizationID, descriptor.URN, responseStatusCode)

		span.SetAttributes(attr.HTTPResponseStatusCode(responseStatusCode))
	}()

	return reverseProxyRequest(ctx, ReverseProxyOptions{
		Logger:                    logger,
		Tracer:                    tp.tracer,
		Writer:                    w,
		Request:                   req,
		URN:                       descriptor.URN.String(),
		Expression:                &FilterRequest{Type: "none", Filter: ""},
		FilterConfig:              DisableResponseFiltering,
		Policy:                    tp.policy,
		ResponseStatusCodeCapture: &responseStatusCode,
		Attributes:                attrs,
		VerifyResponse: func(resp *http.Response) error {
			if resp.Header.Get("Gram-Invoke-ID") != invocationID.String() {
				return fmt.Errorf("failed to verify function invocation ID")
			}
			return nil
		},
		ID:               descriptor.ID,
		Name:             descriptor.Name,
		DeploymentID:     descriptor.DeploymentID,
		ProjectID:        descriptor.ProjectID,
		ProjectSlug:      descriptor.ProjectSlug,
		OrganizationID:   descriptor.OrganizationID,
		OrganizationSlug: descriptor.OrganizationSlug,
	})
}

func (tp *ToolProxy) doHTTP(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	descriptor *ToolDescriptor,
	plan *HTTPToolCallPlan,
	attrRecorder tm.HTTPLogAttributes,
) error {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attr.HTTPRoute(plan.Path), // this is just from the raw OpenAPI spec. It is not a path with any parameters filled in, so not identifiable.
	)
	logger = logger.With(
		attr.SlogHTTPRoute(plan.Path), // this is just from the raw OpenAPI spec. It is not a path with any parameters filled in, so not identifiable.
	)

	// set these values in case we hit an early validation error
	attrRecorder.RecordMethod(plan.Method)
	attrRecorder.RecordRoute(plan.Path)

	// Variable to capture status code for metrics
	var responseStatusCode int
	defer func() {
		logger.InfoContext(ctx, "http tool call",
			attr.SlogHTTPResponseStatusCode(responseStatusCode),
			attr.SlogHTTPRequestMethod(plan.Method),
			attr.SlogHTTPResponseHeaderContentType(plan.RequestContentType.Value),
		)
		// Record metrics for the tool call, some cardinality is introduced with org and tool name we will keep an eye on it
		tp.metrics.RecordToolCall(ctx, descriptor.OrganizationID, descriptor.URN, responseStatusCode)

		span.SetAttributes(attr.HTTPResponseStatusCode(responseStatusCode))
	}()

	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, logger)
	}

	var toolCallBody ToolCallBody
	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	// We use json.Number for accurate decoding in path, query, and header parameters.
	dec.UseNumber()

	if err := dec.Decode(&toolCallBody); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request body").Log(ctx, logger)
	}

	if len(plan.Schema) > 0 {
		bodyBytes, err = validateAndAttemptHealing(ctx, logger, bodyBytes, string(plan.Schema))

		// Extract the body field from healed bodyBytes
		var healedToolCallBody ToolCallBody
		if unmarshalErr := json.Unmarshal(bodyBytes, &healedToolCallBody); unmarshalErr == nil {
			toolCallBody.Body = healedToolCallBody.Body
		}

		// If still invalid after healing attempt, return error
		if err != nil {
			logger.InfoContext(ctx, "tool call request schema failed validation", attr.SlogError(err))
			responseStatusCode = http.StatusBadRequest
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(toolcallErrorSchema{
				Error: fmt.Sprintf("The input to the tool is invalid with the attached error. Please review the tool schema closely: %s", err.Error()),
			}); err != nil {
				logger.ErrorContext(ctx, "failed to encode tool call error", attr.SlogError(err))
			}
			return nil
		}
	}

	// We prefer tool call specified arguments over user-specified config
	if toolCallBody.EnvironmentVariables != nil {
		for k, v := range toolCallBody.EnvironmentVariables {
			env.UserConfig.Set(k, v)
		}
	}

	// Handle path parameters
	requestPath := plan.Path
	if toolCallBody.PathParameters != nil {
		pathParams := make(map[string]string)
		for name, value := range toolCallBody.PathParameters {
			param := plan.PathParams[name]
			var settings *serialization.HTTPParameter
			if param == nil {
				logger.WarnContext(ctx, "no parameter settings found for path parameter", attr.SlogHTTPParamName(name))
			} else {
				settings = &serialization.HTTPParameter{
					Name:            param.Name,
					Style:           param.Style,
					Explode:         param.Explode,
					AllowEmptyValue: param.AllowEmptyValue,
				}
			}
			// style: simple and explode: false is the default serialization for path parameters
			params := serialization.ParsePathAndHeaderParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), settings)
			if params != nil && params[name] != "" {
				pathParams[name] = params[name]
			} else {
				logger.ErrorContext(ctx, "failed to parse path parameter", attr.SlogHTTPParamName(name))
			}
		}
		urlStr := insertPathParams(requestPath, pathParams)
		parsedURL, parseErr := url.Parse(urlStr)
		if parseErr != nil {
			return oops.E(oops.CodeUnexpected, parseErr, "failed to parse URL with path parameters").Log(ctx, logger)
		}
		requestPath = parsedURL.String()
	}

	// Get the server URL from the tool definition
	var serverURL string
	if plan.DefaultServerUrl.Valid {
		serverURL = plan.DefaultServerUrl.Value
	}

	if envServerURL := processServerEnvVars(ctx, logger, plan, env); envServerURL != "" {
		serverURL = envServerURL
	}

	if serverURL == "" {
		logger.ErrorContext(ctx, "no server URL provided for tool", attr.SlogToolName(descriptor.Name))
		return oops.E(oops.CodeInvalid, nil, "no server URL provided for tool").Log(ctx, logger)
	}

	fullURL, err := url.JoinPath(serverURL, requestPath)
	var urlErr *url.Error
	switch {
	case errors.As(err, &urlErr) && urlErr.Err != nil:
		return oops.E(oops.CodeInvalid, err, "error parsing server url: %s", urlErr.Err.Error()).Log(ctx, logger)
	case err != nil:
		// we do not want to print the full err here because it may leak the server URL which can contain
		// sensitive information like usernames, passwords, etc.
		return oops.E(oops.CodeInvalid, err, "error parsing server url").Log(ctx, logger)
	}

	var req *http.Request
	if strings.HasPrefix(plan.RequestContentType.Value, "application/x-www-form-urlencoded") {
		encoded := ""
		if len(toolCallBody.Body) > 0 {
			// Assume toolCallBody.Body is a JSON object (map[string]interface{})
			var formMap map[string]interface{}
			if err := json.Unmarshal(toolCallBody.Body, &formMap); err != nil {
				return oops.E(oops.CodeBadRequest, err, "failed to parse form body").Log(ctx, logger)
			}
			values := url.Values{}
			for k, v := range formMap {
				formEncodeValue(values, k, v)
			}
			encoded = values.Encode()
		}
		req, err = http.NewRequestWithContext(
			ctx,
			plan.Method,
			fullURL,
			strings.NewReader(encoded),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build url-encoded request").Log(ctx, logger)
		}
		req.Header.Set("Content-Type", plan.RequestContentType.Value)
	} else {
		req, err = http.NewRequestWithContext(
			ctx,
			plan.Method,
			fullURL,
			bytes.NewReader(toolCallBody.Body),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build json request").Log(ctx, logger)
		}
		if plan.RequestContentType.Value != "" {
			req.Header.Set("Content-Type", plan.RequestContentType.Value)
		}
	}

	if toolCallBody.QueryParameters != nil {
		values := url.Values{}
		for name, value := range toolCallBody.QueryParameters {
			param := plan.QueryParams[name]
			var settings *serialization.HTTPParameter
			if param == nil {
				logger.WarnContext(ctx, "no parameter settings found for query parameter", attr.SlogHTTPParamName(name))
			} else {
				settings = &serialization.HTTPParameter{
					Name:            param.Name,
					Style:           param.Style,
					Explode:         param.Explode,
					AllowEmptyValue: param.AllowEmptyValue,
				}
			}
			// style: form and explode: true with , delimiter is default for query parameters
			params := serialization.ParseQueryParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), settings)
			if len(params) > 0 {
				for name, value := range params {
					for _, vv := range value {
						values.Add(name, vv)
					}
				}
			} else {
				logger.ErrorContext(ctx, "failed to parse query parameter", attr.SlogHTTPParamName(name))
			}
		}
		req.URL.RawQuery = values.Encode()
	}

	// Handle header parameters (non security schemes)
	if toolCallBody.HeaderParameters != nil {
		for name, value := range toolCallBody.HeaderParameters {
			param := plan.HeaderParams[name]
			var settings *serialization.HTTPParameter
			if param == nil {
				logger.WarnContext(ctx, "no parameter settings found for header parameter", attr.SlogHTTPParamName(name))
			} else {
				settings = &serialization.HTTPParameter{
					Name:            param.Name,
					Style:           param.Style,
					Explode:         param.Explode,
					AllowEmptyValue: param.AllowEmptyValue,
				}
			}
			// style: simple and explode: false is the default serialization for headers
			params := serialization.ParsePathAndHeaderParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), settings)
			if params != nil && params[name] != "" {
				req.Header.Set(name, params[name])
			} else {
				logger.ErrorContext(ctx, "failed to parse header parameter", attr.SlogHTTPParamName(name))
			}
		}
	}

	shouldContinue := processSecurity(ctx, logger, req, w, &responseStatusCode, descriptor, plan, tp.cache, env, serverURL, attrRecorder)
	if !shouldContinue {
		return nil
	}

	req.Header.Set("X-Gram-Proxy", "1")

	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	return reverseProxyRequest(ctx, ReverseProxyOptions{
		Logger:                    logger,
		Tracer:                    tp.tracer,
		Writer:                    w,
		Request:                   req,
		URN:                       descriptor.URN.String(),
		Expression:                toolCallBody.ResponseFilter,
		FilterConfig:              plan.ResponseFilter,
		Policy:                    tp.policy,
		ResponseStatusCodeCapture: &responseStatusCode,
		Attributes:                attrRecorder,
		VerifyResponse:            func(resp *http.Response) error { return nil },
		ID:                        descriptor.ID,
		Name:                      descriptor.Name,
		DeploymentID:              descriptor.DeploymentID,
		ProjectID:                 descriptor.ProjectID,
		ProjectSlug:               descriptor.ProjectSlug,
		OrganizationID:            descriptor.OrganizationID,
		OrganizationSlug:          descriptor.OrganizationSlug,
	})
}

type promptGetParams struct {
	Arguments map[string]any `json:"arguments"`
}

func (tp *ToolProxy) doPrompt(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, requestBody io.Reader, env ToolCallEnv, descriptor *ToolDescriptor, plan *PromptToolCallPlan) error {
	var params promptGetParams
	if err := json.NewDecoder(requestBody).Decode(&params); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse get prompt request").Log(ctx, logger)
	}

	promptData, err := templates.RenderTemplate(ctx, logger, plan.Prompt, plan.Kind, plan.Engine, params.Arguments)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to render template").Log(ctx, logger)
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(promptData)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write prompt data").Log(ctx, logger)
	}

	return nil
}

// ExternalMCPOAuthTokenKey is the key used to pass OAuth tokens for external MCP servers via ToolCallEnv.
const ExternalMCPOAuthTokenKey = "externalmcp_oauth" //nolint:gosec // not a credential, just a key name

func (tp *ToolProxy) doExternalMCP(
	ctx context.Context,
	logger *slog.Logger,
	w http.ResponseWriter,
	requestBody io.Reader,
	env ToolCallEnv,
	plan *ExternalMCPToolCallPlan,
) error {
	// Read the request body (tool arguments)
	arguments, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read tool arguments").Log(ctx, logger)
	}

	// Build client options with OAuth token if required and available
	var opts *externalmcp.ClientOptions
	if plan.RequiresOAuth {
		if token := env.UserConfig.Get(ExternalMCPOAuthTokenKey); token != "" {
			opts = &externalmcp.ClientOptions{
				Authorization: "Bearer " + token,
				TransportType: plan.TransportType,
			}
		}
	}

	// Create client and call tool
	client, err := externalmcp.NewClient(ctx, logger, plan.RemoteURL, plan.TransportType, opts)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to connect to external MCP server").Log(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, client.Close)

	callResult, err := client.CallTool(ctx, plan.ToolName, arguments)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to call external MCP tool").Log(ctx, logger)
	}

	// Write the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := struct {
		Content []json.RawMessage `json:"content"`
		IsError bool              `json:"isError,omitzero"`
	}{
		Content: callResult.Content,
		IsError: callResult.IsError,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to write external MCP tool result").Log(ctx, logger)
	}

	return nil
}

type retryConfig struct {
	initialInterval time.Duration
	maxInterval     time.Duration
	maxAttempts     int
	backoffFactor   float64
	statusCodes     []int    // HTTP status codes to retry on
	methods         []string // HTTP methods to retry on
}

func retryWithBackoff(
	ctx context.Context,
	retryBackoff retryConfig,
	doRequest func() (*http.Response, error),
) (*http.Response, error) {
	var resp *http.Response
	var err error
	delayInterval := retryBackoff.initialInterval
	for attempt := 0; attempt < retryBackoff.maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(delayInterval):
			case <-ctx.Done():
				return nil, fmt.Errorf("retry context done: %w", ctx.Err())
			}

			delayInterval = min(time.Duration(float64(delayInterval)*retryBackoff.backoffFactor), retryBackoff.maxInterval)
		}
		resp, err = doRequest()
		// retry by default on gateway errors
		if err != nil {
			continue
		}
		if !slices.Contains(retryBackoff.methods, resp.Request.Method) || !slices.Contains(retryBackoff.statusCodes, resp.StatusCode) {
			return resp, err
		}

		if retryAfter := resp.Header.Get("retry-after"); retryAfter != "" {
			if parsedNumber, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && parsedNumber > 0 {
				retryAfterDuration := time.Duration(parsedNumber) * time.Second
				delayInterval = min(retryAfterDuration, retryBackoff.maxInterval)
				continue
			}

			if parsedDate, err := time.Parse(time.RFC1123, retryAfter); err == nil {
				retryAfterDuration := time.Until(parsedDate)
				if retryAfterDuration > 0 {
					delayInterval = min(retryAfterDuration, retryBackoff.maxInterval)
				}
			}
		}
	}
	return resp, err
}

type ReverseProxyOptions struct {
	Logger                    *slog.Logger
	Tracer                    trace.Tracer
	Writer                    http.ResponseWriter
	Request                   *http.Request
	URN                       string
	Expression                *FilterRequest
	FilterConfig              *ResponseFilter
	Policy                    *guardian.Policy
	ResponseStatusCodeCapture *int
	VerifyResponse            func(*http.Response) error
	Attributes                tm.HTTPLogAttributes
	// Descriptor fields
	ID               string
	Name             string
	DeploymentID     string
	ProjectID        string
	ProjectSlug      string
	OrganizationID   string
	OrganizationSlug string
}

func reverseProxyRequest(ctx context.Context, opts ReverseProxyOptions) error {
	ctx, span := opts.Tracer.Start(ctx, fmt.Sprintf("gateway_proxy.%s", opts.Name))
	defer span.End()

	attrRecorder := opts.Attributes

	toolInfo := tm.ToolInfo{
		ID:             opts.ID,
		URN:            opts.URN,
		Name:           opts.Name,
		ProjectID:      opts.ProjectID,
		DeploymentID:   opts.DeploymentID,
		OrganizationID: opts.OrganizationID,
		FunctionID:     nil,
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           opts.Policy.Dialer().DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}

	var baseTransport http.RoundTripper = transport
	baseTransport = tm.NewToolCallLogRoundTripper(
		baseTransport,
		opts.Logger,
		opts.Tracer,
		toolInfo,
		attrRecorder,
	)

	otelTransport := otelhttp.NewTransport(
		baseTransport,
		otelhttp.WithPropagators(propagation.TraceContext{}),
	)

	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: otelTransport,
	}

	executeRequest := func() (*http.Response, error) {
		// Clone the request for each retry attempt
		retryReq := opts.Request.Clone(ctx)

		// Set the fresh body on the cloned request and wrap with counter
		if opts.Request.Body != nil && opts.Request.GetBody != nil {
			freshBody, err := opts.Request.GetBody()
			if err != nil {
				return nil, fmt.Errorf("retry: clone request body: %w", err)
			}

			retryReq.Body = freshBody
		}

		return client.Do(retryReq)
	}
	resp, err := retryWithBackoff(ctx, retryConfig{
		initialInterval: 500 * time.Millisecond,
		maxInterval:     5 * time.Second,
		maxAttempts:     3,
		backoffFactor:   2,
		statusCodes: []int{ // reasonable status code presets
			408, // Request Timeout
			429, // Rate Limit Exceeded
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504, // Gateway Timeout
			509, // Bandwidth Limit Exceeded
			521, // Web Server Is Down (Cloudflare)
			522, // Connection Timed Out (Cloudflare)
			523, // Origin Is Unreachable (Cloudflare)
			524, // A Timeout Occurred (Cloudflare)
		},
		methods: []string{
			http.MethodGet,
		},
	}, executeRequest)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return oops.E(oops.CodeGatewayError, err, "failed to execute request").Log(ctx, opts.Logger)
	}

	defer o11y.LogDefer(ctx, opts.Logger, func() error {
		return resp.Body.Close()
	})

	if err := opts.VerifyResponse(resp); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return oops.E(oops.CodeGatewayError, err, "response verification failed").Log(ctx, opts.Logger)
	}

	if len(resp.Trailer) > 0 {
		var trailerKeys []string
		for key := range resp.Trailer {
			trailerKeys = append(trailerKeys, key)
		}
		opts.Writer.Header().Set("Trailer", strings.Join(trailerKeys, ", "))
	}

	// We proxy over approved headers
	for key, values := range resp.Header {
		for _, value := range values {
			if slices.Contains(proxiedHeaders, key) {
				opts.Writer.Header().Add(key, value)
			}
		}
	}

	// Copy cookies from response
	for _, cookie := range resp.Cookies() {
		http.SetCookie(opts.Writer, cookie)
	}

	span.SetAttributes(attr.HTTPResponseExternal(true))
	opts.Writer.Header().Set(constants.HeaderProxiedResponse, "1")

	finalStatusCode := resp.StatusCode
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		opts.Writer.WriteHeader(finalStatusCode)

		// Streaming mode: flush after each chunk
		opts.Logger.InfoContext(ctx, "streaming with flush", attr.SlogHTTPResponseHeaderContentType(resp.Header.Get("Content-Type")))

		buf := make([]byte, 32*1024)
		flusher, canFlush := opts.Writer.(http.Flusher)
		var streamedBytes int64

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				streamedBytes += int64(n)
				if _, writeErr := opts.Writer.Write(buf[:n]); writeErr != nil {
					opts.Logger.ErrorContext(ctx, "client write failed", attr.SlogError(writeErr))
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if err != nil {
				if err != io.EOF {
					span.SetStatus(codes.Error, err.Error())
					opts.Logger.ErrorContext(ctx, "upstream read failed", attr.SlogError(err))
				}
				break
			}
		}

		attrRecorder.RecordResponseBody(streamedBytes)
	} else {
		var body io.Reader = resp.Body

		result := handleResponseFiltering(ctx, opts.Logger, opts.FilterConfig, opts.Expression, resp)
		if result != nil {
			opts.Writer.Header().Set("Content-Type", result.contentType)
			opts.Writer.Header().Set(constants.HeaderFilteredResponse, "1")
			span.SetAttributes(attr.HTTPResponseFiltered(true))
			finalStatusCode = result.statusCode
			body = result.resp
		}

		opts.Writer.WriteHeader(finalStatusCode)
		written, err := io.Copy(opts.Writer, body)
		attrRecorder.RecordResponseBody(written)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			opts.Logger.ErrorContext(ctx, "failed to copy response body", attr.SlogError(err))
		}
	}

	// Copy trailer values from upstream to client response (respecting allowlist)
	for key, values := range resp.Trailer {
		if !slices.Contains(proxiedHeaders, key) {
			continue
		}
		for _, value := range values {
			opts.Writer.Header().Add(key, value)
		}
	}

	if opts.ResponseStatusCodeCapture != nil {
		*opts.ResponseStatusCodeCapture = finalStatusCode
	}

	return nil
}

func processServerEnvVars(ctx context.Context, logger *slog.Logger, tool *HTTPToolCallPlan, env ToolCallEnv) string {
	if tool.ServerEnvVar != "" {
		if envVar := env.SystemEnv.Get(tool.ServerEnvVar); envVar != "" {
			return envVar
		} else if len(env.SystemEnv.All()) > 0 {
			// IMPORTANT: when system environment variables exist, we _always_ disallow user-supplied
			// server URLs to prevent exfiltration of system environment variables to user-controlled servers.
			return ""
		}

		envVar := env.UserConfig.Get(tool.ServerEnvVar)
		if envVar != "" {
			return envVar
		} else {
			logger.WarnContext(ctx, "provided variables for server not found", attr.SlogEnvVarName(tool.ServerEnvVar))
		}
	}
	return ""
}

var paramRegex = regexp.MustCompile(`{([^}]+)}`)

func insertPathParams(urlStr string, params map[string]string) string {
	if len(params) == 0 {
		return urlStr
	}

	return paramRegex.ReplaceAllStringFunc(urlStr, func(match string) string {
		name := match[1 : len(match)-1]
		if value, ok := params[name]; ok {
			return value
		}
		return match
	})
}

// encodeValue recursively encodes a value into URL form format, specifically handling deep objects
func formEncodeValue(values url.Values, key string, value any) {
	switch v := value.(type) {
	case []interface{}:
		// Handle arrays: key[0]=val1&key[1]=val2
		for i, item := range v {
			indexKey := fmt.Sprintf("%s[%d]", key, i)
			formEncodeValue(values, indexKey, item)
		}
	case map[string]interface{}:
		// Handle objects: key[field]=value
		for k, val := range v {
			objKey := fmt.Sprintf("%s[%s]", key, k)
			formEncodeValue(values, objKey, val)
		}
	default:
		// Handle primitives
		values.Set(key, fmt.Sprintf("%v", value))
	}
}
