package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

// KeyType selects which of an org's provisioned OpenRouter keys pays for a
// request. Each org can hold one key per type (openrouter_api_keys is keyed
// by (organization_id, key_type)): the chat key funds customer-facing
// completion surfaces, while the internal key funds platform-initiated LLM
// usage (risk judges, title generation, chat resolutions, memory) so a burst
// of scanning inference can never exhaust the chat cap and 402 the
// customer's chat surface. Selection is an explicit request field — never
// derived from the usage source, which the completions proxy accepts from
// clients and could be spoofed onto the internal key.
type KeyType string

const (
	KeyTypeChat     KeyType = "chat"
	KeyTypeInternal KeyType = "internal"
)

// AllKeyTypes is the single definition of the valid key-type set. Validate
// and any caller that fans out across an org's keys (e.g. account-type
// limit refreshes) consume it, so adding a key type here propagates without
// hunting call sites.
var AllKeyTypes = []KeyType{KeyTypeChat, KeyTypeInternal}

// upstreamKeyCreateTimeout bounds the POST /v1/keys call made while holding
// the per-(org, key type) provisioning advisory lock.
const upstreamKeyCreateTimeout = 15 * time.Second

// OrDefault resolves the zero value to the chat key, so existing callers
// that never set a key type keep their behavior.
func (k KeyType) OrDefault() KeyType {
	if k == "" {
		return KeyTypeChat
	}
	return k
}

// Validate rejects unknown key types (the zero value counts as chat). The
// allowed values are deliberately enforced here, not with a DB CHECK
// constraint, per this repo's schema conventions — and callers that mint
// rows or pick workflow ids must call it so a typo cannot create a third
// key type under the chat naming pattern or clobber the chat refresh
// workflow id.
func (k KeyType) Validate() error {
	if slices.Contains(AllKeyTypes, k.OrDefault()) {
		return nil
	}
	return fmt.Errorf("invalid openrouter key type %q", string(k))
}

// Just a general allowlist for models we allow to proxy through us for playground usage, chat, or agentic usecases
// This list can stay sufficiently robust, we should just need to allow list a model before it goes through us
var allowList = map[string]bool{
	"anthropic/claude-fable-5":      true,
	"anthropic/claude-sonnet-5":     true,
	"anthropic/claude-opus-4.8":     true,
	"anthropic/claude-opus-4.7":     true,
	"anthropic/claude-sonnet-4.6":   true,
	"anthropic/claude-sonnet-4.5":   true,
	"anthropic/claude-opus-4.6":     true,
	"anthropic/claude-opus-4.5":     true,
	"anthropic/claude-haiku-4.5":    true,
	"openai/gpt-5.6-sol":            true,
	"openai/gpt-5.6-terra":          true,
	"openai/gpt-5.6-luna":           true,
	"openai/gpt-5.5":                true,
	"openai/gpt-5.5-pro":            true,
	"openai/gpt-5.4":                true,
	"openai/gpt-5.4-mini":           true,
	"openai/gpt-5.4-nano":           true,
	"openai/gpt-5.3-codex":          true,
	"openai/gpt-5.1":                true,
	"openai/gpt-5":                  true,
	"google/gemini-3.5-flash":       true,
	"google/gemini-3.1-pro-preview": true,
	"google/gemini-3.1-flash-lite":  true,
	"deepseek/deepseek-v4-pro":      true,
	"deepseek/deepseek-v4-flash":    true,
	"deepseek/deepseek-v3.2":        true,
	"meta-llama/llama-4-maverick":   true,
	"x-ai/grok-4.3":                 true,
	"x-ai/grok-4.20":                true,
	"qwen/qwen3.7-max":              true,
	"qwen/qwen3-coder":              true,
	"moonshotai/kimi-k2.6":          true,
	"moonshotai/kimi-k2.5":          true,
	"mistralai/mistral-medium-3-5":  true,
	"mistralai/codestral-2508":      true,
	"mistralai/devstral-2512":       true,
	"mistralai/mistral-medium-3.1":  true,
}

// IsModelAllowed checks if a model is in the allowlist
func IsModelAllowed(model string) bool {
	return allowList[model]
}

// providerFallbacks pins the model an unknown or de-listed model resolves to,
// per provider. Without this, ResolveModel's alphabetical fallback silently
// upgrades callers to whatever sorts first — for Anthropic that is the
// premium-priced claude-fable-5. Each entry names the provider's
// standard-cost workhorse; keep it allowlisted (enforced by tests).
var providerFallbacks = map[string]string{
	"anthropic":  "anthropic/claude-sonnet-5",
	"openai":     "openai/gpt-5.6-terra",
	"google":     "google/gemini-3.5-flash",
	"deepseek":   "deepseek/deepseek-v4-flash",
	"meta-llama": "meta-llama/llama-4-maverick",
	"x-ai":       "x-ai/grok-4.3",
	"qwen":       "qwen/qwen3.7-max",
	"moonshotai": "moonshotai/kimi-k2.6",
	"mistralai":  "mistralai/mistral-medium-3-5",
}

