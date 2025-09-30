package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
	tm "github.com/speakeasy-api/gram/server/internal/thirdparty/tool-metrics"
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

const (
	HeaderProxiedResponse  = "X-Gram-Proxy-Response"
	HeaderFilteredResponse = "X-Gram-Proxy-ResponseFiltered"
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
	HeaderParameters     map[string]any         `json:"headerParameters"`
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
	tcm     tm.ToolMetricsClient
}

func NewToolProxy(
	logger *slog.Logger,
	tracerProivder trace.TracerProvider,
	meterProvider metric.MeterProvider,
	source ToolCallSource,
	cache cache.Cache,
	policy *guardian.Policy,
	tcm tm.ToolMetricsClient,
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
		tcm:     tcm,
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
	dec := json.NewDecoder(bytes.NewReader(bodyBytes))
	// We use json.Number for accurate decoding in path, query, and header parameters.
	dec.UseNumber()

	if err := dec.Decode(&toolCallBody); err != nil {
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
				formEncodeValue(values, k, v)
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

	// Handle header parameters (non security schemes)
	if toolCallBody.HeaderParameters != nil {
		for name, value := range toolCallBody.HeaderParameters {
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

	return reverseProxyRequest(ctx, logger, itp.tracer, tool, toolCallBody.ResponseFilter, w, req, itp.policy, &responseStatusCode, itp.tcm)
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
	tcm tm.ToolMetricsClient,
) error {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("tool_proxy.%s", tool.Name))
	defer span.End()

	var durationMs float64
	var resolvedIP string

	spanCtx := trace.SpanContextFromContext(ctx)

	var traceID, spanID string
	if spanCtx.HasTraceID() {
		traceID = spanCtx.TraceID().String()
	}
	if spanCtx.HasSpanID() {
		spanID = spanCtx.SpanID().String()
	}

	var reqBodyBytes []byte
	if req.Body != nil {
		reqBodyBytes, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBodyBytes))
	}

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

		// capture the start time of the request ip using httptrace
		httpTrace := &httptrace.ClientTrace{
			GotConn: func(connInfo httptrace.GotConnInfo) {
				if len(connInfo.Conn.RemoteAddr().String()) == 0 {
					resolvedIP = "0.0.0.0" // fallback for when the ip address is not found
					return
				}

				resolvedIP = strings.Split(connInfo.Conn.RemoteAddr().String(), ":")[0]
				logger.DebugContext(ctx, "IP address resolved for request", "ip", resolvedIP)
			},
		}

		ctx = httptrace.WithClientTrace(ctx, httpTrace)
		retryReq = retryReq.WithContext(ctx)
		startTime := time.Now()

		resp, err := client.Do(retryReq)
		if err != nil {
			return nil, fmt.Errorf("retry: do request: %w", err)
		}

		durationMs = time.Since(startTime).Seconds() * 1000
		return resp, nil
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
		err = tcm.Log(ctx, tm.ToolHTTPRequest{
			OrganizationID: tool.OrganizationID,
			ProjectID:      tool.ProjectID,
			DeploymentID:   tool.DeploymentID,
			ToolID:         tool.ID,
			ToolURN:        tool.ID, // how to get the URN?
			ToolType:       tm.ToolTypeHttp,
			TraceID:        traceID,
			SpanID:         spanID,
			HTTPMethod:     req.Method,
			HTTPRoute:      req.URL.Path,
			StatusCode:     uint16(oops.StatusCodes[oops.CodeGatewayError]),
			DurationMs:     durationMs,
			UserAgent:      "Gram",
			ClientIPv4:     resolvedIP,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to log tool request").Log(ctx, logger)
		}

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

	span.SetAttributes(attr.HTTPResponseExternal(true))
	w.Header().Set(HeaderProxiedResponse, "1")

	var respBodyBuffer bytes.Buffer

	finalStatusCode := resp.StatusCode
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		w.WriteHeader(finalStatusCode)

		// Streaming mode: flush after each chunk
		logger.InfoContext(ctx, "streaming with flush", attr.SlogHTTPResponseHeaderContentType(resp.Header.Get("Content-Type")))

		// Create a TeeReader to capture the response body
		teeReader := io.TeeReader(resp.Body, &respBodyBuffer)

		buf := make([]byte, 32*1024)
		flusher, canFlush := w.(http.Flusher)

		for {
			n, err := teeReader.Read(buf)
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
			w.Header().Set("Content-Type", result.contentType)
			w.Header().Set(HeaderFilteredResponse, "1")
			span.SetAttributes(attr.HTTPResponseFiltered(true))
			finalStatusCode = result.statusCode
			body = result.resp
		}

		// Create a TeeReader to capture the response body
		teeReader := io.TeeReader(body, &respBodyBuffer)

		w.WriteHeader(finalStatusCode)
		if _, err = io.Copy(w, teeReader); err != nil {
			span.SetStatus(codes.Error, err.Error())
			logger.ErrorContext(ctx, "failed to copy response body", attr.SlogError(err))
		}
	}

	if responseStatusCodeCapture != nil {
		*responseStatusCodeCapture = finalStatusCode
	}

	// get the response headers
	responseHeaders := make(map[string]string)
	for key, values := range resp.Header {
		for _, value := range values {
			responseHeaders[key] = value
		}
	}

	// get the request headers
	requestHeaders := make(map[string]string)
	for key, values := range req.Header {
		for _, value := range values {
			requestHeaders[key] = value
		}
	}

	err = tcm.Log(ctx, tm.ToolHTTPRequest{
		OrganizationID:    tool.OrganizationID,
		ProjectID:         tool.ProjectID,
		DeploymentID:      tool.DeploymentID,
		ToolID:            tool.ID,
		ToolURN:           tool.ID, // how to get the URN?
		ToolType:          tm.ToolTypeHttp,
		TraceID:           traceID,
		SpanID:            spanID,
		HTTPMethod:        req.Method,
		HTTPRoute:         req.URL.Path,
		StatusCode:        uint16(finalStatusCode),
		DurationMs:        durationMs,
		UserAgent:         "Gram",
		ClientIPv4:        resolvedIP,
		RequestHeaders:    requestHeaders,
		RequestBody:       conv.Ptr(string(reqBodyBytes)),
		RequestBodySkip:   nil, // when will this be set?
		RequestBodyBytes:  uint64(len(reqBodyBytes)),
		ResponseHeaders:   responseHeaders,
		ResponseBody:      conv.Ptr(respBodyBuffer.String()),
		ResponseBodySkip:  nil, // when will this be set?
		ResponseBodyBytes: uint64(len(respBodyBuffer.Bytes())),
	})
	if err != nil {
		logger.ErrorContext(ctx, "failed to log tool http request", attr.SlogError(err))
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
