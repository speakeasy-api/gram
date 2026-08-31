package openrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsRepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const OpenRouterBaseURL = "https://openrouter.ai/api"

// KeyType selects which of an org's provisioned OpenRouter keys pays for a
// request. Each org can hold one key per type (openrouter_api_keys is keyed
// by (organization_id, key_type)): the "chat" type funds Other inference
// surfaces, while the "internal" type funds Security inference and other
// platform-initiated LLM
// usage (risk judges, title generation, chat resolutions, memory) so a burst
// of scanning inference can never exhaust the Other inference cap and 402
// customer-facing inference. Selection is an explicit request field — never
// derived from the usage source, which the completions proxy accepts from
// clients and could be spoofed onto the internal key.
type KeyType string

const (
	KeyTypeChat     KeyType = "chat"
	KeyTypeInternal KeyType = "internal"
)

// KeyDesiredState is the state a durable billing transition expects for an
// existing platform key. It travels with the event-specific workflow so an
// older opposite transition can be recognized after out-of-order delivery.
type KeyDesiredState string

const (
	KeyDesiredStateEnabled  KeyDesiredState = "enabled"
	KeyDesiredStateDisabled KeyDesiredState = "disabled"
)

func (s KeyDesiredState) Validate() error {
	switch s {
	case KeyDesiredStateEnabled, KeyDesiredStateDisabled:
		return nil
	default:
		return fmt.Errorf("invalid OpenRouter key desired state %q", s)
	}
}

// AllKeyTypes is the single definition of the valid key-type set. Validate
// and any caller that fans out across an org's keys (e.g. account-type
// limit refreshes) consume it, so adding a key type here propagates without
// hunting call sites.
var AllKeyTypes = []KeyType{KeyTypeChat, KeyTypeInternal}

var billableKeyTypes = []KeyType{KeyTypeChat, KeyTypeInternal}

// BillableKeyTypes returns the platform-managed OpenRouter keys whose spend a
// PAYG organization owns. Estimate and invoice paths must consume this set.
func BillableKeyTypes() []KeyType {
	return slices.Clone(billableKeyTypes)
}

// BillableKeyTypeStrings returns BillableKeyTypes in the shape SQL queries use.
func BillableKeyTypeStrings() []string {
	keyTypes := make([]string, len(billableKeyTypes))
	for i, keyType := range billableKeyTypes {
		keyTypes[i] = string(keyType)
	}
	return keyTypes
}

// BillableKeyPolicyFingerprint mechanically identifies the ordered canonical
// billable key set so collection and settlement can reject rolling-deployment
// handoffs that used different policies.
func BillableKeyPolicyFingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join(BillableKeyTypeStrings(), "\x00")))
	return fmt.Sprintf("%x", sum)
}

// IsBillable reports whether PAYG invoices include this key type.
func (k KeyType) IsBillable() bool {
	return slices.Contains(billableKeyTypes, k.OrDefault())
}

const (
	// upstreamKeyCreateTimeout bounds the POST /v1/keys call made while
	// holding the per-(org, key type) provisioning advisory lock.
	upstreamKeyCreateTimeout = 15 * time.Second
	// upstreamKeyPatchTimeout leaves the spend-cap activity enough time to
	// persist its mirror and audit before its 30-second attempt deadline.
	upstreamKeyPatchTimeout = 20 * time.Second
)

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
	"anthropic/claude-opus-5":       true,
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
	"google/gemini-3.5-flash-lite":  true,
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
	"z-ai/glm-5.3-flash":            true,
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
	"z-ai":       "z-ai/glm-5.3-flash",
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
	string(billing.TierBase):       5,
	string(billing.TierPro):        100,
	string(billing.TierPayg):       100,
	string(billing.TierEnterprise): 100,
	"":                             5, // safety default
}

// AccountTypeCreditLimit returns the explicit platform-key policy for a billing
// tier. Callers that require a particular tier must handle ok=false instead of
// falling back to the free-tier limit.
func AccountTypeCreditLimit(tier billing.Tier) (limit int, ok bool) {
	limit, ok = creditsAccountTypeMap[string(tier)]
	return limit, ok
}

// DefaultCreditLimit applies the complete mint-time policy after the caller
// has resolved whether the organization is still trial-tier.
func DefaultCreditLimit(orgID string, tier billing.Tier, activeTrial bool) (limit int, ok bool) {
	if IsSpecialLimitOrg(orgID) {
		return 500, true
	}
	if activeTrial {
		return trialCreditLimit, true
	}
	return AccountTypeCreditLimit(tier)
}