// ResolveModel returns the model as-is if it's in the allowlist. Otherwise, it
// returns the provider's pinned fallback from providerFallbacks, or — for
// providers without a pin — the first allowed model sorted alphabetically.
// Returns empty string if no fallback is found.
func ResolveModel(model string) string {
	if allowList[model] {
		return model
	}

	provider, _, ok := strings.Cut(model, "/")
	if !ok || provider == "" {
		return ""
	}

	if fallback := providerFallbacks[provider]; fallback != "" && allowList[fallback] {
		return fallback
	}

	prefix := provider + "/"
	var candidates []string
	for m := range allowList {
		if strings.HasPrefix(m, prefix) {
			candidates = append(candidates, m)
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	sort.Strings(candidates)
	return candidates[0]
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

// IsSpecialLimitOrg reports whether the org bypasses standard credit limits.
func IsSpecialLimitOrg(orgID string) bool {
	return slices.Contains(specialLimitOrgs, orgID)
}

type Provisioner interface {
	ProvisionAPIKey(ctx context.Context, orgID string, keyType KeyType) (string, error)

	// RefreshAPIKeyLimit mutates the upstream OpenRouter key limit (PATCH
	// /v1/keys/:hash) and mirrors the new value into the local DB.
	RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error)

	GetCreditsUsed(ctx context.Context, orgID string, keyType KeyType) (float64, int, error)

	// GetKeyUsage issues GET /v1/key for the given API key and returns the
	// rounded monthly usage along with the upstream-configured monthly limit
	// already rounded to the int64 representation used by the DB. The limit is
	// nil when OpenRouter returns an unlimited key.
	GetKeyUsage(ctx context.Context, apiKey string) (used float64, limit *int64, err error)

	// ReconcileMonthlyCredits compares upstreamLimit against the caller-supplied
	// currentLimit (the DB-cached value) and writes the upstream value to the
	// openrouter_api_keys row when they diverge. It is a DB-only reconciliation
	// — it does NOT call OpenRouter — and is intended to self-heal drift
	// introduced by out-of-band edits on the OpenRouter dashboard. A nil
	// upstreamLimit (unlimited key) is treated as a no-op. Returns the
	// effective limit the caller should use for the current tick.
	ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType KeyType, currentLimit int64, upstreamLimit *int64) (int64, error)

	// GetModelUsage fetches generation usage by ID. Normal completion paths use
	// inline usage; this is only a fallback for streams closed before the final
	// usage chunk arrives. A generation is only visible under the key that made
	// it, so the caller must name the same key type the completion used.
	GetModelUsage(ctx context.Context, generationID string, orgID string, keyType KeyType) (*ModelUsage, error)
}

type KeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string, keyType KeyType) error
}

type OpenRouter struct {
	provisioningKey string
	env             string
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	orgRepo         *orgRepo.Queries
	orClient        *guardian.HTTPClient
	refresher       KeyRefresher
	featureClient   *productfeatures.Client
	// baseURL is OpenRouterBaseURL outside of tests.
	baseURL string
}

var _ Provisioner = (*OpenRouter)(nil)

func New(logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, db *pgxpool.Pool, env string, provisioningKey string, refresher KeyRefresher, featureClient *productfeatures.Client, tracking billing.Tracker) *OpenRouter {
	orClient := guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig())

	return &OpenRouter{
		provisioningKey: provisioningKey,
		env:             env,
		logger:          logger.With(attr.SlogComponent("openrouter")),
		db:              db,
		repo:            repo.New(db),
		orgRepo:         orgRepo.New(db),
		orClient:        orClient,
		refresher:       refresher,
		featureClient:   featureClient,
		baseURL:         OpenRouterBaseURL,
	}
}

func (o *OpenRouter) ProvisionAPIKey(ctx context.Context, orgID string, keyType KeyType) (string, error) {
	var openrouterKey string

	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return "", fmt.Errorf("provision openrouter key: %w", err)
	}
	key, err := o.repo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	switch {
	// A real read failure must be checked before the missing-key case: a
	// failed lookup returns a zero-valued row, so key.Key == "" would
	// otherwise swallow the error and mint an upstream key.
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").LogError(ctx, o.logger)

	case errors.Is(err, pgx.ErrNoRows), key.Key == "":
		openrouterKey, err = o.createAndStoreAPIKey(ctx, orgID, keyType)
		if err != nil {
			return "", err
		}

	default:
		openrouterKey = key.Key
	}

	if err := inv.Check("openrouter provisioning", "key is set", openrouterKey != ""); err != nil {
		return "", fmt.Errorf("assertion error: %w", err)
	}

	return openrouterKey, nil
}

