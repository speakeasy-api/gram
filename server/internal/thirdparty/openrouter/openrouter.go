package openrouter

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

var allowList = map[string]bool{
	"anthropic/claude-sonnet-4.5":   true,
	"anthropic/claude-haiku-4.5":    true,
	"anthropic/claude-sonnet-4":     true,
	"openai/gpt-4o":                 true,
	"openai/gpt-4o-mini":            true,
	"openai/gpt-5":                  true,
	"openai/gpt-4.1":                true,
	"anthropic/claude-3.7-sonnet":   true,
	"anthropic/claude-opus-4":       true,
	"google/gemini-2.5-pro-preview": true,
	"moonshotai/kimi-k2":            true,
	"mistralai/mistral-medium-3":    true,
	"mistralai/codestral-2501":      true,
}

// IsModelAllowed checks if a model is in the allowlist
func IsModelAllowed(model string) bool {
	return allowList[model]
}

var creditsAccountTypeMap = map[string]int{
	"free":       5,
	"pro":        25,
	"enterprise": 50,
	"":           5, // safety default
}

var specialLimitOrgs = []string{
	"5a25158b-24dc-4d49-b03d-e85acfbea59c", // speakeasy-team
}

type Provisioner interface {
	ProvisionAPIKey(ctx context.Context, orgID string) (string, error)
	RefreshAPIKeyLimit(ctx context.Context, orgID string) (int, error)
	GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error)
	GetModelPricing(ctx context.Context, canonicalSlug string) (*mv.ModelPricing, error)
	FetchAndCacheModelPricing(ctx context.Context) error
}

type KeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error
}

type OpenRouter struct {
	provisioningKey   string
	env               string
	logger            *slog.Logger
	repo              *repo.Queries
	orgRepo           *orgRepo.Queries
	orClient          *http.Client
	refresher         KeyRefresher
	modelPricingCache cache.TypedCacheObject[mv.ModelPricing]
}

func New(logger *slog.Logger, db *pgxpool.Pool, env string, provisioningKey string, refresher KeyRefresher, cacheImpl cache.Cache) *OpenRouter {
	return &OpenRouter{
		provisioningKey:   provisioningKey,
		env:               env,
		logger:            logger,
		repo:              repo.New(db),
		orgRepo:           orgRepo.New(db),
		orClient:          retryablehttp.NewClient().StandardClient(),
		refresher:         refresher,
		modelPricingCache: cache.NewTypedObjectCache[mv.ModelPricing](logger.With(attr.SlogCacheNamespace("model_pricing")), cacheImpl, cache.SuffixNone),
	}
}

func (o *OpenRouter) ProvisionAPIKey(ctx context.Context, orgID string) (string, error) {
	var openrouterKey string

	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	switch {
	case errors.Is(err, sql.ErrNoRows), key.Key == "":
		org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
		}

		creditAmount := getLimitForOrg(org)

		keyResponse, err := o.createOpenRouterAPIKey(ctx, orgID, org.Slug, creditAmount)
		if err != nil {
			return "", err
		}

		_, err = o.repo.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
			OrganizationID: orgID,
			Key:            *keyResponse.Key,
			KeyHash:        keyResponse.Data.Hash,
			MonthlyCredits: int64(creditAmount),
		})
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to store openrouter key data").Log(ctx, o.logger)
		}

		if o.refresher != nil {
			if err := o.refresher.ScheduleOpenRouterKeyRefresh(ctx, orgID); err != nil {
				return "", oops.E(oops.CodeUnexpected, err, "error scheduling open router key refresh").Log(ctx, o.logger)
			}
		}

		openrouterKey = *keyResponse.Key

	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").Log(ctx, o.logger)

	default:
		openrouterKey = key.Key
	}

	if err := inv.Check("openrouter provisioning", "key is set", openrouterKey != ""); err != nil {
		return "", fmt.Errorf("assertion error: %w", err)
	}

	return openrouterKey, nil
}

func (o *OpenRouter) RefreshAPIKeyLimit(ctx context.Context, orgID string) (int, error) {
	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("failed to get OpenRouter API key: %w", err)
	}

	if key.MonthlyCredits == 0 && !key.Disabled {
		return 0, errors.New("cannot make an update to monthly credits of 0")
	}

	previousKeyHash := key.KeyHash

	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
	}

	limit := getLimitForOrg(org)

	keyResponse, err := o.createOpenRouterAPIKey(ctx, orgID, org.Slug, limit)
	if err != nil {
		return 0, err
	}

	_, err = o.repo.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID,
		MonthlyCredits: int64(limit),
		KeyHash:        keyResponse.Data.Hash,
		Key:            *keyResponse.Key,
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to update openrouter key").Log(ctx, o.logger)
	}

	if err := o.deleteOpenRouterAPIKey(ctx, previousKeyHash); err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to clean up previous openrouter key").Log(ctx, o.logger)
	}

	return limit, nil
}

type keyUsageResponse struct {
	Data struct {
		Limit *float64 `json:"limit"`
		Usage *float64 `json:"usage"`
	} `json:"data"`
}

func (o *OpenRouter) GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error) {
	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
	}
	limit := getLimitForOrg(org)

	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	if err != nil {
		return 0, limit, nil // the key doesn't exist yet
	}

	req, err := http.NewRequestWithContext(ctx, "GET", OpenRouterBaseURL+"/v1/key", nil)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to get openrouter key HTTP request", attr.SlogError(err))
		return 0, limit, fmt.Errorf("failed to get key request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", attr.SlogError(err))
		return 0, limit, fmt.Errorf("failed to send update key request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return 0, limit, errors.New("failed to update OpenRouter API key: " + resp.Status)
	}

	var usageResp keyUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode key usage response", attr.SlogError(err))
		return 0, limit, fmt.Errorf("failed to decode key usage response: %w", err)
	}

	var creditsUsed float64
	if usageResp.Data.Usage != nil {
		creditsUsed = math.Round(*usageResp.Data.Usage*100) / 100
	}

	return creditsUsed, limit, nil
}

