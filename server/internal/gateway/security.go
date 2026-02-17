package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

func processSecurity(
	ctx context.Context,
	logger *slog.Logger,
	req *http.Request,
	w http.ResponseWriter,
	responseStatusCodeCapture *int,
	tool *ToolDescriptor,
	plan *HTTPToolCallPlan,
	cacheImpl cache.Cache,
	env toolconfig.ToolCallEnv,
	serverURL string,
	attrRecorder tm.HTTPLogAttributes,
) bool {
	// Merge: system env is base, user config overrides
	mergedEnv := toolconfig.NewCaseInsensitiveEnv()
	for k, v := range env.SystemEnv.All() {
		mergedEnv.Set(k, v)
	}
	for k, v := range env.UserConfig.All() {
		mergedEnv.Set(k, v)
	}

	securityHeadersProcessed := make(map[string]string)
	setHeader := func(key, value string) {
		req.Header.Set(key, value)
		securityHeadersProcessed[http.CanonicalHeaderKey(key)] = value
	}
	defer func() {
		// if tool calling logging is enabled this will record the headers that were set with redaction
		attrRecorder.RecordRequestHeaders(securityHeadersProcessed, true)
	}()
	for _, security := range plan.Security {
		if !security.Type.Valid {
			logger.ErrorContext(ctx, "invalid security type in tool definition")
			continue
		}

		switch security.Type.Value {
		case "apiKey":
			if len(security.EnvVariables) == 0 {
				logger.ErrorContext(ctx, "no environment variables provided for api key auth", attr.SlogSecurityScheme(security.Scheme.Value))
			} else if mergedEnv.Get(security.EnvVariables[0]) == "" {
				logger.ErrorContext(ctx, "missing value for environment variable in api key auth", attr.SlogEnvVarName(security.EnvVariables[0]), attr.SlogSecurityScheme(security.Scheme.Value))
			} else if !security.Name.Valid || security.Name.Value == "" {
				logger.ErrorContext(ctx, "no name provided for api key auth", attr.SlogSecurityScheme(security.Scheme.Value))
			} else {
				key := security.EnvVariables[0]
				switch security.Placement.Value {
				case "header":
					setHeader(security.Name.Value, mergedEnv.Get(key))
				case "query":
					values := req.URL.Query()
					values.Set(security.Name.Value, mergedEnv.Get(key))
					req.URL.RawQuery = values.Encode()
				default:
					logger.ErrorContext(ctx, "unsupported api key placement", attr.SlogSecurityPlacement(security.Placement.Value))
				}
			}
		case "http":
			switch security.Scheme.Value {
			case "bearer":
				if len(security.EnvVariables) == 0 {
					logger.ErrorContext(ctx, "no environment variables provided for bearer auth", attr.SlogSecurityScheme(security.Scheme.Value))
				} else if mergedEnv.Get(security.EnvVariables[0]) == "" {
					logger.ErrorContext(ctx, "token value is empty for bearer auth", attr.SlogEnvVarName(security.EnvVariables[0]), attr.SlogSecurityScheme(security.Scheme.Value))
				} else {
					token := mergedEnv.Get(security.EnvVariables[0])
					setHeader("Authorization", formatForBearer(token))
				}
			case "basic":
				if len(security.EnvVariables) < 2 {
					logger.ErrorContext(ctx, "not enough environment variables provided for basic auth", attr.SlogSecurityScheme(security.Scheme.Value))
				} else {
					var username, password string
					for _, envVar := range security.EnvVariables {
						if strings.Contains(envVar, "USERNAME") {
							username = mergedEnv.Get(envVar)
						} else if strings.Contains(envVar, "PASSWORD") {
							password = mergedEnv.Get(envVar)
						}
					}

					if username == "" || password == "" {
						logger.ErrorContext(ctx, "missing username or password value for basic auth",
							attr.SlogValueString(fmt.Sprintf("env_username_present=%t env_password_present=%t", security.EnvVariables[0] == "", security.EnvVariables[1] == "")),
							attr.SlogSecurityScheme(security.Scheme.Value))
					} else {
						req.SetBasicAuth(username, password)
						// special case doesn't go through normal setHeader function
						securityHeadersProcessed[http.CanonicalHeaderKey("Authorization")] = req.Header.Get("Authorization")
					}
				}
			default:
				logger.ErrorContext(ctx, "unsupported http security scheme", attr.SlogSecurityScheme(security.Scheme.Value))
				continue
			}
		case "openIdConnect":
			for _, envVar := range security.EnvVariables {
				if strings.Contains(envVar, "ACCESS_TOKEN") {
					if token := mergedEnv.Get(envVar); token == "" {
						logger.ErrorContext(ctx, "missing authorization code", attr.SlogEnvVarName(envVar))
					} else {
						setHeader("Authorization", formatForBearer(token))
					}
				}
			}
		case "oauth2":
			if security.OAuthTypes == nil {
				logger.ErrorContext(ctx, "no oauth types provided for oauth2 auth", attr.SlogSecurityScheme(security.Scheme.Value))
			}

			for _, oauthType := range security.OAuthTypes {
				switch oauthType {
				case "authorization_code", "implicit":
					for _, envVar := range security.EnvVariables {
						if strings.Contains(envVar, "ACCESS_TOKEN") {
							if token := mergedEnv.Get(envVar); token == "" {
								logger.ErrorContext(ctx, "missing authorization code", attr.SlogEnvVarName(envVar))
							} else {
								setHeader("Authorization", formatForBearer(token))
							}
						}
					}
				case "client_credentials":
					token, err := processClientCredentials(ctx, logger, req, cacheImpl, tool, plan.SecurityScopes, security, mergedEnv, serverURL)
					if err != nil {
						logger.ErrorContext(ctx, "could not process client credentials", attr.SlogError(err))
						if strings.Contains(err.Error(), "failed to make client credentials token request") {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusUnauthorized)
							if responseStatusCodeCapture != nil {
								*responseStatusCodeCapture = http.StatusUnauthorized
							}
							if err := json.NewEncoder(w).Encode(toolcallErrorSchema{
								Error: err.Error(),
							}); err != nil {
								logger.ErrorContext(ctx, "failed to encode tool call error", attr.SlogError(err))
							}
							return false
						}
					}
					setHeader("Authorization", formatForBearer(token))
				}
			}
		default:
			logger.ErrorContext(ctx, "unsupported security scheme type", attr.SlogSecurityType(security.Type.Value))
			continue
		}
	}

	for key, value := range env.SystemEnv.All() {
		headerKey := toolconfig.ToHTTPHeader(key)
		canonicalKey := http.CanonicalHeaderKey(headerKey)
		if _, alreadyProcessed := securityHeadersProcessed[canonicalKey]; !alreadyProcessed {
			req.Header.Set(headerKey, value)
		}
	}

	return true
}