// ResolveDefaultCreditLimit reads whether the organization is still trial-tier
// and applies the full mint-time policy shared by provisioning and
// legacy-key repair. The clock is not the source of truth: an un-demoted,
// unconverted trial row is still trial-tier until the demotion sweeper
// stamps demoted_at. A read failure falls through to the account type rather
// than capping a paying customer on a transient database error.
func ResolveDefaultCreditLimit(
	ctx context.Context,
	logger *slog.Logger,
	dbtx trialsRepo.DBTX,
	orgID string,
	tier billing.Tier,
) (limit int, ok bool) {
	if IsSpecialLimitOrg(orgID) {
		return DefaultCreditLimit(orgID, tier, false)
	}

	trial, err := trialsRepo.New(dbtx).GetTrial(ctx, orgID)
	activeTrial := err == nil && !trial.ConvertedAt.Valid && !trial.DemotedAt.Valid
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.WarnContext(ctx, "error reading trial; using the account type credit limit",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
	}

	return DefaultCreditLimit(orgID, tier, activeTrial)
}

// trialCreditLimit caps each key an organization inside a trial holds, so its
// total trial exposure is this amount multiplied by len(AllKeyTypes). A trial
// is armed without verified intent and the credit-balance gate hard-stops the
// free tier only, so these key limits are its only spend ceiling.
//
// The limit binds at mint time and nothing re-mints a key. AGE-3138 and
// AGE-3141 cover the two lifecycle edges that leaves wrong.
const trialCreditLimit = 50

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
	// /v1/keys/:hash) and mirrors the new value into the local DB. It is also
	// the reinstatement path: a key that DisableAPIKey turned off comes back
	// enabled, upstream and locally.
	RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error)
	AddAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error)
	RemoveAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error)

	// DisableAPIKey turns the org's key off upstream and records that locally,
	// after which ProvisionAPIKey refuses it with ErrPlatformKeyDisabled. The
	// key survives, so RefreshAPIKeyLimit can reinstate it. An org with no key
	// of that type is a no-op.
	DisableAPIKey(ctx context.Context, orgID string, keyType KeyType) error

	GetCreditsUsed(ctx context.Context, orgID string, keyType KeyType) (float64, int, error)

	// GetKeyUsage issues GET /v1/key for the given API key and returns the
	// rounded monthly usage along with the upstream-configured monthly limit
	// already rounded to the int64 representation used by the DB. The limit is
	// nil when OpenRouter returns an unlimited key.
	GetKeyUsage(ctx context.Context, apiKey string) (used float64, limit *int64, err error)

	// ReconcileMonthlyCredits compares upstreamLimit against the caller-supplied
	// currentLimit and currentGeneration from the DB snapshot and writes the upstream value to the
	// openrouter_api_keys row when they diverge. It is a DB-only reconciliation
	// — it does NOT call OpenRouter — and is intended to self-heal drift
	// introduced by out-of-band edits on the OpenRouter dashboard. A nil
	// upstreamLimit means unlimited and is mirrored as zero. The conditional
	// write never overwrites a cap operation committed after that snapshot,
	// including one that deliberately writes the same numeric limit.
	// Returns the effective limit the caller should use for the current tick.
	ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error)

	// GetModelUsage fetches generation usage by ID. Normal completion paths use
	// inline usage; this is only a fallback for streams closed before the final
	// usage chunk arrives. A generation is only visible under the key that made
	// it, so the caller must name the same key type the completion used.
	GetModelUsage(ctx context.Context, generationID string, orgID string, keyType KeyType) (*ModelUsage, error)
}

// DBTX is the database executor accepted by generated OpenRouter and
// organization queries. It lets a caller that already owns a session-level
// billing lock perform the associated reads and write on that same session.
type DBTX = repo.DBTX

type KeyRefresher interface {
	ScheduleOpenRouterKeyRefresh(ctx context.Context, orgID string, keyType KeyType, limit *int) error
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
	enc             *encryption.Client
	// baseURL is OpenRouterBaseURL outside of tests.
	baseURL string
}

var _ Provisioner = (*OpenRouter)(nil)

// Option customizes an OpenRouter client without changing production defaults.
type Option func(*OpenRouter)

// WithTestBaseURL points OpenRouter requests at a loopback HTTP test server.
func WithTestBaseURL(baseURL string) (Option, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenRouter test base URL: %w", err)
	}

	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if parsed.Scheme != "http" || parsed.Host == "" || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
		return nil, fmt.Errorf("OpenRouter test base URL must use HTTP and a loopback host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("OpenRouter test base URL must not contain userinfo, query, fragment, or a non-root path")
	}

	parsed.Path = ""
	return func(openRouter *OpenRouter) {
		openRouter.baseURL = parsed.String()
	}, nil
}

func New(logger *slog.Logger, tracerProvider trace.TracerProvider, guardianPolicy *guardian.Policy, db *pgxpool.Pool, env string, provisioningKey string, refresher KeyRefresher, featureClient *productfeatures.Client, tracking billing.Tracker, enc *encryption.Client, options ...Option) *OpenRouter {
	orClient := guardianPolicy.PooledClient(guardian.WithDefaultRetryConfig())

	openRouter := &OpenRouter{
		provisioningKey: provisioningKey,
		env:             env,
		logger:          logger.With(attr.SlogComponent("openrouter")),
		db:              db,
		repo:            repo.New(db),
		orgRepo:         orgRepo.New(db),
		orClient:        orClient,
		refresher:       refresher,
		featureClient:   featureClient,
		enc:             enc,
		baseURL:         OpenRouterBaseURL,
	}
	for _, option := range options {
		option(openRouter)
	}

	return openRouter
}

