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
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

// ErrGenerationNotFound is returned when the generation details are not found after retries.
// This typically means the generation data hasn't propagated yet and may be available later.
var ErrGenerationNotFound = errors.New("generation not found")

// Just a general allowlist for models we allow to proxy through us for playground usage, chat, or agentic usecases
// This list can stay sufficiently robust, we should just need to allow list a model before it goes through us
var allowList = map[string]bool{
	"anthropic/claude-sonnet-4.5":   true,
	"anthropic/claude-haiku-4.5":    true,
	"anthropic/claude-sonnet-4":     true,
	"anthropic/claude-opus-4.5":     true,
	"openai/gpt-4o":                 true,
	"openai/gpt-4o-mini":            true,
	"openai/gpt-5.1-codex":          true,
	"openai/gpt-5":                  true,
	"openai/gpt-5.1":                true,
	"openai/gpt-4.1":                true,
	"anthropic/claude-3.7-sonnet":   true,
	"anthropic/claude-opus-4":       true,
	"google/gemini-2.5-pro-preview": true,
	"google/gemini-3-pro-preview":   true,
	"moonshotai/kimi-k2":            true,
	"mistralai/mistral-medium-3":    true,
	"mistralai/mistral-medium-3.1":  true,
	"mistralai/codestral-2501":      true,
}

// IsModelAllowed checks if a model is in the allowlist
func IsModelAllowed(model string) bool {
	return allowList[model]
}

// default credit limits per acccount type
// this can always be customized per org in the DB
// or via running OpenrouterKeyRefreshWorkflow {OrgID: "abc123", Limit: new_monthly_limit} in temporal directly
var creditsAccountTypeMap = map[string]int{
	"free":       5,
	"pro":        100,
	"enterprise": 100,
	"":           5, // safety default
}

var specialLimitOrgs = []string{
	"5a25158b-24dc-4d49-b03d-e85acfbea59c", // speakeasy-team
}

type Provisioner interface {
	ProvisionAPIKey(ctx context.Context, orgID string) (string, error)
	RefreshAPIKeyLimit(ctx context.Context, orgID string, limit *int) (int, error)
	GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error)
	TriggerModelUsageTracking(ctx context.Context, generationID string, orgID string, projectID string, source billing.ModelUsageSource, chatID string) error
}

type KeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string) error
}

type OpenRouter struct {
	provisioningKey string
	env             string
	logger          *slog.Logger
	repo            *repo.Queries
	orgRepo         *orgRepo.Queries
	orClient        *http.Client
	refresher       KeyRefresher
	featureClient   *productfeatures.Client
	tracking        billing.Tracker
	posthog         *posthog.Posthog
}

func New(logger *slog.Logger, db *pgxpool.Pool, env string, provisioningKey string, refresher KeyRefresher, featureClient *productfeatures.Client, tracking billing.Tracker, posthog *posthog.Posthog) *OpenRouter {
	return &OpenRouter{
		provisioningKey: provisioningKey,
		env:             env,
		logger:          logger,
		repo:            repo.New(db),
		orgRepo:         orgRepo.New(db),
		orClient:        retryablehttp.NewClient().StandardClient(),
		refresher:       refresher,
		featureClient:   featureClient,
		tracking:        tracking,
		posthog:         posthog,
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

		creditAmount := o.getLimitForOrg(org)

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

func (o *OpenRouter) RefreshAPIKeyLimit(ctx context.Context, orgID string, limit *int) (int, error) {
	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	if err != nil {
		return 0, fmt.Errorf("failed to get OpenRouter API key: %w", err)
	}

	if key.MonthlyCredits == 0 && !key.Disabled {
		return 0, errors.New("cannot make an update to monthly credits of 0")
	}

	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
	}

	var keyLimit int
	if limit != nil {
		keyLimit = *limit
	} else {
		keyLimit = o.getLimitForOrg(org)
	}

	keyResponse, err := o.updateOpenRouterAPIKeyLimit(ctx, key.KeyHash, keyLimit)
	if err != nil {
		return 0, err
	}

	_, err = o.repo.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID,
		MonthlyCredits: int64(keyLimit),
		KeyHash:        keyResponse.Data.Hash,
		Key:            key.Key,
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to update openrouter key").Log(ctx, o.logger)
	}

	return keyLimit, nil
}

type keyUsageResponse struct {
	Data struct {
		Limit        *float64 `json:"limit"`
		UsageMonthly *float64 `json:"usage_monthly"`
	} `json:"data"`
}

type generationData struct {
	ID                    string  `json:"id"`
	TotalCost             float64 `json:"total_cost"`
	CacheDiscount         float64 `json:"cache_discount"`
	UpstreamInferenceCost float64 `json:"upstream_inference_cost"`
	Model                 string  `json:"model"`
	TokensPrompt          int     `json:"tokens_prompt"`
	TokensCompletion      int     `json:"tokens_completion"`
	NativeTokensReasoning int     `json:"native_tokens_reasoning"`
	NativeTokensCached    int     `json:"native_tokens_cached"`
	APIType               string  `json:"api_type"`
}

type generationResponse struct {
	Data generationData `json:"data"`
}

func (o *OpenRouter) GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error) {
	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
	}
	limit := o.getLimitForOrg(org)

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
	if usageResp.Data.UsageMonthly != nil {
		creditsUsed = math.Round(*usageResp.Data.UsageMonthly*100) / 100
	}

	return creditsUsed, limit, nil
}

func (o *OpenRouter) getLimitForOrg(org orgRepo.OrganizationMetadatum) int {
	if slices.Contains(specialLimitOrgs, org.ID) {
		return 500
	}

	return creditsAccountTypeMap[org.GramAccountType]
}