// createAndStoreAPIKey mints an upstream OpenRouter key and records it,
// serialized per (org, key type) with an advisory lock held across the
// upstream call: concurrent first completions would otherwise both miss the
// row, both create upstream keys, and the loser's insert would fail on the
// composite primary key, leaving an orphaned upstream key. Contention only
// happens on an org's first completion per key type, so holding the
// transaction across one HTTP round trip is acceptable — but the round trip
// is time-boxed below, because the lock and a pooled DB connection are held
// across it and every waiter pins its own pool connection.
func (o *OpenRouter) createAndStoreAPIKey(ctx context.Context, orgID string, keyType KeyType) (string, error) {
	dbtx, err := o.db.Begin(ctx)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error provisioning openrouter key").LogError(ctx, o.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := o.repo.WithTx(dbtx)
	if err := txRepo.LockOpenRouterKeyProvisioning(ctx, repo.LockOpenRouterKeyProvisioningParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "error locking openrouter key provisioning").LogError(ctx, o.logger)
	}

	// Re-read under the lock: a concurrent provisioner may have created the
	// key while we waited.
	key, err := txRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	switch {
	// Read-failure check must precede the missing-key case — see
	// ProvisionAPIKey.
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").LogError(ctx, o.logger)
	case errors.Is(err, pgx.ErrNoRows), key.Key == "":
	default:
		return key.Key, nil
	}

	// Read through the transaction: this goroutine already holds a pool
	// connection, and under provisioning contention every waiter holds one
	// too — acquiring a second connection here could deadlock the winner
	// against a pool exhausted by its own waiters.
	org, err := o.orgRepo.WithTx(dbtx).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to get organization").LogError(ctx, o.logger)
	}

	creditAmount := o.getLimitForOrg(org)

	// Cap the upstream call so guardian's retry backoff cannot stretch the
	// advisory-lock hold to minutes during an OpenRouter outage; a burst of
	// waiters would otherwise exhaust the DB pool.
	createCtx, cancel := context.WithTimeout(ctx, upstreamKeyCreateTimeout)
	defer cancel()
	keyResponse, err := o.createOpenRouterAPIKey(createCtx, orgID, org.Slug, keyType, creditAmount)
	if err != nil {
		return "", err
	}

	_, err = txRepo.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		Key:            *keyResponse.Key,
		KeyHash:        keyResponse.Data.Hash,
		MonthlyCredits: int64(creditAmount),
	})
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to store openrouter key data").LogError(ctx, o.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to store openrouter key data").LogError(ctx, o.logger)
	}

	if o.refresher != nil {
		if err := o.refresher.ScheduleOpenRouterKeyRefresh(ctx, orgID, keyType); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "error scheduling open router key refresh").LogError(ctx, o.logger)
		}
	}

	return *keyResponse.Key, nil
}

func (o *OpenRouter) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, fmt.Errorf("refresh openrouter key limit: %w", err)
	}
	key, err := o.repo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get OpenRouter API key: %w", err)
	}

	if key.MonthlyCredits == 0 && !key.Disabled {
		return 0, errors.New("cannot make an update to monthly credits of 0")
	}

	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").LogError(ctx, o.logger)
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
		KeyType:        string(keyType),
		MonthlyCredits: int64(keyLimit),
		KeyHash:        keyResponse.Data.Hash,
		Key:            key.Key,
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to update openrouter key").LogError(ctx, o.logger)
	}

	return keyLimit, nil
}

type keyUsageResponse struct {
	Data struct {
		Limit        *float64 `json:"limit"`
		UsageMonthly *float64 `json:"usage_monthly"`
	} `json:"data"`
}

func (o *OpenRouter) GetCreditsUsed(ctx context.Context, orgID string, keyType KeyType) (float64, int, error) {
	org, err := o.orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").LogError(ctx, o.logger)
	}
	limit := o.getLimitForOrg(org)

	key, err := o.repo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType.OrDefault()),
	})
	if err != nil {
		return 0, limit, nil // the key doesn't exist yet
	}

	used, _, err := o.GetKeyUsage(ctx, key.Key)
	if err != nil {
		return 0, limit, err
	}

	return used, limit, nil
}

