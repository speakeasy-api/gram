package instances

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/serialization"
	tools_repo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
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

type ToolCallBody struct {
	PathParameters       map[string]any    `json:"pathParameters"`
	QueryParameters      map[string]any    `json:"queryParameters"`
	Headers              map[string]any    `json:"headers"`
	Body                 json.RawMessage   `json:"body"`
	EnvironmentVariables map[string]string `json:"environmentVariables"`
	GramRequestSummary   string            `json:"gram-request-summary"`
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

type InstanceToolProxy struct {
	source  ToolCallSource
	logger  *slog.Logger
	tracer  trace.Tracer
	metrics *metrics
	cache   cache.Cache
}

func NewInstanceToolProxy(
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
	source ToolCallSource,
	cache cache.Cache,
) *InstanceToolProxy {
	return &InstanceToolProxy{
		source:  source,
		logger:  logger,
		tracer:  tracer,
		metrics: newMetrics(meter, logger),
		cache:   cache,
	}
}

func (itp *InstanceToolProxy) Do(
	ctx context.Context,
	w http.ResponseWriter,
	requestBody io.Reader,
	envVars map[string]string,
	toolExecutionInfo *toolsets.HTTPToolExecutionInfo,
) error {
	logger := itp.logger.With(
		slog.String("project_id", toolExecutionInfo.Tool.ProjectID.String()),
		slog.String("tool", toolExecutionInfo.Tool.Name),
		slog.String("path", toolExecutionInfo.Tool.Path),
		slog.String("source", string(itp.source)),
		slog.String("account_type", toolExecutionInfo.AccountType),
		slog.String("org_slug", toolExecutionInfo.OrgSlug),
		slog.String("project_slug", toolExecutionInfo.ProjectSlug),
	)

	// Variable to capture status code for metrics
	var responseStatusCode int
	defer func() {
		logger.InfoContext(ctx, "tool call", slog.String("status_code", fmt.Sprintf("%d", responseStatusCode)), slog.String("method", toolExecutionInfo.Tool.HttpMethod), slog.String("content_type", toolExecutionInfo.Tool.RequestContentType.String))
		// Record metrics for the tool call, some cardinality is introduced with org and tool name we will keep an eye on it
		itp.metrics.RecordHTTPToolCall(ctx, toolExecutionInfo.OrganizationID, toolExecutionInfo.OrgSlug, toolExecutionInfo.Tool.Name, responseStatusCode)
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

	// We are silently failing before we actually start returning errors to the LLM related to body not fitting json schema
	if len(toolExecutionInfo.Tool.Schema) > 0 {
		if validateErr := ValidateToolCallBody(ctx, logger, bodyBytes, string(toolExecutionInfo.Tool.Schema)); validateErr != nil {
			logger.InfoContext(ctx, "tool call request schema failed validation", slog.String("error", validateErr.Error()))
			responseStatusCode = http.StatusBadRequest
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			if err := json.NewEncoder(w).Encode(toolcallErrorSchema{
				Error: fmt.Sprintf("The input to the tool is invalid with the attached error. Please review the tool schema closely: %s", validateErr.Error()),
			}); err != nil {
				logger.ErrorContext(ctx, "failed to encode tool call error", slog.String("error", err.Error()))
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
	requestPath := toolExecutionInfo.Tool.Path
	if toolCallBody.PathParameters != nil {
		parameterSettings, err := serialization.ParseParameterSettings(toolExecutionInfo.Tool.PathSettings)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to parse path parameter settings").Log(ctx, logger)
		}
		pathParams := make(map[string]string)
		for name, value := range toolCallBody.PathParameters {
			parameterSettings := parameterSettings[name]
			if parameterSettings == nil {
				logger.WarnContext(ctx, "no parameter settings found for path parameter", slog.String("parameter", name))
			}
			// style: simple and explode: false is the default serialization for path parameters
			params := serialization.ParsePathAndHeaderParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), parameterSettings)
			if params != nil && params[name] != "" {
				pathParams[name] = params[name]
			} else {
				logger.ErrorContext(ctx, "failed to parse path parameter", slog.String("parameter", name), slog.Any("value", value))
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
	if toolExecutionInfo.Tool.DefaultServerUrl.Valid {
		serverURL = toolExecutionInfo.Tool.DefaultServerUrl.String
	}

	if envServerURL := processServerEnvVars(ctx, logger, toolExecutionInfo, ciEnv); envServerURL != "" {
		serverURL = envServerURL
	}

	if serverURL == "" {
		logger.ErrorContext(ctx, "no server URL provided for tool", slog.String("tool", toolExecutionInfo.Tool.Name))
		return oops.E(oops.CodeUnexpected, nil, "no server URL provided for tool").Log(ctx, logger)
	}

	// Create a new request
	fullURL := strings.TrimRight(serverURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
	var req *http.Request
	if strings.HasPrefix(toolExecutionInfo.Tool.RequestContentType.String, "application/x-www-form-urlencoded") {
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
			toolExecutionInfo.Tool.HttpMethod,
			fullURL,
			strings.NewReader(encoded),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build url-encoded request").Log(ctx, logger)
		}
		req.Header.Set("Content-Type", toolExecutionInfo.Tool.RequestContentType.String)
	} else {
		req, err = http.NewRequestWithContext(
			ctx,
			toolExecutionInfo.Tool.HttpMethod,
			fullURL,
			bytes.NewReader(toolCallBody.Body),
		)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to build json request").Log(ctx, logger)
		}
		if toolExecutionInfo.Tool.RequestContentType.String != "" {
			req.Header.Set("Content-Type", toolExecutionInfo.Tool.RequestContentType.String)
		}
	}

	if toolCallBody.QueryParameters != nil {
		parameterSettings, err := serialization.ParseParameterSettings(toolExecutionInfo.Tool.QuerySettings)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "failed to parse query parameter settings").Log(ctx, logger)
		}
		values := url.Values{}
		for name, value := range toolCallBody.QueryParameters {
			parameterSettings := parameterSettings[name]
			if parameterSettings == nil {
				logger.WarnContext(ctx, "no parameter settings found for query parameter", slog.String("parameter", name))
			}
			// style: form and explode: true with , delimiter is default for query parameters
			params := serialization.ParseQueryParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), parameterSettings)
			if len(params) > 0 {
				for name, value := range params {
					for _, vv := range value {
						values.Add(name, vv)
					}
				}
			} else {
				logger.ErrorContext(ctx, "failed to parse query parameter", slog.String("parameter", name), slog.Any("value", value))
			}
		}
		req.URL.RawQuery = values.Encode()
	}

	// Handle headers
	if toolCallBody.Headers != nil {
		parameterSettings, err := serialization.ParseParameterSettings(toolExecutionInfo.Tool.HeaderSettings)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "failed to parse header parameter settings").Log(ctx, logger)
		}
		for name, value := range toolCallBody.Headers {
			parameterSettings := parameterSettings[name]
			if parameterSettings == nil {
				logger.WarnContext(ctx, "no parameter settings found for header parameter", slog.String("parameter", name))
			}
			// style: simple and explode: false is the default serialization for headers
			params := serialization.ParsePathAndHeaderParameter(ctx, logger, name, reflect.TypeOf(value), reflect.ValueOf(value), parameterSettings)
			if params != nil && params[name] != "" {
				req.Header.Set(name, params[name])
			} else {
				logger.ErrorContext(ctx, "failed to parse header", slog.String("header", name), slog.Any("value", value))
			}
		}
	}

	processSecurity(ctx, logger, req, w, &responseStatusCode, toolExecutionInfo, itp.cache, ciEnv, serverURL)

	if err := protectSSRF(ctx, logger, req.URL); err != nil {
		return oops.E(oops.CodeForbidden, err, "unauthorized ssrf request").Log(ctx, logger)
	}

	return reverseProxyRequest(ctx, logger, itp.tracer, toolExecutionInfo.Tool, w, req, &responseStatusCode)
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
	tool tools_repo.HttpToolDefinition,
	w http.ResponseWriter,
	req *http.Request,
	responseStatusCodeCapture *int,
) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("tool_proxy.%s", tool.Name))
	defer span.End()

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
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
		logger.InfoContext(ctx, "streaming with flush", slog.String("content_type", resp.Header.Get("Content-Type")))

		buf := make([]byte, 32*1024)
		flusher, canFlush := w.(http.Flusher)

		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if _, writeErr := w.Write(buf[:n]); writeErr != nil {
					logger.ErrorContext(ctx, "client write failed", slog.String("error", writeErr.Error()))
					break
				}
				if canFlush {
					flusher.Flush()
				}
			}
			if err != nil {
				if err != io.EOF {
					span.SetStatus(codes.Error, err.Error())
					logger.ErrorContext(ctx, "upstream read failed", slog.String("error", err.Error()))
				}
				break
			}
		}
	} else {
		if _, err := io.Copy(w, resp.Body); err != nil {
			span.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to copy response body", slog.String("error", err.Error()))
		}
	}

	return nil
}