func getLimitForOrg(org orgRepo.OrganizationMetadatum) int {
	if slices.Contains(specialLimitOrgs, org.ID) {
		return 500
	}
	return creditsAccountTypeMap[org.GramAccountType]
}

type createKeyRequest struct {
	Name  string   `json:"name"`
	Label string   `json:"label"`
	Limit *float64 `json:"limit,omitempty"`
}

type keyResponse struct {
	Data struct {
		Limit float64 `json:"limit"`
		Hash  string  `json:"hash"`
	} `json:"data"`
	Key *string `json:"key,omitempty"` // will be empty outside of createKey
}

func (o *OpenRouter) createOpenRouterAPIKey(ctx context.Context, orgID string, orgSlug string, keyLimit int) (*keyResponse, error) {
	creditLimit := float64(keyLimit)
	requestBody := createKeyRequest{Name: fmt.Sprintf("gram-%s-%s", o.env, orgID), Label: fmt.Sprintf("%s (%s environment)", orgSlug, o.env), Limit: &creditLimit}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to marshal create openrouter key request body", attr.SlogError(err))
		return nil, fmt.Errorf("failed to serialize create key request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", OpenRouterBaseURL+"/v1/keys", bytes.NewReader(bodyBytes))
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to create openrouter key HTTP request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to build create key request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to send create key request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, errors.New("failed to create OpenRouter API key: " + resp.Status)
	}

	var response keyResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode create openrouter key response body", attr.SlogError(err))
		return nil, fmt.Errorf("failed to decode create openrouter key response body: %w", err)
	}

	if response.Key == nil {
		o.logger.ErrorContext(ctx, "missing key in OpenRouter response")
		return nil, errors.New("missing key in OpenRouter response")
	}

	return &response, nil
}

func (o *OpenRouter) deleteOpenRouterAPIKey(ctx context.Context, keyHash string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", OpenRouterBaseURL+fmt.Sprintf("/v1/keys/%s", keyHash), nil)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to delete openrouter key HTTP request", attr.SlogError(err))
		return fmt.Errorf("failed to create delete key request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", attr.SlogError(err))
		return fmt.Errorf("failed to send delete key request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return errors.New("failed to update OpenRouter API key: " + resp.Status)
	}

	return nil
}

// modelPricingResponse represents pricing information from the OpenRouter API response
type modelPricingResponse struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
	Request    string `json:"request"`
	Image      string `json:"image"`
}

// ModelInfo represents information about an OpenRouter model
type ModelInfo struct {
	ID            string               `json:"id"`
	CanonicalSlug string               `json:"canonical_slug"`
	Name          string               `json:"name"`
	Pricing       modelPricingResponse `json:"pricing"`
	ContextLength int                  `json:"context_length"`
	Created       int64                `json:"created"`
}

// ModelsResponse represents the response from OpenRouter /v1/models endpoint
type ModelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// FetchAndCacheModelPricing fetches model pricing data from OpenRouter API and stores it in Redis cache.
// Each model's pricing is stored with a key based on its canonical slug.
func (o *OpenRouter) FetchAndCacheModelPricing(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", OpenRouterBaseURL+"/v1/models", nil)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to create openrouter models HTTP request", attr.SlogError(err))
		return fmt.Errorf("failed to create models request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request to fetch models", attr.SlogError(err))
		return fmt.Errorf("failed to send models request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		o.logger.ErrorContext(ctx, "failed to fetch models from OpenRouter")
		return fmt.Errorf("failed to fetch models from OpenRouter: %s", resp.Status)
	}

	var modelsResp ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode models response", attr.SlogError(err))
		return fmt.Errorf("failed to decode models response: %w", err)
	}

	// Cache pricing data for each model using canonical slug as key
	for _, model := range modelsResp.Data {
		if model.CanonicalSlug == "" {
			o.logger.WarnContext(ctx, "skipping model with empty canonical slug")
			continue
		}

		pricing := mv.ModelPricing{
			CanonicalSlug: model.CanonicalSlug,
			Prompt:        model.Pricing.Prompt,
			Completion:    model.Pricing.Completion,
			Request:       model.Pricing.Request,
			Image:         model.Pricing.Image,
		}

		if err := o.modelPricingCache.Store(ctx, pricing); err != nil {
			o.logger.ErrorContext(ctx, "failed to cache model pricing",
				attr.SlogError(err))
			// Continue caching other models even if one fails
			continue
		}

		o.logger.DebugContext(ctx, "cached model pricing")
	}

	o.logger.InfoContext(ctx, "successfully fetched and cached model pricing")

	return nil
}

// GetModelPricing retrieves pricing data for a model from Redis cache using its canonical slug.
// Returns an error if the pricing data is not found in cache or if cache is not configured.
func (o *OpenRouter) GetModelPricing(ctx context.Context, canonicalSlug string) (*mv.ModelPricing, error) {
	if canonicalSlug == "" {
		return nil, errors.New("canonical slug is required")
	}

	cacheKey := mv.ModelPricingCacheKey(canonicalSlug)
	pricing, err := o.modelPricingCache.Get(ctx, cacheKey)
	if err != nil {
		o.logger.DebugContext(ctx, "model pricing not found in cache",
			attr.SlogError(err))
		return nil, fmt.Errorf("model pricing not found for canonical slug %s: %w", canonicalSlug, err)
	}

	return &pricing, nil
}
