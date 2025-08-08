package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
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
}

type ResponseFilterRequest struct {
	Type   string `json:"type"`
	Filter string `json:"filter"`
}

type ToolCallBody struct {
	PathParameters       map[string]any         `json:"pathParameters"`
	QueryParameters      map[string]any         `json:"queryParameters"`
	Headers              map[string]any         `json:"headers"`
	Body                 json.RawMessage        `json:"body"`
	ResponseFilter       *ResponseFilterRequest `json:"responseFilter"`
	EnvironmentVariables map[string]string      `json:"environmentVariables"`
	GramRequestSummary   string                 `json:"gram-request-summary"`
}

type caseInsensitiveEnv struct {
	data map[string]string
}

func newCaseInsensitiveEnv(m map[string]string) *caseInsensitiveEnv {
	ci := &caseInsensitiveEnv{data: make(map[string]string, len(m))}
	for k, v := range m {
		ci.data[strings.ToLower(k)] = v
	}
	return ci
}

func (c *caseInsensitiveEnv) Get(key string) string {
	return c.data[strings.ToLower(key)]
}

func (c *caseInsensitiveEnv) Set(key, value string) {
	c.data[strings.ToLower(key)] = value
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
	source  ToolCallSource
	logger  *slog.Logger
	tracer  trace.Tracer
	metrics *metrics
	cache   cache.Cache
	policy  *guardian.Policy
}

func NewToolProxy(
	logger *slog.Logger,
	tracerProivder trace.TracerProvider,
	meterProvider metric.MeterProvider,
	source ToolCallSource,
	cache cache.Cache,
	policy *guardian.Policy,
) *ToolProxy {
	tracer := tracerProivder.Tracer("github.com/speakeasy-api/gram/server/internal/gateway")
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/gateway")

	return &ToolProxy{
		source:  source,
		logger:  logger,
		tracer:  tracer,
		metrics: newMetrics(meter, logger),
		cache:   cache,
		policy:  policy,
	}
}

