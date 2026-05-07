package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// ContextWindowResolver returns the smallest context_length advertised by any
// OpenRouter endpoint for a model. The minimum is used so callers can rely on
// the value as a safe upper bound regardless of which provider OpenRouter
// routes the request to.
type ContextWindowResolver struct {
	logger     *slog.Logger
	httpClient *guardian.HTTPClient
	cache      cache.TypedCacheObject[mv.ModelContextWindow]
	baseURL    string
}

func NewContextWindowResolver(logger *slog.Logger, guardianPolicy *guardian.Policy, cacheImpl cache.Cache) *ContextWindowResolver {
	component := logger.With(attr.SlogComponent("openrouter_context_window"))
	return &ContextWindowResolver{
		logger:     component,
		httpClient: guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig()),
		cache:      cache.NewTypedObjectCache[mv.ModelContextWindow](component.With(attr.SlogCacheNamespace("openrouter_context_window")), cacheImpl, cache.SuffixNone),
		baseURL:    OpenRouterBaseURL,
	}
}

func (r *ContextWindowResolver) Resolve(ctx context.Context, modelID string) (int, error) {
	if cached, err := r.cache.Get(ctx, mv.ModelContextWindowCacheKey(modelID)); err == nil {
		return cached.Tokens, nil
	}

	tokens, err := r.fetchMin(ctx, modelID)
	if err != nil {
		return 0, err
	}

	if err := r.cache.Store(ctx, mv.ModelContextWindow{ID: modelID, Tokens: tokens}); err != nil {
		r.logger.WarnContext(ctx, "failed to cache model context window", attr.SlogError(err), attr.SlogGenAIRequestModel(modelID))
	}

	return tokens, nil
}

type endpointInfo struct {
	ProviderName  string `json:"provider_name"`
	ContextLength int    `json:"context_length"`
}

type endpointsResponse struct {
	Data struct {
		Endpoints []endpointInfo `json:"endpoints"`
	} `json:"data"`
}

func (r *ContextWindowResolver) fetchMin(ctx context.Context, modelID string) (int, error) {
	author, slug, ok := strings.Cut(modelID, "/")
	if !ok || author == "" || slug == "" {
		return 0, fmt.Errorf("invalid model id %q: expected <author>/<slug>", modelID)
	}

	endpoint := fmt.Sprintf("%s/v1/models/%s/%s/endpoints", r.baseURL, url.PathEscape(author), url.PathEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("build endpoints request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send endpoints request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("openrouter endpoints (status %d) for model %s", resp.StatusCode, modelID)
	}

	var decoded endpointsResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return 0, fmt.Errorf("decode endpoints response: %w", err)
	}

	if len(decoded.Data.Endpoints) == 0 {
		return 0, errors.New("no endpoints returned for model " + modelID)
	}

	minTokens := 0
	for _, ep := range decoded.Data.Endpoints {
		if ep.ContextLength <= 0 {
			continue
		}
		if minTokens == 0 || ep.ContextLength < minTokens {
			minTokens = ep.ContextLength
		}
	}

	if minTokens == 0 {
		return 0, errors.New("no positive context_length found for model " + modelID)
	}

	return minTokens, nil
}
