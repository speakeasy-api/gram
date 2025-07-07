package instances

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

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func processSecurity(ctx context.Context, logger *slog.Logger, req *http.Request, w http.ResponseWriter, responseStatusCodeCapture *int, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, cacheImpl cache.Cache, envVars *caseInsensitiveEnv, serverURL string) {
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
					req.Header.Set("Authorization", formatForBearer(token))
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
		case "oauth2":
			if security.OauthTypes == nil {
				logger.ErrorContext(ctx, "no oauth types provided for oauth2 auth", slog.String("scheme", security.Scheme.String))
			}

			for _, oauthType := range security.OauthTypes {
				switch oauthType {
				case "authorization_code":
					for _, envVar := range security.EnvVariables {
						if strings.Contains(envVar, "ACCESS_TOKEN") {
							if token := envVars.Get(envVar); token == "" {
								logger.ErrorContext(ctx, "missing authorization code", slog.String("env_var", envVar))
							} else {
								req.Header.Set("Authorization", formatForBearer(token))
							}
						}
					}
				case "client_credentials":
					if err := processClientCredentials(ctx, logger, req, cacheImpl, toolExecutionInfo, security, envVars, serverURL); err != nil {
						logger.ErrorContext(ctx, "could not process client credentials", slog.String("error", err.Error()))
						if strings.Contains(err.Error(), "failed to make client credentials token request") {
							w.Header().Set("Content-Type", "application/json")
							w.WriteHeader(http.StatusUnauthorized)
							if responseStatusCodeCapture != nil {
								*responseStatusCodeCapture = http.StatusUnauthorized
							}
							if err := json.NewEncoder(w).Encode(toolcallErrorSchema{
								Error: err.Error(),
							}); err != nil {
								logger.ErrorContext(ctx, "failed to encode tool call error", slog.String("error", err.Error()))
							}
							return
						}
					}
				}
			}
		default:
			logger.ErrorContext(ctx, "unsupported security scheme type", slog.String("type", security.Type.String))
			continue
		}
	}
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

func processClientCredentials(ctx context.Context, logger *slog.Logger, req *http.Request, cacheImpl cache.Cache, toolExecutionInfo *toolsets.HTTPToolExecutionInfo, security repo.HttpSecurity, envVars *caseInsensitiveEnv, serverURL string) error {
	// To discuss, currently we are taking the approach of exact scope match for reused tokens
	// We could look into enabling a prefix match feature for caches where we return multiple entries matching the projectID, clientID, tokenURL and then check scopes against all returned values
	// We would want to make sure any underlying cache implementation supports this feature
	tokenCache := cache.NewTypedObjectCache[clientCredentialsTokenCache](logger.With(slog.String("cache", "client_credentials_token_cache")), cacheImpl, cache.SuffixNone)
	var clientSecret, clientID, tokenURLOverride, accessToken string
	for _, v := range security.EnvVariables {
		if strings.Contains(v, "CLIENT_SECRET") {
			clientSecret = envVars.Get(v)
		} else if strings.Contains(v, "CLIENT_ID") {
			clientID = envVars.Get(v)
		} else if strings.Contains(v, "TOKEN_URL") {
			tokenURLOverride = envVars.Get(v)
		} else if strings.Contains(v, "ACCESS_TOKEN") {
			accessToken = envVars.Get(v)
		}
	}

	if accessToken != "" {
		req.Header.Set("Authorization", formatForBearer(accessToken))
		return nil
	}

	if clientSecret == "" {
		return fmt.Errorf("missing client secret for client credentials")
	}

	if clientID == "" {
		return fmt.Errorf("missing client id for client credentials")
	}

	var requestedScopes []string
	if scopes, ok := toolExecutionInfo.SecurityScopes[security.Key]; ok {
		requestedScopes = scopes
	}

	var oauthFlows oAuthFlows
	if err := json.Unmarshal(security.OauthFlows, &oauthFlows); err != nil {
		return fmt.Errorf("failed to unmarshal oauth flows for client credentials: %w", err)
	}

	if oauthFlows.ClientCredentials == nil {
		return fmt.Errorf("no client credentials flow found")
	}

	tokenURL := oauthFlows.ClientCredentials.TokenUrl
	if strings.HasPrefix(tokenURL, "/") {
		tokenURL = strings.TrimRight(serverURL, "/") + tokenURL
	}

	if tokenURLOverride != "" {
		tokenURL = tokenURLOverride
	}

	if tokenURL == "" {
		return fmt.Errorf("no client credentials token url found")
	}

	cacheKey := clientCredentialsTokenCacheCacheKey(toolExecutionInfo.Tool.ProjectID.String(), clientID, tokenURL, requestedScopes)
	if token := getTokenFromCache(ctx, logger, tokenCache, cacheKey); token != "" {
		req.Header.Set("Authorization", formatForBearer(token))
		return nil
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
		return fmt.Errorf("failed to create client credentials token request: %w", err)
	}

	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Make the token request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("failed to make client credentials token request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			logger.ErrorContext(ctx, "failed to close response body", slog.String("error", closeErr.Error()))
		}
	}()

	if resp.StatusCode != http.StatusOK {
		// Read the response body for error details
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to make client credentials token request: status %d, failed to read response body: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("failed to make client credentials token request: status %d, response: %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResponse clientCredentialsTokenResponse

	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return fmt.Errorf("failed to decode client credentials token response: %w", err)
	}

	if tokenResponse.AccessToken == "" {
		return fmt.Errorf("no access token in client credentials token response")
	}

	// If we are passed an expiry value we will use the cache
	if tokenResponse.ExpiresIn > 0 {
		if err := tokenCache.Store(ctx, clientCredentialsTokenCache{
			ProjectID:   toolExecutionInfo.Tool.ProjectID.String(),
			ClientID:    clientID,
			TokenURL:    tokenURL,
			AccessToken: tokenResponse.AccessToken,
			Scopes:      requestedScopes,
			ExpiresIn:   time.Duration(tokenResponse.ExpiresIn) * time.Second,
			CreatedAt:   time.Now(),
		}); err != nil {
			logger.ErrorContext(ctx, "failed to store client credentials token in cache", slog.String("error", err.Error()))
		}
	}

	req.Header.Set("Authorization", formatForBearer(tokenResponse.AccessToken))

	return nil
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
		logger.InfoContext(ctx, "cached client credentials token has expired", slog.String("cache_key", cacheKey))
		if err := tokenCache.Delete(ctx, clientCredentialsTokenCache{
			ProjectID:   cachedToken.ProjectID,
			ClientID:    cachedToken.ClientID,
			TokenURL:    cachedToken.TokenURL,
			AccessToken: cachedToken.AccessToken,
			Scopes:      cachedToken.Scopes,
			ExpiresIn:   cachedToken.ExpiresIn,
			CreatedAt:   cachedToken.CreatedAt,
		}); err != nil {
			logger.ErrorContext(ctx, "failed to delete expired client credentials token from cache", slog.String("error", err.Error()))
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