type oAuthFlows struct {
	ClientCredentials *oAuthFlow `json:"clientCredentials,omitempty" yaml:"clientCredentials,omitempty"`
	AuthorizationCode *oAuthFlow `json:"authorizationCode,omitempty" yaml:"authorizationCode,omitempty"`
}

type oAuthFlow struct {
	AuthorizationUrl string `json:"authorizationUrl,omitempty" yaml:"authorizationUrl,omitempty"`
	TokenUrl         string `json:"tokenUrl,omitempty" yaml:"tokenUrl,omitempty"`
	RefreshUrl       string `json:"refreshUrl,omitempty" yaml:"refreshUrl,omitempty"`
}

var _ cache.CacheableObject[clientCredentialsTokenCache] = (*clientCredentialsTokenCache)(nil)

type clientCredentialsTokenCache struct {
	ProjectID   string
	ClientID    string
	TokenURL    string
	AccessToken string
	Scopes      []string
	ExpiresIn   time.Duration
	CreatedAt   time.Time
}

func normalizeScopes(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}

	normalized := make([]string, len(scopes))
	copy(normalized, scopes)
	slices.Sort(normalized)

	return strings.Join(normalized, ",")
}

func clientCredentialsTokenCacheCacheKey(projectID, clientID, tokenURL string, scopes []string) string {
	return "clientCredentialsTokenCache:projectID-" + projectID + "-clientID-" + clientID + "-tokenURL-" + url.QueryEscape(tokenURL) + "-scopes-" + normalizeScopes(scopes)
}

func (c clientCredentialsTokenCache) CacheKey() string {
	return clientCredentialsTokenCacheCacheKey(c.ProjectID, c.ClientID, c.TokenURL, c.Scopes)
}