// GetKeyUsage issues the upstream `/v1/key` call with the given API key and
// returns the rounded monthly usage along with the upstream-configured monthly
// limit already rounded to the int64 representation used by the DB. The
// returned limit is nil when OpenRouter reports an unlimited key. Callers that
// already have the key (e.g. the credits monitoring activity, which joins
// openrouter_api_keys in a single SQL query) can skip the org/key DB lookups
// in GetCreditsUsed.
func (o *OpenRouter) GetKeyUsage(ctx context.Context, apiKey string) (float64, *int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/v1/key", nil)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to build openrouter key usage request", attr.SlogError(err))
		return 0, nil, fmt.Errorf("build key usage request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to send openrouter key usage request", attr.SlogError(err))
		return 0, nil, fmt.Errorf("send key usage request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return 0, nil, errors.New("fetch OpenRouter key usage: " + resp.Status)
	}

	var usageResp keyUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usageResp); err != nil {
		o.logger.ErrorContext(ctx, "failed to decode key usage response", attr.SlogError(err))
		return 0, nil, fmt.Errorf("decode key usage response: %w", err)
	}

	var creditsUsed float64
	if usageResp.Data.UsageMonthly != nil {
		creditsUsed = math.Round(*usageResp.Data.UsageMonthly*100) / 100
	}

	var limit *int64
	if usageResp.Data.Limit != nil {
		l := int64(math.Round(*usageResp.Data.Limit))
		limit = &l
	}

	return creditsUsed, limit, nil
}

// ReconcileMonthlyCredits self-heals drift in the locally cached monthly limit
// after an out-of-band change on the OpenRouter dashboard. See the
// Provisioner interface doc for the full contract.
func (o *OpenRouter) ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType KeyType, currentLimit int64, upstreamLimit *int64) (int64, error) {
	if upstreamLimit == nil {
		return currentLimit, nil
	}

	newLimit := *upstreamLimit
	if newLimit == currentLimit {
		return currentLimit, nil
	}

	if err := o.repo.UpdateOpenRouterKeyMonthlyCredits(ctx, repo.UpdateOpenRouterKeyMonthlyCreditsParams{
		OrganizationID: orgID,
		KeyType:        string(keyType.OrDefault()),
		MonthlyCredits: newLimit,
	}); err != nil {
		return currentLimit, fmt.Errorf("reconcile openrouter monthly credits: %w", err)
	}

	o.logger.InfoContext(ctx, "reconciled openrouter monthly credits from upstream",
		attr.SlogOrganizationID(orgID),
		attr.SlogOpenRouterKeyType(string(keyType.OrDefault())),
		attr.SlogOpenRouterKeyPreviousLimit(int(currentLimit)),
		attr.SlogOpenRouterKeyLimit(int(newLimit)),
	)

	return newLimit, nil
}

func (o *OpenRouter) getLimitForOrg(org orgRepo.OrganizationMetadatum) int {
	if slices.Contains(specialLimitOrgs, org.ID) {
		return 500
	}

	return creditsAccountTypeMap[org.GramAccountType]
}