func (itp *ToolProxy) Do(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	envVars map[string]string,
	tool *HTTPTool,
) error {
	ctx, span := itp.tracer.Start(ctx, "proxyToolCall", trace.WithAttributes(
		attr.ToolName(tool.Name),
		attr.ToolID(tool.ID),
		attr.ProjectID(tool.ProjectID),
		attr.DeploymentID(tool.DeploymentID),
		attr.ToolCallSource(string(itp.source)),
		attr.HTTPRoute(tool.Path), // this is just from the raw OpenAPI spec. It is not a path with any parameters filled in, so not identifiable.
	))
	defer span.End()

	logger := itp.logger.With(
		attr.SlogProjectID(tool.ProjectID),
		attr.SlogDeploymentID(tool.DeploymentID),
		attr.SlogToolID(tool.ID),
		attr.SlogToolName(tool.Name),
		attr.SlogToolCallSource(string(itp.source)),
		attr.SlogHTTPRoute(tool.Path), // this is just from the raw OpenAPI spec. It is not a path with any parameters filled in, so not identifiable.
	)

	// Variable to capture status code for metrics
	var responseStatusCode int
	defer func() {
		logger.InfoContext(ctx, "tool call",
			attr.SlogHTTPResponseStatusCode(responseStatusCode),
			attr.SlogHTTPRequestMethod(tool.Method),
			attr.SlogHTTPResponseHeaderContentType(tool.RequestContentType.Value),
		)
		// Record metrics for the tool call, some cardinality is introduced with org and tool name we will keep an eye on it
		itp.metrics.RecordHTTPToolCall(ctx, tool.OrganizationID, tool.Name, responseStatusCode)

		span.SetAttributes(attr.HTTPResponseStatusCode(responseStatusCode))
	}()

	ciEnv := newCaseInsensitiveEnv(envVars)

	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to read request body").Log(ctx, logger)
	}

	var toolCallBody ToolCallBody
	if err := json.Unmarshal(bodyBytes, &toolCallBody); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request body").Log(ctx, logger)
	}

	if len(tool.Schema) > 0 {
		if validateErr := validateToolCallBody(ctx, logger, bodyBytes, string(tool.Schema)); validateErr != nil {
			logger.InfoContext(ctx, "tool call request schema failed validation", attr.SlogError(validateErr))
			responseStatusCode = http.StatusBadRequest
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(toolcallErrorSchema{
				Error: fmt.Sprintf("The input to the tool is invalid with the attached error. Please review the tool schema closely: %s", validateErr.Error()),
			}); err != nil {
				logger.ErrorContext(ctx, "failed to encode tool call error", attr.SlogError(err))
			}
			return nil
		}
	}

	// environment variable overrides on tool calls typically defined in the SDK
	if toolCallBody.EnvironmentVariables != nil {
		for k, v := range toolCallBody.EnvironmentVariables {
			ciEnv.Set(k, v)
		}
	}

	// Handle path parameters
	requestPath := tool.Path
	if toolCallBody.PathParameters != nil {
		pathParams := make(map[string]string)
		for name, value := range toolCallBody.PathParameters {
			param := tool.PathParams[name]
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
	if tool.DefaultServerUrl.Valid {
		serverURL = tool.DefaultServerUrl.Value
	}

	if envServerURL := processServerEnvVars(ctx, logger, tool, ciEnv); envServerURL != "" {
		serverURL = envServerURL
	}

	if serverURL == "" {
		logger.ErrorContext(ctx, "no server URL provided for tool", attr.SlogToolName(tool.Name))
		return oops.E(oops.CodeInvalid, nil, "no server URL provided for tool").Log(ctx, logger)
	}

	// Create a new request
	fullURL := strings.TrimRight(serverURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
	var req *http.Request
	if strings.HasPrefix(tool.RequestContentType.Value, "application/x-www-form-urlencoded") {
		encoded := ""
		if len(toolCallBody.Body) > 0 {
			// Assume toolCallBody.Body is a JSON object (map[string]interface{})
			var formMap map[string]interface{}
			if err := json.Unmarshal(toolCallBody.Body, &formMap); err != nil {
				return oops.E(oops.CodeBadRequest, err, "failed to parse form body").Log(ctx, logger)
			}
			values := url.Values{}
			for k, v := range formMap {
				values.Set(k, fmt.Sprintf("%v", v))
			}
			encoded = values.Encode()
		}
		req, err = http.NewRequestWithContext(
			ctx,
			tool.Method,
			fullURL,
			strings.NewReader(encoded),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build url-encoded request").Log(ctx, logger)
		}
		req.Header.Set("Content-Type", tool.RequestContentType.Value)
	} else {
		req, err = http.NewRequestWithContext(
			ctx,
			tool.Method,
			fullURL,
			bytes.NewReader(toolCallBody.Body),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build json request").Log(ctx, logger)
		}
		if tool.RequestContentType.Value != "" {
			req.Header.Set("Content-Type", tool.RequestContentType.Value)
		}
	}

	if toolCallBody.QueryParameters != nil {
		values := url.Values{}
		for name, value := range toolCallBody.QueryParameters {
			param := tool.QueryParams[name]
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

	// Handle headers
	if toolCallBody.Headers != nil {
		for name, value := range toolCallBody.Headers {
			param := tool.HeaderParams[name]
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

	shouldContinue := processSecurity(ctx, logger, req, w, &responseStatusCode, tool, itp.cache, ciEnv, serverURL)
	if !shouldContinue {
		return nil
	}

	req.Header.Set("X-Gram-Proxy", "1")

	return reverseProxyRequest(ctx, logger, itp.tracer, tool, toolCallBody.ResponseFilter, w, req, itp.policy, &responseStatusCode)
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

func reverseProxyRequest(ctx context.Context,
	logger *slog.Logger,
	tracer trace.Tracer,
	tool *HTTPTool,
	responseFilter *ResponseFilterRequest,
	w http.ResponseWriter,
	req *http.Request,
	policy *guardian.Policy,
	responseStatusCodeCapture *int,
) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("tool_proxy.%s", tool.Name))
	defer span.End()

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           policy.Dialer().DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   runtime.GOMAXPROCS(0) + 1,
	}

	client := &http.Client{
		Timeout:   60 * time.Second,
		Transport: transport,
	}

	executeRequest := func() (*http.Response, error) {
		// Clone the request for each retry attempt
		retryReq := req.Clone(ctx)

		// Set fresh body on the cloned request
		if req.Body != nil && req.GetBody != nil {
			freshBody, err := req.GetBody()
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
		return oops.E(oops.CodeGatewayError, err, "failed to execute request").Log(ctx, logger)
	}
	defer o11y.LogDefer(ctx, logger, func() error {
		return resp.Body.Close()
	})

	if len(resp.Trailer) > 0 {
		var trailerKeys []string
		for key := range resp.Trailer {
			trailerKeys = append(trailerKeys, key)
		}
		w.Header().Set("Trailer", strings.Join(trailerKeys, ", "))
	}

	// We proxy over approved headers
	for key, values := range resp.Header {
		for _, value := range values {
			if slices.Contains(proxiedHeaders, key) {
				w.Header().Add(key, value)
			}
		}
	}

	// Copy cookies from response
	for _, cookie := range resp.Cookies() {
		http.SetCookie(w, cookie)
	}

	// Copy status code
	if responseStatusCodeCapture != nil {
		*responseStatusCodeCapture = resp.StatusCode
	}
	w.WriteHeader(resp.StatusCode)

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		// Streaming mode: flush after each chunk
		logger.InfoContext(ctx, "streaming with flush", attr.SlogHTTPResponseHeaderContentType(resp.Header.Get("Content-Type")))

		buf := make([]byte, 32*1024)
		flusher, canFlush := w.(http.Flusher)

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					logger.ErrorContext(ctx, "client write failed", attr.SlogError(writeErr))
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if err != nil {
				if err != io.EOF {
					span.SetStatus(codes.Error, err.Error())
					logger.ErrorContext(ctx, "upstream read failed", attr.SlogError(err))
				}
				break
			}
		}
	} else {
		var body io.Reader = resp.Body

		result := handleResponseFiltering(ctx, logger, tool, responseFilter, resp)
		if result != nil {
			w.WriteHeader(result.statusCode)
			w.Header().Set("Content-Type", result.contentType)
			body = result.resp
		}

		if _, err := io.Copy(w, body); err != nil {
			span.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to copy response body", attr.SlogError(err))
		}
	}

	return nil
}

func processServerEnvVars(ctx context.Context, logger *slog.Logger, tool *HTTPTool, envVars *caseInsensitiveEnv) string {
	if tool.ServerEnvVar != "" {
		envVar := envVars.Get(tool.ServerEnvVar)
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