// keyMaterial resolves the usable API key for a row by decrypting the
// encrypted column, the only place key material lives. A row without a
// ciphertext is a hard error, as is a decrypt failure, which means this
// process runs with the wrong encryption key.
func (o *OpenRouter) keyMaterial(key repo.OpenrouterApiKey) (string, error) {
	if !key.KeyEncrypted.Valid {
		return "", fmt.Errorf("openrouter key row for organization %s (%s) holds no encrypted key material", key.OrganizationID, key.KeyType)
	}

	plaintext, err := o.enc.Decrypt(key.KeyEncrypted.String)
	if err != nil {
		return "", fmt.Errorf("decrypt openrouter key for organization %s (%s): %w", key.OrganizationID, key.KeyType, err)
	}

	return plaintext, nil
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
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").LogError(ctx, o.logger)

	// Only a missing row triggers provisioning. An existing row without a
	// ciphertext hard-errors in keyMaterial below instead: minting a
	// replacement would orphan the upstream key the row already names.
	case errors.Is(err, pgx.ErrNoRows):
		openrouterKey, err = o.createAndStoreAPIKey(ctx, orgID, keyType)
		if err != nil {
			return "", err
		}

	default:
		// Every platform-key completion resolves through here, so this is where
		// the lockdown binds. Refusing locally beats an upstream rejection
		// whose status also means "our provisioning key is broken".
		if EffectiveDisabled(key.Disabled, key.DisableCauses) {
			return "", fmt.Errorf("resolve %s key: %w", keyType, ErrPlatformKeyDisabled)
		}
		openrouterKey, err = o.keyMaterial(key)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").LogError(ctx, o.logger)
		}
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
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return "", oops.E(oops.CodeUnexpected, err, "error reading open router key data").LogError(ctx, o.logger)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		// The lockdown binds here too: the key may have been disabled between
		// the caller's read and this re-read under the lock. A row without a
		// ciphertext hard-errors in keyMaterial rather than falling through to
		// mint a replacement upstream key.
		if EffectiveDisabled(key.Disabled, key.DisableCauses) {
			return "", fmt.Errorf("resolve %s key: %w", keyType, ErrPlatformKeyDisabled)
		}
		plaintext, keyErr := o.keyMaterial(key)
		if keyErr != nil {
			return "", oops.E(oops.CodeUnexpected, keyErr, "error reading open router key data").LogError(ctx, o.logger)
		}
		return plaintext, nil
	}

	// Read through the transaction: this goroutine already holds a pool
	// connection, and under provisioning contention every waiter holds one
	// too — acquiring a second connection here could deadlock the winner
	// against a pool exhausted by its own waiters.
	org, err := o.orgRepo.WithTx(dbtx).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to get organization").LogError(ctx, o.logger)
	}

	creditAmount := o.defaultLimitForOrg(ctx, dbtx, org)

	// Cap the upstream call so guardian's retry backoff cannot stretch the
	// advisory-lock hold to minutes during an OpenRouter outage; a burst of
	// waiters would otherwise exhaust the DB pool.
	createCtx, cancel := context.WithTimeout(ctx, upstreamKeyCreateTimeout)
	defer cancel()
	keyResponse, err := o.createOpenRouterAPIKey(createCtx, orgID, org.Slug, keyType, creditAmount)
	if err != nil {
		return "", err
	}

	keyCiphertext, err := o.enc.Encrypt([]byte(*keyResponse.Key))
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to encrypt openrouter key").LogError(ctx, o.logger)
	}

	_, err = txRepo.CreateOpenRouterAPIKey(ctx, repo.CreateOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		KeyEncrypted:   conv.ToPGText(keyCiphertext),
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
		if err := o.refresher.ScheduleOpenRouterKeyRefresh(ctx, orgID, keyType, nil); err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "error scheduling open router key refresh").LogError(ctx, o.logger)
		}
	}

	return *keyResponse.Key, nil
}

func (o *OpenRouter) RefreshAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.refreshAPIKeyLimitInTx(ctx, orgID, keyType, limit, false)
}

// RefreshAPIKeyLimitWithDB is RefreshAPIKeyLimit using db for every local read
// and write. Callers holding a session advisory lock must use the same
// connection rather than acquiring a second pool connection while locked.
func (o *OpenRouter) RefreshAPIKeyLimitWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.refreshAPIKeyLimitWithLock(ctx, db, orgID, keyType, limit, false)
}