func (c clientCredentialsTokenCache) AdditionalCacheKeys() []string {
	return []string{}
}

func (c clientCredentialsTokenCache) TTL() time.Duration {
	return c.ExpiresIn
}

type clientCredentialsTokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

type clientCredentialsTokenResponseCamelCase struct {
	AccessToken string `json:"accessToken"`
	ExpiresIn   int    `json:"expiresIn"`
}

func processClientCredentials(ctx context.Context, logger *slog.Logger, req *http.Request, cacheImpl cache.Cache, tool *ToolDescriptor, planScopes map[string][]string, security *HTTPToolSecurity, mergedEnv *toolconfig.CaseInsensitiveEnv, serverURL string) (string, error) {
	// To discuss, currently we are taking the approach of exact scope match for reused tokens
	// We could look into enabling a prefix match feature for caches where we return multiple entries matching the projectID, clientID, tokenURL and then check scopes against all returned values
	// We would want to make sure any underlying cache implementation supports this feature
	tokenCache := cache.NewTypedObjectCache[clientCredentialsTokenCache](logger.With(attr.SlogCacheNamespace("client_credentials_token_cache")), cacheImpl, cache.SuffixNone)
	var clientSecret, clientID, tokenURLOverride, accessToken string
	for _, v := range security.EnvVariables {
		if strings.Contains(v, "CLIENT_SECRET") {
			clientSecret = mergedEnv.Get(v)
		} else if strings.Contains(v, "CLIENT_ID") {
			clientID = mergedEnv.Get(v)
		} else if strings.Contains(v, "TOKEN_URL") {
			tokenURLOverride = mergedEnv.Get(v)
		} else if strings.Contains(v, "ACCESS_TOKEN") {
			accessToken = mergedEnv.Get(v)
		}
	}

	if accessToken != "" {
		return accessToken, nil
	}

	if clientSecret == "" {
		return "", fmt.Errorf("missing client secret for client credentials")
	}

	if clientID == "" {
		return "", fmt.Errorf("missing client id for client credentials")
	}

	var requestedScopes []string
	if scopes, ok := planScopes[security.Key]; ok {
		requestedScopes = scopes
	}

	var oauthFlows oAuthFlows
	if err := json.Unmarshal(security.OAuthFlows, &oauthFlows); err != nil {
		return "", fmt.Errorf("failed to unmarshal oauth flows for client credentials: %w", err)
	}

	if oauthFlows.ClientCredentials == nil {
		return "", fmt.Errorf("no client credentials flow found")
	}

	tokenURL := oauthFlows.ClientCredentials.TokenUrl
	if strings.HasPrefix(tokenURL, "/") {
		tokenURL = strings.TrimRight(serverURL, "/") + tokenURL
	}

	if tokenURLOverride != "" {
		tokenURL = tokenURLOverride
	}

	if tokenURL == "" {
		return "", fmt.Errorf("no client credentials token url found")
	}

	cacheKey := clientCredentialsTokenCacheCacheKey(tool.ProjectID, clientID, tokenURL, requestedScopes)
	if token := getTokenFromCache(ctx, logger, tokenCache, cacheKey); token != "" {
		req.Header.Set("Authorization", formatForBearer(token))
		return token, nil
	}

	// Prepare the token request
	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	values.Set("client_id", clientID)
	values.Set("client_secret", clientSecret)
	if len(requestedScopes) > 0 {
		values.Set("scope", strings.Join(requestedScopes, " "))
	}

	data := strings.NewReader(values.Encode())

	tokenReq, err := http.NewRequestWithContext(ctx, "POST", tokenURL, data)
	if err != nil {
		return "", fmt.Errorf("failed to create client credentials token request: %w", err)
	}

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Make the token request
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: otelhttp.NewTransport(
			http.DefaultTransport,
			otelhttp.WithPropagators(propagation.TraceContext{}),
		),
	}
	resp, err := client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to make client credentials token request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(closeErr))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Retry with basic auth if we get 401, 403, or 400
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
			logger.InfoContext(ctx, "retrying client credentials token request with basic auth")

			// Close the original response since we're done with it
			if closeErr := resp.Body.Close(); closeErr != nil {
				logger.ErrorContext(ctx, "failed to close original response body", attr.SlogError(closeErr))
			}

			retryResp, retryErr := retryTokenRequestWithBasicAuth(ctx, client, tokenURL, clientID, clientSecret, requestedScopes)
			if retryErr != nil {
				return "", fmt.Errorf("failed to make client credentials token request: %w", retryErr)
			}

			resp = retryResp
		}

		// Check final response status (either original or retry)
		if resp.StatusCode != http.StatusOK {
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return "", fmt.Errorf("failed to make client credentials token request: status %d, failed to read response body: %w", resp.StatusCode, err)
			}

			return "", fmt.Errorf("failed to make client credentials token request: status %d, response: %s", resp.StatusCode, string(bodyBytes))
		}
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read client credentials token response body: %w", err)
	}

	accessToken, expiresIn, err := parseClientCredentialsTokenResponse(bodyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse client credentials token response: %w", err)
	}

	// If we are passed an expiry value we will use the cache
	if expiresIn > 0 {
		if err := tokenCache.Store(ctx, clientCredentialsTokenCache{
			ProjectID:   tool.ProjectID,
			ClientID:    clientID,
			TokenURL:    tokenURL,
			AccessToken: accessToken,
			Scopes:      requestedScopes,
			ExpiresIn:   time.Duration(expiresIn) * time.Second,
			CreatedAt:   time.Now(),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to store client credentials token in cache", attr.SlogError(err))
		}
	}

	return accessToken, nil
}