// upstreamKeyIdentity names an org's OpenRouter key. Chat key naming must
// stay byte-identical to the historical format — the upstream keys already
// exist under these names — so only internal keys get a suffix.
func upstreamKeyIdentity(env, orgID, orgSlug string, keyType KeyType) (name, label string) {
	name = fmt.Sprintf("gram-%s-%s", env, orgID)
	label = fmt.Sprintf("%s (%s environment)", orgSlug, env)
	if keyType == KeyTypeInternal {
		name += "-internal"
		label = fmt.Sprintf("%s (%s environment, internal)", orgSlug, env)
	}
	return name, label
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

func (o *OpenRouter) createOpenRouterAPIKey(ctx context.Context, orgID string, orgSlug string, keyType KeyType, keyLimit int) (*keyResponse, error) {
	creditLimit := float64(keyLimit)
	name, label := upstreamKeyIdentity(o.env, orgID, orgSlug, keyType)
	requestBody := createKeyRequest{
		Name:       name,
		Label:      label,
		Limit:      &creditLimit,
		LimitReset: "monthly",
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		o.logger.ErrorContext(ctx, "failed to marshal create openrouter key request body", attr.SlogError(err))
		return nil, fmt.Errorf("failed to serialize create key request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/v1/keys", bytes.NewReader(bodyBytes))
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

	req, err := http.NewRequestWithContext(ctx, "PATCH", o.baseURL+fmt.Sprintf("/v1/keys/%s", keyHash), bytes.NewReader(bodyBytes))
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

type ModelUsage struct {
	TotalCost             *float64
	CacheDiscount         float64
	UpstreamInferenceCost float64
	Model                 string
	TokensPrompt          int
	TokensCompletion      int
	NativeTokensCached    int
	NativeTokensReasoning int
}

type generationResponse struct {
	Data struct {
		TotalCost             float64 `json:"total_cost"`
		CacheDiscount         float64 `json:"cache_discount"`
		UpstreamInferenceCost float64 `json:"upstream_inference_cost"`
		Model                 string  `json:"model"`
		TokensPrompt          int     `json:"tokens_prompt"`
		TokensCompletion      int     `json:"tokens_completion"`
		NativeTokensCached    int     `json:"native_tokens_cached"`
		NativeTokensReasoning int     `json:"native_tokens_reasoning"`
	} `json:"data"`
}

func (o *OpenRouter) getGenerationDetails(ctx context.Context, generationID string, orgID string, keyType KeyType) (*generationResponse, int, error) {
	// A generation is only visible under the key that produced it — querying
	// with the wrong key type 404s (e.g. a streamed internal completion's
	// usage fallback).
	key, err := o.repo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType.OrDefault()),
	})
	if err != nil {
		return nil, 0, oops.E(oops.CodeUnexpected, err, "failed to get openrouter API key").LogError(ctx, o.logger)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/v1/generation", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create generation request: %w", err)
	}

	q := req.URL.Query()
	q.Set("id", generationID)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+key.Key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.orClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("send generation request: %w", err)
	}

	defer o11y.NoLogDefer(func() error {
		return resp.Body.Close()
	})

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("fetch generation from OpenRouter: %s", resp.Status)
	}

	var genResp generationResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode generation response: %w", err)
	}

	return &genResp, resp.StatusCode, nil
}

// GetModelUsage fetches generation details from OpenRouter when inline usage is
// unavailable, currently only for streams closed before the final usage chunk.
func (o *OpenRouter) GetModelUsage(ctx context.Context, generationID string, orgID string, keyType KeyType) (*ModelUsage, error) {
	var genResp *generationResponse
	var statusCode int
	var err error

	// This path is intentionally narrow: normal completions consume inline
	// usage, and only incomplete inline accounting reaches this fallback. Give
	// OpenRouter generation stats time to propagate without reviving the old
	// poll-on-every-completion behavior that produced error-log noise.
	delays := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 8 * time.Second}
	for attempt, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled while fetching generation details: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		genResp, statusCode, err = o.getGenerationDetails(ctx, generationID, orgID, keyType)
		if err == nil {
			break
		}
		if statusCode != http.StatusNotFound || attempt == len(delays)-1 {
			break
		}
	}

	if err != nil {
		return nil, err
	}

	cost := genResp.Data.TotalCost
	return &ModelUsage{
		TotalCost:             &cost,
		CacheDiscount:         genResp.Data.CacheDiscount,
		UpstreamInferenceCost: genResp.Data.UpstreamInferenceCost,
		Model:                 genResp.Data.Model,
		TokensPrompt:          genResp.Data.TokensPrompt,
		TokensCompletion:      genResp.Data.TokensCompletion,
		NativeTokensCached:    genResp.Data.NativeTokensCached,
		NativeTokensReasoning: genResp.Data.NativeTokensReasoning,
	}, nil
}

// ToModelUsage projects the inline OpenRouter usage payload into the
// billing-facing ModelUsage shape. Returns nil when the payload has no
// signal (no tokens and no cost) — e.g. an aborted stream that never
// reached the final usage chunk.
func (u Usage) ToModelUsage(model string) *ModelUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 && u.Cost == nil && u.CostDetails == nil && u.PromptTokensDetails == nil && u.CompletionTokensDetails == nil {
		return nil
	}

	out := &ModelUsage{
		TotalCost:             nil,
		CacheDiscount:         0,
		UpstreamInferenceCost: 0,
		Model:                 model,
		TokensPrompt:          u.PromptTokens,
		TokensCompletion:      u.CompletionTokens,
		NativeTokensCached:    0,
		NativeTokensReasoning: 0,
	}

	if u.Cost != nil {
		cost := *u.Cost
		out.TotalCost = &cost
	}
	if u.CostDetails != nil {
		out.UpstreamInferenceCost = u.CostDetails.UpstreamInferenceCost
		out.CacheDiscount = u.CostDetails.CacheDiscount
	}
	if u.PromptTokensDetails != nil {
		out.NativeTokensCached = u.PromptTokensDetails.CachedTokens
	}
	if u.CompletionTokensDetails != nil {
		out.NativeTokensReasoning = u.CompletionTokensDetails.ReasoningTokens
	}
	return out
}