// ReinstateAPIKeyLimit refreshes a key while explicitly allowing a disabled
// key to come back when its caller needs the policy default resolved from nil.
func (o *OpenRouter) ReinstateAPIKeyLimit(ctx context.Context, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.refreshAPIKeyLimitInTx(ctx, orgID, keyType, limit, true)
}

// ReinstateAPIKeyLimitWithDB is ReinstateAPIKeyLimit using db for every local
// read and write. Callers holding a session advisory lock must pass that same
// connection instead of making the pool acquire a second connection.
func (o *OpenRouter) ReinstateAPIKeyLimitWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int) (int, error) {
	return o.refreshAPIKeyLimitWithLock(ctx, db, orgID, keyType, limit, true)
}

func (o *OpenRouter) refreshAPIKeyLimitInTx(ctx context.Context, orgID string, keyType KeyType, limit *int, reinstate bool) (int, error) {
	tx, err := o.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin OpenRouter key refresh: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	refreshed, err := o.refreshAPIKeyLimitWithLock(ctx, tx, orgID, keyType, limit, reinstate)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit OpenRouter key refresh: %w", err)
	}
	return refreshed, nil
}

func (o *OpenRouter) refreshAPIKeyLimitWithLock(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int, reinstate bool) (int, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, fmt.Errorf("refresh openrouter key limit: %w", err)
	}
	if err := repo.New(db).AcquireOpenRouterBillingLock(ctx, repo.AcquireOpenRouterBillingLockParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}); err != nil {
		return 0, fmt.Errorf("lock OpenRouter key refresh: %w", err)
	}
	return o.refreshAPIKeyLimit(ctx, db, orgID, keyType, limit, reinstate)
}

func (o *OpenRouter) refreshAPIKeyLimit(ctx context.Context, db DBTX, orgID string, keyType KeyType, limit *int, reinstate bool) (int, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, fmt.Errorf("refresh openrouter key limit: %w", err)
	}
	if limit != nil && *limit <= 0 {
		return 0, errors.New("refresh openrouter key limit: monthly credits must be positive")
	}
	keyRepo := repo.New(db)
	key, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get OpenRouter API key: %w", err)
	}

	if limit == nil && EffectiveDisabled(key.Disabled, key.DisableCauses) && !reinstate {
		// Generic refreshes must never undo a billing lockdown. An explicit
		// activation, re-subscription, or platform-admin enable is the only path
		// allowed to reinstate either platform key.
		return int(key.MonthlyCredits), nil
	}

	org, err := orgRepo.New(db).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to get organization").LogError(ctx, o.logger)
	}
	if limit == nil && org.GramAccountType == string(billing.TierPayg) && !reinstate {
		// OpenRouter is the authority for a PAYG customer's chosen inference cap
		// on each materialized platform key. Generic tier refreshes preserve both
		// the mirrored value and the key's
		// disabled state without touching upstream or rewriting the row. Only an
		// explicit activation/re-subscription or setSpendCap operation passes a
		// non-nil limit.
		return int(key.MonthlyCredits), nil
	}

	var keyLimit int
	if limit != nil {
		keyLimit = *limit
	} else {
		keyLimit = o.defaultLimitForOrg(ctx, db, org)
	}

	creditLimit := float64(keyLimit)
	patch := updateKeyRequest{Limit: &creditLimit, LimitReset: "monthly", Disabled: nil}
	effectiveDisabled := EffectiveDisabled(key.Disabled, key.DisableCauses)
	legacyReinstate := effectiveDisabled && key.DisableCauses == nil
	if legacyReinstate {
		// Before classification, setting a limit on a disabled key also restored
		// the legacy broad switch. Classified causes are authoritative and may
		// only be removed by their owning cause-aware path.
		patch.Disabled = new(false)
	}

	patchCtx, cancel := context.WithTimeout(ctx, upstreamKeyPatchTimeout)
	defer cancel()
	keyResponse, err := o.patchOpenRouterAPIKey(patchCtx, key.KeyHash, patch)
	if err != nil {
		return 0, err
	}
	if keyResponse.Data.Hash != key.KeyHash {
		err := errors.New("refresh openrouter key limit: upstream key identity changed")
		// This means the upstream key addressed by our immutable hash no longer
		// resolves to the same identity. Every caller must fail closed, and the
		// detection site must remain observable even for background refreshes
		// that do not have an RPC boundary to log the failure.
		o.logger.ErrorContext(ctx, "OpenRouter key identity changed while setting inference cap",
			attr.SlogOrganizationID(orgID),
			attr.SlogOpenRouterKeyType(string(keyType)),
			attr.SlogError(err),
		)
		return 0, err
	}

	_, err = keyRepo.UpdateOpenRouterKey(ctx, repo.UpdateOpenRouterKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
		MonthlyCredits: int64(keyLimit),
		KeyHash:        keyResponse.Data.Hash,
		// This matches the upstream PATCH above. SQL also checks that the row is
		// still unclassified, so a cause classified during the network call is
		// never cleared locally.
		Reinstate: legacyReinstate,
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "failed to update openrouter key").LogError(ctx, o.logger)
	}

	return keyLimit, nil
}

