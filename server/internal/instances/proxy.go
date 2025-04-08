package instances

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	"log/slog"

	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/serialization"
	"github.com/speakeasy-api/gram/internal/toolsets"
)

type ToolCallBody struct {
	PathParameters  map[string]any  `json:"pathParameters"`
	QueryParameters map[string]any  `json:"queryParameters"`
	Headers         map[string]any  `json:"headers"`
	Body            json.RawMessage `json:"body"`
}

func InstanceToolProxy(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, r *http.Request, environmentEntries []environments_repo.EnvironmentEntry, toolExecutionInfo *toolsets.HTTPToolExecutionInfo) {
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
		pathParams := make(map[string]string)
		for name, value := range toolCallBody.PathParameters {
			// style: simple and explode: false is the default serialization for path parameters
			params := serialization.ParseSimpleParams(name, reflect.TypeOf(value), reflect.ValueOf(value), false)
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

	newReqCtx := r.Context()
	// Create a new request
	req, err := http.NewRequestWithContext(
		newReqCtx,
		toolExecutionInfo.Tool.HttpMethod,
		strings.TrimRight(serverURL, "/")+"/"+strings.TrimLeft(requestPath, "/"),
		bytes.NewReader(toolCallBody.Body),
	)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	if toolCallBody.QueryParameters != nil {
		values := url.Values{}
		for name, value := range toolCallBody.QueryParameters {
			// style: form and explode: true with , delimiter is default for query parameters
			params := serialization.ParseFormParams(name, reflect.TypeOf(value), reflect.ValueOf(value), ",", true)
			if params != nil && len(params) > 0 {
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
		for name, value := range toolCallBody.Headers {
			// style: simple and explode: false is the default serialization for headers
			params := serialization.ParseSimpleParams(name, reflect.TypeOf(value), reflect.ValueOf(value), false)
			if params != nil && params[name] != "" {
				req.Header.Set(name, params[name])
			} else {
				logger.ErrorContext(ctx, "failed to parse header", slog.String("header", name), slog.Any("value", value))
			}
		}
	}

	processSecurity(ctx, logger, req, toolExecutionInfo, envVars)

	// TODO: This is temporary while in development
	bodyBytes, _ := io.ReadAll(req.Body)
	// Restore the body so it can be read again in the actual request
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	logger.InfoContext(ctx, "outgoing request details",
		slog.String("method", req.Method),
		slog.String("url", req.URL.String()),
		slog.Any("headers", req.Header),
		slog.String("body", string(bodyBytes)))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		logger.ErrorContext(ctx, "failed to execute request", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

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

	// Copy the response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		logger.ErrorContext(ctx, "failed to copy response body", slog.String("error", err.Error()))
		// Can't write headers or body at this point since we've already started the response
		return
	}
}
func processServerEnvVars(ctx context.Context, logger *slog.Logger, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars map[string]string) string {
	if toolExecutionInfo.Tool.ServerEnvVar != "" {
		envVar := envVars[toolExecutionInfo.Tool.ServerEnvVar]
		if envVar != "" {
			return envVar
		} else {
			logger.WarnContext(ctx, "environment variable for server not found", slog.String("envVar", toolExecutionInfo.Tool.ServerEnvVar))
		}
	}
	return ""
}

func processSecurity(ctx context.Context, logger *slog.Logger, req *http.Request, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, envVars map[string]string) {
	for _, security := range toolExecutionInfo.Security {
		switch security.Type {
		case "apiKey":
			if len(security.EnvVariables) == 0 {
				logger.ErrorContext(ctx, "no environment variables provided for api key auth", slog.String("scheme", security.Scheme.String))
			} else if envVars[security.EnvVariables[0]] == "" {
				logger.ErrorContext(ctx, "missing value for environment variable in api key auth", slog.String("envVar", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
			} else {
				key := security.EnvVariables[0]
				// TODO: We currently aren't directly storing name for API key security schemes, is this name sufficient?
				if security.InPlacement == "header" {
					req.Header.Set(security.Name, envVars[key])
				} else if security.InPlacement == "query" {
					values := req.URL.Query()
					values.Set(security.Name, envVars[key])
					req.URL.RawQuery = values.Encode()
				} else {
					logger.ErrorContext(ctx, "unsupported api key placement", slog.String("placement", security.InPlacement))
				}
			}
		case "http":
			switch security.Scheme.String {
			case "bearer":
				if len(security.EnvVariables) == 0 {
					logger.ErrorContext(ctx, "no environment variables provided for bearer auth", slog.String("scheme", security.Scheme.String))
				} else if envVars[security.EnvVariables[0]] == "" {
					logger.ErrorContext(ctx, "token value is empty for bearer auth", slog.String("envVar", security.EnvVariables[0]), slog.String("scheme", security.Scheme.String))
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
							slog.String("envVarUsername", security.EnvVariables[0]),
							slog.String("envVarPassword", security.EnvVariables[1]),
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
			logger.ErrorContext(ctx, "unsupported security scheme type", slog.String("type", security.Type))
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