func protectSSRF(ctx context.Context, logger *slog.Logger, parsed *url.URL) error {
	host := parsed.Hostname()
	// Block localhost explicitly
	if host == "localhost" || strings.HasPrefix(host, "localhost.") {
		logger.WarnContext(ctx, "blocked localhost", slog.String("url", parsed.String()))
		return errors.New("localhost is not allowed")
	}

	// If host is an IP, block private ranges
	ip := net.ParseIP(host)
	if ip != nil {
		privateCIDRs := []string{
			"127.0.0.0/8",
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16",
			"::1/128",
			"fc00::/7",
			"fe80::/10",
		}

		for _, cidr := range privateCIDRs {
			if _, block, err := net.ParseCIDR(cidr); err == nil && block.Contains(ip) {
				logger.WarnContext(ctx, "blocked private IP", slog.String("ip", ip.String()), slog.String("url", parsed.String()))
				return errors.New("internal IP is not allowed")
			}
		}
	}

	return nil
}

func processServerEnvVars(ctx context.Context, logger *slog.Logger, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars *caseInsensitiveEnv) string {
	if toolExecutionInfo.Tool.ServerEnvVar != "" {
		envVar := envVars.Get(toolExecutionInfo.Tool.ServerEnvVar)
		if envVar != "" {
			return envVar
		} else {
			logger.WarnContext(ctx, "environment variable for server not found", slog.String("key", toolExecutionInfo.Tool.ServerEnvVar))
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