// DisableAPIKey stops an organization from spending on its platform key. Gram
// enforces the lockdown itself: ProvisionAPIKey refuses a disabled key and
// returns ErrPlatformKeyDisabled. The upstream flag covers any spend that never
// passes through key resolution, such as a key that leaked.
//
// The upstream PATCH runs before the local write. The reverse order would
// record a lockdown that a permanently failing PATCH never made.
func (o *OpenRouter) DisableAPIKey(ctx context.Context, orgID string, keyType KeyType) error {
	return o.disableAPIKey(ctx, o.db, orgID, keyType)
}

// AddAPIKeyDisableCause is staged for the cause-specific cutover. Wave B does
// not call it from product paths, but keeping the primitive here lets identity
// validation ship before any cause can be persisted after an upstream PATCH.
func (o *OpenRouter) AddAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return DisableCauseChange{}, fmt.Errorf("add OpenRouter API key disable cause: %w", err)
	}
	if err := cause.Validate(); err != nil {
		return DisableCauseChange{}, fmt.Errorf("add OpenRouter API key disable cause: %w", err)
	}

	var result DisableCauseChange
	err := o.withOpenRouterKeyBillingLock(ctx, orgID, keyType, func(conn *pgxpool.Conn) error {
		keyRepo := repo.New(conn)
		key, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			result = DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}
			return nil
		case err != nil:
			return fmt.Errorf("read OpenRouter key before adding disable cause: %w", err)
		case key.DisableCauses == nil:
			return errors.New("cannot add OpenRouter disable cause to unclassified key")
		case slices.Contains(key.DisableCauses, string(cause)):
			result = DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}
			return nil
		}

		patchCtx, cancel := context.WithTimeout(ctx, upstreamKeyPatchTimeout)
		response, patchErr := o.patchOpenRouterAPIKey(patchCtx, key.KeyHash, updateKeyRequest{Limit: nil, LimitReset: "", Disabled: new(true)})
		cancel()
		if patchErr != nil {
			return fmt.Errorf("disable upstream OpenRouter API key: %w", patchErr)
		}
		if response.Data.Hash != key.KeyHash {
			return errors.New("OpenRouter key identity mismatch after disable")
		}

		latest, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
		if err != nil || latest.KeyHash != key.KeyHash || latest.DisableCauses == nil {
			return errors.New("OpenRouter key changed concurrently while adding disable cause")
		}
		if slices.Contains(latest.DisableCauses, string(cause)) {
			result = DisableCauseChange{CauseChanged: false, KeyAccessChanged: false}
			return nil
		}
		accessChanged := len(latest.DisableCauses) == 0
		_, err = keyRepo.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
			OrganizationID: orgID, KeyType: string(keyType), KeyHash: key.KeyHash, DisableCause: string(cause),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("OpenRouter key changed concurrently while adding disable cause")
		}
		if err != nil {
			return fmt.Errorf("persist OpenRouter API key disable cause: %w", err)
		}
		result = DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}
		return nil
	})
	if err != nil {
		return DisableCauseChange{}, err
	}
	return result, nil
}

// AddAPIKeyDisableCauseWithDB uses db for every local read and write. The
// caller must hold the established per-key billing advisory lock.
func (o *OpenRouter) AddAPIKeyDisableCauseWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, cause DisableCause) (DisableCauseChange, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return unchangedDisableCauseChange(), fmt.Errorf("add OpenRouter API key disable cause: %w", err)
	}
	if err := cause.Validate(); err != nil {
		return unchangedDisableCauseChange(), fmt.Errorf("add OpenRouter API key disable cause: %w", err)
	}

	keyRepo := repo.New(db)
	key, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return unchangedDisableCauseChange(), nil
	case err != nil:
		return unchangedDisableCauseChange(), fmt.Errorf("read OpenRouter key before adding disable cause: %w", err)
	case key.DisableCauses == nil:
		return unchangedDisableCauseChange(), errors.New("cannot add OpenRouter disable cause to unclassified key")
	case slices.Contains(key.DisableCauses, string(cause)):
		return unchangedDisableCauseChange(), nil
	}

	patchCtx, cancel := context.WithTimeout(ctx, upstreamKeyPatchTimeout)
	response, patchErr := o.patchOpenRouterAPIKey(patchCtx, key.KeyHash, updateKeyRequest{Limit: nil, LimitReset: "", Disabled: new(true)})
	cancel()
	if patchErr != nil {
		return unchangedDisableCauseChange(), fmt.Errorf("disable upstream OpenRouter API key: %w", patchErr)
	}
	if response.Data.Hash != key.KeyHash {
		return unchangedDisableCauseChange(), errors.New("OpenRouter key identity mismatch after disable")
	}

	latest, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	if err != nil || latest.KeyHash != key.KeyHash || latest.DisableCauses == nil {
		return unchangedDisableCauseChange(), errors.New("OpenRouter key changed concurrently while adding disable cause")
	}
	if slices.Contains(latest.DisableCauses, string(cause)) {
		return unchangedDisableCauseChange(), nil
	}
	accessChanged := len(latest.DisableCauses) == 0
	if _, err = keyRepo.AddOpenRouterAPIKeyDisableCause(ctx, repo.AddOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(keyType), KeyHash: key.KeyHash, DisableCause: string(cause),
	}); errors.Is(err, pgx.ErrNoRows) {
		return unchangedDisableCauseChange(), errors.New("OpenRouter key changed concurrently while adding disable cause")
	} else if err != nil {
		return unchangedDisableCauseChange(), fmt.Errorf("persist OpenRouter API key disable cause: %w", err)
	}

	return DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}, nil
}