// Technically the OAuth spec does expect snake_case field names in the response but we will be generous to mistakes and try with camelCase
func parseClientCredentialsTokenResponse(body []byte) (string, int, error) {
	accessToken := ""
	expiresIn := 0
	var tokenResponse clientCredentialsTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", 0, fmt.Errorf("failed to decode client credentials token response: %w", err)
	}

	accessToken = tokenResponse.AccessToken
	expiresIn = tokenResponse.ExpiresIn

	if accessToken == "" {
		var tokenResponseCamelCase clientCredentialsTokenResponseCamelCase
		if err := json.Unmarshal(body, &tokenResponseCamelCase); err != nil {
			return "", 0, fmt.Errorf("failed to decode client credentials token response: %w", err)
		}
		accessToken = tokenResponseCamelCase.AccessToken
		expiresIn = tokenResponseCamelCase.ExpiresIn
	}

	if accessToken == "" {
		return "", 0, fmt.Errorf("no access token in client credentials token response")
	}

	return accessToken, expiresIn, nil
}

func retryTokenRequestWithBasicAuth(ctx context.Context, client *http.Client, tokenURL, clientID, clientSecret string, requestedScopes []string) (*http.Response, error) {
	values := url.Values{}
	values.Set("grant_type", "client_credentials")
	if len(requestedScopes) > 0 {
		values.Set("scope", strings.Join(requestedScopes, " "))
	}

	data := strings.NewReader(values.Encode())

	retryReq, err := http.NewRequestWithContext(ctx, "POST", tokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("failed to create retry token request: %w", err)
	}

	retryReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	retryReq.SetBasicAuth(clientID, clientSecret)

	resp, err := client.Do(retryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make retry token request: %w", err)
	}

	return resp, nil
}

func getTokenFromCache(ctx context.Context, logger *slog.Logger, tokenCache cache.TypedCacheObject[clientCredentialsTokenCache], cacheKey string) string {
	token := ""
	cachedToken, err := tokenCache.Get(ctx, cacheKey)
	if err != nil {
		return ""
	}

	token = cachedToken.AccessToken
	// we do this in case the underlying cache implementation does not support TTL
	if time.Since(cachedToken.CreatedAt) > cachedToken.ExpiresIn {
		if err := tokenCache.Delete(ctx, clientCredentialsTokenCache{
			ProjectID:   cachedToken.ProjectID,
			ClientID:    cachedToken.ClientID,
			TokenURL:    cachedToken.TokenURL,
			AccessToken: cachedToken.AccessToken,
			Scopes:      cachedToken.Scopes,
			ExpiresIn:   cachedToken.ExpiresIn,
			CreatedAt:   cachedToken.CreatedAt,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to delete expired client credentials token from cache", attr.SlogError(err))
		}

		return ""
	}

	return token
}

func formatForBearer(token string) string {
	if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = "Bearer " + token
	}
	return token
}
