package instances

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/serialization"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

type ToolCallBody struct {
	PathParameters  map[string]any  `json:"pathParameters"`
	QueryParameters map[string]any  `json:"queryParameters"`
	Headers         map[string]any  `json:"headers"`
	Body            json.RawMessage `json:"body"`
}

func InstanceToolProxy(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, w http.ResponseWriter, r *http.Request, environmentEntries []environments_repo.EnvironmentEntry, toolExecutionInfo *toolsets.HTTPToolExecutionInfo) {
	var toolCallBody ToolCallBody
	if err := json.NewDecoder(r.Body).Decode(&toolCallBody); err != nil {
		logger.ErrorContext(ctx, "invalid request body", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Transform environment entries into a map
	envVars := make(map[string]string)
	for _, entry := range environmentEntries {
		envVars[entry.Name] = entry.Value
	}

	// Handle path parameters
	requestPath := toolExecutionInfo.Tool.Path
	if toolCallBody.PathParameters != nil {
		parameterSettings, err := serialization.ParseParameterSettings(toolExecutionInfo.Tool.PathSettings)
		if err != nil {
			logger.ErrorContext(ctx, "failed to parse path parameter settings", slog.String("error", err.Error()))
			http.Error(w, "problem parsing parameter settings", http.StatusInternalServerError)
			return
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
			logger.ErrorContext(ctx, "failed to parse URL with path parameters", slog.String("error", parseErr.Error()))
			http.Error(w, "Failed to parse URL with path parameters", http.StatusInternalServerError)
			return
		}
		requestPath = parsedURL.String()
	}

	// Get the server URL from the tool definition
	var serverURL string
	if toolExecutionInfo.Tool.DefaultServerUrl.Valid {
		serverURL = toolExecutionInfo.Tool.DefaultServerUrl.String
	}

	if envServerURL := processServerEnvVars(ctx, logger, toolExecutionInfo, envVars); envServerURL != "" {
		serverURL = envServerURL
	}

	if serverURL == "" {
		logger.ErrorContext(ctx, "no server URL provided for tool", slog.String("tool", toolExecutionInfo.Tool.Name))
		http.Error(w, "No server URL provided for tool", http.StatusInternalServerError)
		return
	}

	// Create a new request
	req, err := http.NewRequestWithContext(
		r.Context(),
		toolExecutionInfo.Tool.HttpMethod,
		strings.TrimRight(serverURL, "/")+"/"+strings.TrimLeft(requestPath, "/"),
		bytes.NewReader(toolCallBody.Body),
	)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// TODO: Eventually we need to get this from tool definition
	req.Header.Set("Content-Type", "application/json")

	if toolCallBody.QueryParameters != nil {
		parameterSettings, err := serialization.ParseParameterSettings(toolExecutionInfo.Tool.QuerySettings)
		if err != nil {
			logger.ErrorContext(ctx, "failed to parse query parameter settings", slog.String("error", err.Error()))
			http.Error(w, "problem parsing parameter settings", http.StatusInternalServerError)
			return
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
			logger.ErrorContext(ctx, "failed to parse header parameter settings", slog.String("error", err.Error()))
			http.Error(w, "problem parsing parameter settings", http.StatusInternalServerError)
			return
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

	processSecurity(ctx, logger, req, toolExecutionInfo, envVars)

	reverseProxyRequest(ctx, tracer, logger, toolExecutionInfo.Tool.Name, w, req)
}

func reverseProxyRequest(ctx context.Context, tracer trace.Tracer, logger *slog.Logger, toolName string, w http.ResponseWriter, req *http.Request) {
	ctx, span := tracer.Start(ctx, fmt.Sprintf("tool_proxy.%s", toolName))
	defer span.End()

	// TODO: This is temporary while in development
	bodyBytes, _ := io.ReadAll(req.Body)
	// Restore the body so it can be read again in the actual request
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	logger.InfoContext(ctx, "outgoing request details",
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Any("headers", req.Header),
		slog.String("body", string(bodyBytes)))

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
		logger.ErrorContext(ctx, "failed to execute request", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusBadGateway)
		return
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

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy cookies from response
	for _, cookie := range resp.Cookies() {
		http.SetCookie(w, cookie)
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
}

func processServerEnvVars(ctx context.Context, logger *slog.Logger, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars map[string]string) string {
	if toolExecutionInfo.Tool.ServerEnvVar != "" {
		envVar := envVars[toolExecutionInfo.Tool.ServerEnvVar]
		if envVar != "" {
			return envVar
		} else {
			logger.WarnContext(ctx, "environment variable for server not found", slog.String("key", toolExecutionInfo.Tool.ServerEnvVar))
		}
	}
	return ""
}

func processSecurity(ctx context.Context, logger *slog.Logger, req *http.Request, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars map[string]string) {
	for _, security := range toolExecutionInfo.Security {
		if !security.Type.Valid {
			logger.ErrorContext(ctx, "invalid security type in tool definition", slog.String("tool", toolExecutionInfo.Tool.Name))
			continue
		}

		switch security.Type.String {
		case "apiKey":
			if len(security.EnvVariables) == 0 {
				logger.ErrorContext(ctx, "no environment variables provided for api key auth", slog.String("scheme", security.Scheme.String))
			} else if envVars[security.EnvVariables[0]] == "" {
				logger.ErrorContext(ctx, "missing value for environment variable in api key auth", slog.String("key", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
			} else {
				key := security.EnvVariables[0]
				switch security.InPlacement {
				case "header":
					req.Header.Set(security.Name, envVars[key])
				case "query":
					values := req.URL.Query()
					values.Set(security.Name, envVars[key])
					req.URL.RawQuery = values.Encode()
				default:
					logger.ErrorContext(ctx, "unsupported api key placement", slog.String("placement", security.InPlacement))
				}
			}
		case "http":
			switch security.Scheme.String {
			case "bearer":
				if len(security.EnvVariables) == 0 {
					logger.ErrorContext(ctx, "no environment variables provided for bearer auth", slog.String("scheme", security.Scheme.String))
				} else if envVars[security.EnvVariables[0]] == "" {
					logger.ErrorContext(ctx, "token value is empty for bearer auth", slog.String("key", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
				} else {
					token := envVars[security.EnvVariables[0]]
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
							username = envVars[envVar]
						} else if strings.Contains(envVar, "PASSWORD") {
							password = envVars[envVar]
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