// RemoveAPIKeyDisableCause removes only cause. Standalone calls serialize with
// all compliant key mutations through the established per-key billing lock.
func (o *OpenRouter) RemoveAPIKeyDisableCause(ctx context.Context, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, unchangedDisableCauseChange(), fmt.Errorf("remove OpenRouter API key disable cause: %w", err)
	}
	if err := cause.Validate(); err != nil {
		return 0, unchangedDisableCauseChange(), fmt.Errorf("remove OpenRouter API key disable cause: %w", err)
	}
	if limit != nil && *limit <= 0 {
		return 0, unchangedDisableCauseChange(), errors.New("remove OpenRouter API key disable cause: monthly credits must be positive")
	}

	var keyLimit int
	var change DisableCauseChange
	err := o.withOpenRouterKeyBillingLock(ctx, orgID, keyType, func(conn *pgxpool.Conn) error {
		var removeErr error
		keyLimit, change, removeErr = o.removeAPIKeyDisableCauseWithDB(ctx, conn, orgID, keyType, cause, limit)
		return removeErr
	})
	if err != nil {
		return 0, unchangedDisableCauseChange(), err
	}
	return keyLimit, change, nil
}

// RemoveAPIKeyDisableCauseWithDB uses db for every local read and write. The
// caller must hold the established per-key billing advisory lock.
func (o *OpenRouter) RemoveAPIKeyDisableCauseWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error) {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return 0, unchangedDisableCauseChange(), fmt.Errorf("remove OpenRouter API key disable cause: %w", err)
	}
	if err := cause.Validate(); err != nil {
		return 0, unchangedDisableCauseChange(), fmt.Errorf("remove OpenRouter API key disable cause: %w", err)
	}
	if limit != nil && *limit <= 0 {
		return 0, unchangedDisableCauseChange(), errors.New("remove OpenRouter API key disable cause: monthly credits must be positive")
	}
	return o.removeAPIKeyDisableCauseWithDB(ctx, db, orgID, keyType, cause, limit)
}