type createKeyRequest struct {
	Name       string   `json:"name"`
	Label      string   `json:"label"`
	Limit      *float64 `json:"limit,omitempty"`
	LimitReset string   `json:"limit_reset,omitempty"`
}

type updateKeyRequest struct {
	Limit      *float64 `json:"limit,omitempty"`
	LimitReset string   `json:"limit_reset,omitempty"`
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
	requestBody := createKeyRequest{
		Name:       fmt.Sprintf("gram-%s-%s", o.env, orgID),
		Label:      fmt.Sprintf("%s (%s environment)", orgSlug, o.env),
		Limit:      &creditLimit,
		LimitReset: "monthly",
	}

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

func (o *OpenRouter) updateOpenRouterAPIKeyLimit(ctx context.Context, keyHash string, keyLimit int) (*keyResponse, error) {
	creditLimit := float64(keyLimit)
	requestBody := updateKeyRequest{Limit: &creditLimit, LimitReset: "monthly"}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to marshal update openrouter key request body", attr.SlogError(err))
		return nil, fmt.Errorf("failed to serialize update key request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", OpenRouterBaseURL+fmt.Sprintf("/v1/keys/%s", keyHash), bytes.NewReader(bodyBytes))
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to create update openrouter key HTTP request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to create update key request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+o.provisioningKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send HTTP request", attr.SlogError(err))
		return nil, fmt.Errorf("failed to send update key request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to update OpenRouter API key limit: " + resp.Status)
	}

	var response keyResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode update openrouter key response body", attr.SlogError(err))
		return nil, fmt.Errorf("failed to decode update openrouter key response body: %w", err)
	}

	return &response, nil
}

func (o *OpenRouter) getGenerationDetails(ctx context.Context, generationID string, orgID string) (*generationResponse, int, error) {
	key, err := o.repo.GetOpenRouterAPIKey(ctx, orgID)
	if err != nil {
		return nil, 0, oops.E(oops.CodeUnexpected, err, "failed to get openrouter API key").Log(ctx, o.logger)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", OpenRouterBaseURL+"/v1/generation", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create generation request: %w", err)
	}

	q := req.URL.Query()
	q.Set("id", generationID)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to send generation request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("failed to fetch generation from OpenRouter: %s", resp.Status)
	}

	var genResp generationResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to decode generation response: %w", err)
	}

	return &genResp, resp.StatusCode, nil
}

// TriggerModelUsageTracking fetches generation details from OpenRouter and tracks model usage.
func (o *OpenRouter) TriggerModelUsageTracking(
	ctx context.Context,
	generationID string,
	orgID string,
	projectID string,
	source billing.ModelUsageSource,
	chatID string,
) error {
	var genResp *generationResponse
	var statusCode int
	var err error

	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get organization").Log(ctx, o.logger)
	}

	// The generation is typically not available synchronously with the chat completion but becomes available quickly.
	// Temporal could handle reliability here, but given we don't want to move this action to temporal right now,
	// this simple retry backoff will be effective enough.
	backoffs := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	for attempt := range backoffs {
		genResp, statusCode, err = o.getGenerationDetails(ctx, generationID, orgID)
		if err == nil {
			break
		}

		// Retry on 404 (generation not found yet)
		if statusCode == http.StatusNotFound && attempt < len(backoffs)-1 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled while fetching generation details: %w", ctx.Err())
			case <-time.After(backoffs[attempt]):
				continue
			}
		}

		if statusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrGenerationNotFound, err.Error())
		}
		return err
	}

	if err != nil {
		if statusCode == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrGenerationNotFound, err.Error())
		}
		return err
	}

	var cost *float64
	if genResp.Data.TotalCost > 0 {
		cost = &genResp.Data.TotalCost
	} else {
		o.logger.ErrorContext(ctx, "no cost found in generation response",
			attr.SlogError(fmt.Errorf("total_cost is %f", genResp.Data.TotalCost)),
			attr.SlogOrganizationID(orgID),
		)
	}

	event := billing.ModelUsageEvent{
		OrganizationID:        orgID,
		ProjectID:             projectID,
		Source:                source,
		ChatID:                chatID,
		Model:                 genResp.Data.Model,
		InputTokens:           int64(genResp.Data.TokensPrompt),
		OutputTokens:          int64(genResp.Data.TokensCompletion),
		TotalTokens:           int64(genResp.Data.TokensPrompt + genResp.Data.TokensCompletion),
		Cost:                  cost,
		NativeTokensCached:    int64(genResp.Data.NativeTokensCached),
		NativeTokensReasoning: int64(genResp.Data.NativeTokensReasoning),
		CacheDiscount:         genResp.Data.CacheDiscount,
		UpstreamInferenceCost: genResp.Data.UpstreamInferenceCost,
	}

	o.tracking.TrackModelUsage(ctx, event)

	if err := o.posthog.CaptureEvent(ctx, "model_usage", orgID, map[string]interface{}{
		"model":                   event.Model,
		"cost":                    event.Cost,
		"source":                  string(event.Source),
		"organization_slug":       org.Slug,
		"organization_id":         event.OrganizationID,
		"project_id":              event.ProjectID,
		"chat_id":                 event.ChatID,
		"input_tokens":            event.InputTokens,
		"output_tokens":           event.OutputTokens,
		"total_tokens":            event.TotalTokens,
		"native_tokens_cached":    event.NativeTokensCached,
		"native_tokens_reasoning": event.NativeTokensReasoning,
		"cache_discount":          event.CacheDiscount,
		"upstream_inference_cost": event.UpstreamInferenceCost,
	}); err != nil {
		o.logger.ErrorContext(ctx, "failed to capture model usage event for posthog", attr.SlogError(err))
	}

	return nil
}
