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
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/serialization"
	tools_repo "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/toolsets"
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

func InstanceToolProxy(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, metrics *o11y.MetricsHandler, w http.ResponseWriter, requestBody io.Reader, envVars map[string]string, toolExecutionInfo *toolsets.HTTPToolExecutionInfo) error {
	ciEnv := newCaseInsensitiveEnv(envVars)
	var toolCallBody ToolCallBody
	if err := json.NewDecoder(requestBody).Decode(&toolCallBody); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid request body").Log(ctx, logger)
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
	var err error
	if strings.HasPrefix(toolExecutionInfo.Tool.RequestContentType.String, "application/x-www-form-urlencoded") {
		// Assume toolCallBody.Body is a JSON object (map[string]interface{})
		var formMap map[string]interface{}
		if err := json.Unmarshal(toolCallBody.Body, &formMap); err != nil {
			return oops.E(oops.CodeBadRequest, err, "failed to parse form body").Log(ctx, logger)
		}
		values := url.Values{}
		for k, v := range formMap {
			values.Set(k, fmt.Sprintf("%v", v))
		}
		encoded := values.Encode()
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

	processSecurity(ctx, logger, req, toolExecutionInfo, ciEnv)

	if err := protectSSRF(ctx, logger, req.URL); err != nil {
		return oops.E(oops.CodeForbidden, err, "unauthorized ssrf request").Log(ctx, logger)
	}

	return reverseProxyRequest(ctx, tracer, logger, metrics, toolExecutionInfo.Tool, w, req)
}

func reverseProxyRequest(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, metrics *o11y.MetricsHandler, tool tools_repo.HttpToolDefinition, w http.ResponseWriter, req *http.Request) error {
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
	resp, err := client.Do(req)
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

	// project_id and tool_name do add some cardinality but this should be reasonable
	// tracking metrics for failures and successes on tool calls is high value, nevertheless we will keep an eye on the metric cost
	if err := metrics.IncCounter(ctx, o11y.MetricNameToolCallCounter,
		attribute.String("tool", tool.Name),
		attribute.String("project_id", tool.ProjectID.String()),
		attribute.String("status_code", fmt.Sprintf("%d", resp.StatusCode)),
	); err != nil {
		logger.ErrorContext(ctx, fmt.Sprintf("failed to increment %s metric", string(o11y.MetricNameToolCallCounter)), slog.String("error", err.Error()))
	}

	// Copy status code
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
		// Normal response copy
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

func processSecurity(ctx context.Context, logger *slog.Logger, req *http.Request, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars *caseInsensitiveEnv) {
	for _, security := range toolExecutionInfo.Security {
		if !security.Type.Valid {
			logger.ErrorContext(ctx, "invalid security type in tool definition", slog.String("tool", toolExecutionInfo.Tool.Name))
			continue
		}

		switch security.Type.String {
		case "apiKey":
			if len(security.EnvVariables) == 0 {
				logger.ErrorContext(ctx, "no environment variables provided for api key auth", slog.String("scheme", security.Scheme.String))
			} else if envVars.Get(security.EnvVariables[0]) == "" {
				logger.ErrorContext(ctx, "missing value for environment variable in api key auth", slog.String("key", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
			} else if !security.Name.Valid || security.Name.String == "" {
				logger.ErrorContext(ctx, "no name provided for api key auth", slog.String("scheme", security.Scheme.String))
			} else {
				key := security.EnvVariables[0]
				switch security.InPlacement.String {
				case "header":
					req.Header.Set(security.Name.String, envVars.Get(key))
				case "query":
					values := req.URL.Query()
					values.Set(security.Name.String, envVars.Get(key))
					req.URL.RawQuery = values.Encode()
				default:
					logger.ErrorContext(ctx, "unsupported api key placement", slog.String("placement", security.InPlacement.String))
				}
			}
		case "http":
			switch security.Scheme.String {
			case "bearer":
				if len(security.EnvVariables) == 0 {
					logger.ErrorContext(ctx, "no environment variables provided for bearer auth", slog.String("scheme", security.Scheme.String))
				} else if envVars.Get(security.EnvVariables[0]) == "" {
					logger.ErrorContext(ctx, "token value is empty for bearer auth", slog.String("key", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
				} else {
					token := envVars.Get(security.EnvVariables[0])
					if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
						token = "Bearer " + token
					}
					req.Header.Set("Authorization", token)
				}
			case "basic":
				if len(security.EnvVariables) < 2 {
					logger.ErrorContext(ctx, "not enough environment variables provided for basic auth", slog.String("scheme", security.Scheme.String))
				} else {
					var username, password string
					for _, envVar := range security.EnvVariables {
						if strings.Contains(envVar, "USERNAME") {
							username = envVars.Get(envVar)
						} else if strings.Contains(envVar, "PASSWORD") {
							password = envVars.Get(envVar)
						}
					}

					if username == "" || password == "" {
						logger.ErrorContext(ctx, "missing username or password value for basic auth",
							slog.Bool("env_username", security.EnvVariables[0] == ""),
							slog.Bool("env_password", security.EnvVariables[1] == ""),
							slog.String("scheme", security.Scheme.String))
					} else {
						req.SetBasicAuth(username, password)
					}
				}
			default:
				logger.ErrorContext(ctx, "unsupported http security scheme", slog.String("scheme", security.Scheme.String))
				continue
			}
		default:
			logger.ErrorContext(ctx, "unsupported security scheme type", slog.String("type", security.Type.String))
			continue
		}
	}
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