func (o *OpenRouter) removeAPIKeyDisableCauseWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, cause DisableCause, limit *int) (int, DisableCauseChange, error) {
	keyRepo := repo.New(db)
	key, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{OrganizationID: orgID, KeyType: string(keyType)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return 0, unchangedDisableCauseChange(), nil
	case err != nil:
		return 0, unchangedDisableCauseChange(), fmt.Errorf("read OpenRouter key before removing disable cause: %w", err)
	case key.DisableCauses == nil:
		return 0, unchangedDisableCauseChange(), errors.New("cannot remove OpenRouter disable cause from unclassified key")
	case !slices.Contains(key.DisableCauses, string(cause)):
		return int(key.MonthlyCredits), unchangedDisableCauseChange(), nil
	}

	accessChanged := true
	for _, existingCause := range key.DisableCauses {
		if existingCause != string(cause) {
			accessChanged = false
			break
		}
	}
	keyLimit := int(key.MonthlyCredits)
	limitChanged := false
	if accessChanged {
		if limit != nil {
			keyLimit = *limit
		} else if key.MonthlyCredits == 0 {
			org, orgErr := orgRepo.New(db).GetOrganizationMetadata(ctx, orgID)
			if orgErr != nil {
				return 0, unchangedDisableCauseChange(), oops.E(oops.CodeUnexpected, orgErr, "failed to get organization").LogError(ctx, o.logger)
			}
			keyLimit = o.defaultLimitForOrg(ctx, db, org)
		}
		limitChanged = int64(keyLimit) != key.MonthlyCredits

		patch := updateKeyRequest{Limit: nil, LimitReset: "", Disabled: new(false)}
		if limitChanged {
			creditLimit := float64(keyLimit)
			patch.Limit = &creditLimit
			patch.LimitReset = "monthly"
		}
		patchCtx, cancel := context.WithTimeout(ctx, upstreamKeyPatchTimeout)
		response, patchErr := o.patchOpenRouterAPIKey(patchCtx, key.KeyHash, patch)
		cancel()
		if patchErr != nil {
			return 0, unchangedDisableCauseChange(), fmt.Errorf("enable upstream OpenRouter API key: %w", patchErr)
		}
		if response.Data.Hash != key.KeyHash {
			return 0, unchangedDisableCauseChange(), errors.New("remove OpenRouter API key disable cause: upstream key identity changed")
		}
	}

	_, err = keyRepo.RemoveOpenRouterAPIKeyDisableCause(ctx, repo.RemoveOpenRouterAPIKeyDisableCauseParams{
		OrganizationID: orgID, KeyType: string(keyType), KeyHash: key.KeyHash, DisableCause: string(cause),
		MonthlyCredits: int64(keyLimit), UpdateMonthlyCredits: limitChanged,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, unchangedDisableCauseChange(), errors.New("OpenRouter key changed concurrently while removing disable cause")
	}
	if err != nil {
		return 0, unchangedDisableCauseChange(), fmt.Errorf("persist OpenRouter API key disable cause removal: %w", err)
	}

	return keyLimit, DisableCauseChange{CauseChanged: true, KeyAccessChanged: accessChanged}, nil
}

func (o *OpenRouter) withOpenRouterKeyBillingLock(ctx context.Context, orgID string, keyType KeyType, operation func(*pgxpool.Conn) error) error {
	conn, err := o.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for OpenRouter key billing lock: %w", err)
	}
	queries := repo.New(conn)
	params := repo.AcquireOpenRouterKeyBillingLockParams{OrganizationID: orgID, KeyType: string(keyType)}
	if err := queries.AcquireOpenRouterKeyBillingLock(ctx, params); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := conn.Hijack().Close(cleanupCtx)
		cancel()
		if closeErr != nil {
			o.logger.ErrorContext(ctx, "close connection after OpenRouter key billing lock failure", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire OpenRouter key billing lock: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		unlocked, releaseErr := queries.ReleaseOpenRouterKeyBillingLock(cleanupCtx, repo.ReleaseOpenRouterKeyBillingLockParams(params))
		if releaseErr == nil && unlocked {
			conn.Release()
			return
		}
		if releaseErr == nil {
			releaseErr = errors.New("lock was not held by this session")
		}
		o.logger.ErrorContext(ctx, "release OpenRouter key billing lock", attr.SlogError(releaseErr))
		if closeErr := conn.Hijack().Close(cleanupCtx); closeErr != nil {
			o.logger.ErrorContext(ctx, "close connection with unreleased OpenRouter key billing lock", attr.SlogError(closeErr))
		}
	}()
	return operation(conn)
}

// DisableAPIKeyWithDB is DisableAPIKey using db for every local read and
// write. It is used while a caller owns the key's session advisory lock.
func (o *OpenRouter) DisableAPIKeyWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType) error {
	return o.disableAPIKey(ctx, db, orgID, keyType)
}

func (o *OpenRouter) disableAPIKey(ctx context.Context, db DBTX, orgID string, keyType KeyType) error {
	keyType = keyType.OrDefault()
	if err := keyType.Validate(); err != nil {
		return fmt.Errorf("disable openrouter key: %w", err)
	}

	keyRepo := repo.New(db)
	key, err := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// An organization that never ran a completion has no key of this type.
		return nil
	case err != nil:
		return fmt.Errorf("get openrouter key to disable: %w", err)
	}

	if _, err := o.patchOpenRouterAPIKey(ctx, key.KeyHash, updateKeyRequest{
		Limit:      nil,
		LimitReset: "",
		Disabled:   new(true),
	}); err != nil {
		return fmt.Errorf("disable upstream openrouter key: %w", err)
	}

	if err := keyRepo.DisableOpenRouterAPIKey(ctx, repo.DisableOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType),
	}); err != nil {
		return fmt.Errorf("mark openrouter key disabled: %w", err)
	}

	return nil
}

type keyUsageResponse struct {
	Data struct {
		Limit        *float64 `json:"limit"`
		UsageMonthly *float64 `json:"usage_monthly"`
	} `json:"data"`
}

func (o *OpenRouter) GetCreditsUsed(ctx context.Context, orgID string, keyType KeyType) (float64, int, error) {
	// The key carries the ceiling the customer actually spends against, which a
	// raise or a tier change can move away from the policy amount. Read it
	// first: resolving the policy amount costs two more queries, and only a key
	// minted before the column existed needs them.
	key, keyErr := o.repo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
		OrganizationID: orgID,
		KeyType:        string(keyType.OrDefault()),
	})

	limit := 0
	if keyErr == nil {
		limit = int(key.MonthlyCredits)
	}

	if errors.Is(keyErr, pgx.ErrNoRows) {
		return 0, 0, nil // the key doesn't exist yet
	}
	if keyErr != nil {
		return 0, 0, fmt.Errorf("read openrouter key for usage: %w", keyErr)
	}

	apiKey, err := o.keyMaterial(key)
	if err != nil {
		return 0, limit, fmt.Errorf("resolve openrouter key material: %w", err)
	}

	used, _, err := o.GetKeyUsage(ctx, apiKey)
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
func (o *OpenRouter) ReconcileMonthlyCredits(ctx context.Context, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return o.reconcileMonthlyCredits(ctx, o.db, orgID, keyType, currentLimit, currentGeneration, upstreamLimit)
}

// ReconcileMonthlyCreditsWithDB is ReconcileMonthlyCredits using db for the
// mirror write. Credits polling holds the same per-key advisory lock across
// its upstream read and this write, so a stale observation cannot race a cap
// operation and erase its applied generation.
func (o *OpenRouter) ReconcileMonthlyCreditsWithDB(ctx context.Context, db DBTX, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	return o.reconcileMonthlyCredits(ctx, db, orgID, keyType, currentLimit, currentGeneration, upstreamLimit)
}

func (o *OpenRouter) reconcileMonthlyCredits(ctx context.Context, db DBTX, orgID string, keyType KeyType, currentLimit int64, currentGeneration int64, upstreamLimit *int64) (int64, error) {
	// A nil upstream limit means the provider key is uncapped, not that the
	// last local mirror is still authoritative. Mirror that as zero so cap
	// meters and threshold alerts cannot claim an unenforced limit.
	newLimit := int64(0)
	if upstreamLimit != nil {
		newLimit = *upstreamLimit
	}
	if newLimit == currentLimit {
		return currentLimit, nil
	}

	keyRepo := repo.New(db)
	updated, err := keyRepo.CompareAndSetOpenRouterKeyMonthlyCredits(ctx, repo.CompareAndSetOpenRouterKeyMonthlyCreditsParams{
		OrganizationID:        orgID,
		KeyType:               string(keyType.OrDefault()),
		MonthlyCredits:        newLimit,
		CurrentMonthlyCredits: currentLimit,
		CurrentGeneration:     currentGeneration,
	})
	if err != nil {
		return currentLimit, fmt.Errorf("reconcile openrouter monthly credits: %w", err)
	}
	if updated == 0 {
		// A concurrent cap change won the compare-and-set. Read its value instead
		// of overwriting it with the stale provider observation.
		key, readErr := keyRepo.GetOpenRouterAPIKey(ctx, repo.GetOpenRouterAPIKeyParams{
			OrganizationID: orgID,
			KeyType:        string(keyType.OrDefault()),
		})
		if readErr != nil {
			return currentLimit, fmt.Errorf("read concurrently updated openrouter monthly credits: %w", readErr)
		}
		return key.MonthlyCredits, nil
	}

	o.logger.InfoContext(ctx, "reconciled openrouter monthly credits from upstream",
		attr.SlogOrganizationID(orgID),
		attr.SlogOpenRouterKeyType(string(keyType.OrDefault())),
		attr.SlogOpenRouterKeyPreviousLimit(int(currentLimit)),
		attr.SlogOpenRouterKeyLimit(int(newLimit)),
	)

	return newLimit, nil
}

// defaultLimitForOrg resolves the monthly credit ceiling an organization is
// entitled to by policy. It answers what a key would be minted at today, not
// what an existing key carries: an operator raise leaves the key above this
// amount, and only the key row records that. The account type is the last
// resort, after the special-org list and the trial row.
//
// dbtx arrives from the caller because the provisioning path runs inside a
// transaction that already holds an advisory lock and a pool connection, so it
// must read through that same transaction.
func (o *OpenRouter) defaultLimitForOrg(ctx context.Context, dbtx trialsRepo.DBTX, org orgRepo.OrganizationMetadatum) int {
	// A trial runs on the real enterprise tier, so the account type on its own
	// cannot tell a trial apart from a paying enterprise customer. A read
	// failure falls through to the account type rather than capping a paying
	// customer on a transient database error.
	limit, _ := ResolveDefaultCreditLimit(ctx, o.logger, dbtx, org.ID, billing.Tier(org.GramAccountType))
	return limit
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
	// Disabled toggles the upstream key off and on. It is a pointer because
	// omitting the field leaves the current state alone, which is what every
	// limit-only patch wants.
	Disabled *bool `json:"disabled,omitempty"`
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

func (o *OpenRouter) patchOpenRouterAPIKey(ctx context.Context, keyHash string, requestBody updateKeyRequest) (*keyResponse, error) {
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

	apiKey, err := o.keyMaterial(key)
	if err != nil {
		return nil, 0, oops.E(oops.CodeUnexpected, err, "failed to resolve openrouter API key material").LogError(ctx, o.logger)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", o.baseURL+"/v1/generation", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("create generation request: %w", err)
	}

	q := req.URL.Query()
	q.Set("id", generationID)
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+apiKey)
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
