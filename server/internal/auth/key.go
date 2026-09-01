package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type APIKeyScope int

const (
	APIKeyScopeInvalid  APIKeyScope = iota
	APIKeyScopeConsumer APIKeyScope = iota
	APIKeyScopeProducer APIKeyScope = iota
	APIKeyScopeChat     APIKeyScope = iota
	APIKeyScopeHooks    APIKeyScope = iota
	APIKeyScopeAgent    APIKeyScope = iota
	// APIKeyScopeAgentUser is the per-user device-agent *data* credential minted
	// by tokenExchange.exchange, and presented on data endpoints (agent.getPlugins).
	// It is deliberately narrower than APIKeyScopeAgent: the `agent` scope (the
	// org install credential) may call exchange to mint per-user keys, but
	// `agent_user` may not — so a leaked per-user key cannot mint another user's
	// key. `agent` implies `agent_user` (see Authorize), so an existing org key
	// still satisfies the data endpoints during the transition, with no
	// re-provisioning.
	APIKeyScopeAgentUser APIKeyScope = iota
	// APIKeyScopeHooksActingUser marks proof-bound relay credentials minted by
	// CLI auth. It is intentionally absent from APIKeyScopes so public key
	// creation cannot mint the trusted approved-client discriminator.
	APIKeyScopeHooksActingUser APIKeyScope = iota
)

// PluginAPIKeyNamePrefix is reserved for keys minted by plugin distribution
// flows. Historical user-created keys may still carry this prefix, so callers
// classifying org-wide hook keys must verify the token/name minting marker.
const (
	PluginAPIKeyNamePrefix  = "plugins-"
	LiteLLMAPIKeyNamePrefix = "litellm-"
)

// IsLiteLLMAPIKeyName recognizes the reserved name shape used by instance keys.
// Checking the full shape preserves last-access tracking for historical user
// keys that happened to use the prefix before it was reserved.
func IsLiteLLMAPIKeyName(name string) bool {
	if _, ok := LiteLLMInstanceIDFromAPIKeyName(name); ok {
		return true
	}
	suffix, ok := strings.CutPrefix(name, LiteLLMAPIKeyNamePrefix)
	if !ok {
		return false
	}
	parts := strings.Split(suffix, "-")
	if len(parts) != 2 || len(parts[0]) != 13 || len(parts[1]) != 8 {
		return false
	}
	for _, char := range parts[0] {
		if char < '0' || char > '9' {
			return false
		}
	}
	for _, char := range parts[1] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// LiteLLMInstanceIDFromAPIKeyName returns the stable instance ID encoded in
// managed keys minted after diagnostics attribution was introduced.
func LiteLLMInstanceIDFromAPIKeyName(name string) (uuid.UUID, bool) {
	suffix, ok := strings.CutPrefix(name, LiteLLMAPIKeyNamePrefix)
	if !ok {
		return uuid.Nil, false
	}
	parts := strings.Split(suffix, "-")
	if len(parts) != 3 || len(parts[0]) != 32 || len(parts[1]) != 13 || len(parts[2]) != 8 {
		return uuid.Nil, false
	}
	for _, char := range parts[1] {
		if char < '0' || char > '9' {
			return uuid.Nil, false
		}
	}
	for _, char := range parts[2] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return uuid.Nil, false
		}
	}
	instanceID, err := uuid.Parse(parts[0])
	return instanceID, err == nil
}

func GenerateAPIKeyMaterial(keyPrefix string) (plaintext, keyHash, displayPrefix string, err error) {
	var randomBytes [32]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", "", "", fmt.Errorf("generate random token bytes: %w", err)
	}
	token := hex.EncodeToString(randomBytes[:])
	plaintext = keyPrefix + token
	keyHash, err = GetAPIKeyHash(plaintext)
	if err != nil {
		return "", "", "", fmt.Errorf("hash api key: %w", err)
	}
	return plaintext, keyHash, keyPrefix + token[:5], nil
}

// IsOrgWidePluginHooksAPIKey recognizes keys minted by plugin publish and
// observability-download flows. The generated name embeds the first six token
// characters while keyPrefix stores the first five, so authentication can
// verify minting provenance instead of trusting a formerly-unrestricted name.
func IsOrgWidePluginHooksAPIKey(name, key, keyPrefix string) bool {
	var suffix string
	switch {
	case strings.HasPrefix(name, PluginAPIKeyNamePrefix+"hooks-download-"):
		suffix = strings.TrimPrefix(name, PluginAPIKeyNamePrefix+"hooks-download-")
	case strings.HasPrefix(name, PluginAPIKeyNamePrefix+"hooks-"):
		suffix = strings.TrimPrefix(name, PluginAPIKeyNamePrefix+"hooks-")
	default:
		return false
	}

	parts := strings.Split(suffix, "-")
	if len(parts) != 3 || len(parts[0]) != 8 || len(parts[1]) != 6 || len(parts[2]) != 6 {
		return false
	}
	const timestampLayout = "20060102-150405"
	timestamp := parts[0] + "-" + parts[1]
	parsedTimestamp, err := time.Parse(timestampLayout, timestamp)
	if err != nil || parsedTimestamp.Format(timestampLayout) != timestamp {
		return false
	}
	for _, char := range parts[2] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	tokenMarker := parts[2]
	return len(key) > len(keyPrefix) &&
		strings.HasPrefix(key, keyPrefix) &&
		strings.HasSuffix(keyPrefix, tokenMarker[:5]) &&
		key[len(keyPrefix)] == tokenMarker[5]
}

// DeviceAgentKeyName builds a unique, human-readable name for a minted
// device-agent API key: "device-agent:<userID>:<YYYYMMDD-HHMMSS>:<random>".
//
// The timestamp records issuance — device-agent keys are minted in
// low-frequency per-org bursts, so it is useful metadata. The random suffix is
// drawn independently of the key's secret token (via crypto/rand), so it
// guarantees uniqueness for the (organization_id, name) index without embedding
// any secret-derived bytes in a metadata field that surfaces in dashboards and
// admin tooling. Unlike the org-wide plugin/hooks keys (IsOrgWidePluginHooksAPIKey),
// device-agent keys are authorized by scope, not name, so there is no minting
// provenance to encode here.
func DeviceAgentKeyName(userID string) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate device-agent key name suffix: %w", err)
	}
	ts := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("device-agent:%s:%s:%s", userID, ts, hex.EncodeToString(suffix[:])), nil
}

var APIKeyScopes = map[string]APIKeyScope{
	"invalid":    APIKeyScopeInvalid,
	"consumer":   APIKeyScopeConsumer,
	"producer":   APIKeyScopeProducer,
	"chat":       APIKeyScopeChat,
	"hooks":      APIKeyScopeHooks,
	"agent":      APIKeyScopeAgent,
	"agent_user": APIKeyScopeAgentUser,
}

func (scope APIKeyScope) String() string {
	switch scope {
	case APIKeyScopeConsumer:
		return "consumer"
	case APIKeyScopeProducer:
		return "producer"
	case APIKeyScopeChat:
		return "chat"
	case APIKeyScopeHooks:
		return "hooks"
	case APIKeyScopeAgent:
		return "agent"
	case APIKeyScopeAgentUser:
		return "agent_user"
	case APIKeyScopeHooksActingUser:
		return "hooks_acting_user"
	default:
		return "invalid"
	}
}

// effectiveScopes expands a key's raw scopes with the product's one-way scope
// implications: a broader scope grants the narrower scopes it is a superset of.
//   - producer ⇒ consumer, chat
//   - agent ⇒ agent_user (the org install credential is a superset of the
//     per-user data credential, so an existing org key still reads the data
//     endpoints without re-provisioning)
//
// Implications are deliberately one-way: a consumer/chat/agent_user key never
// gains the broader scope. In particular agent_user does NOT imply agent, so a
// leaked per-user key cannot reach the mint endpoint (tokenExchange.exchange).
func effectiveScopes(scopes []string) []string {
	out := slices.Clone(scopes)
	grant := func(have, gain APIKeyScope) {
		if slices.Contains(out, have.String()) && !slices.Contains(out, gain.String()) {
			out = append(out, gain.String())
		}
	}
	grant(APIKeyScopeProducer, APIKeyScopeConsumer)
	grant(APIKeyScopeProducer, APIKeyScopeChat)
	grant(APIKeyScopeAgent, APIKeyScopeAgentUser)
	return out
}

func GetAPIKeyHash(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:]), nil
}

// APIKeyPrefix returns the full key prefix for the given environment
// (e.g., "gram_local_", "gram_test_", "gram_live_").
func APIKeyPrefix(env string) string {
	var keyEnv string
	switch env {
	case "local":
		keyEnv = "local"
	case "dev":
		keyEnv = "test"
	case "prod":
		keyEnv = "live"
	default:
		keyEnv = "local"
	}
	return "gram_" + keyEnv + "_"
}

type ByKey struct {
	keyDB       *repo.Queries
	orgRepo     *orgRepo.Queries
	logger      *slog.Logger
	billingRepo billing.Repository
}

func NewKeyAuth(db *pgxpool.Pool, logger *slog.Logger, billingRepo billing.Repository) *ByKey {
	return &ByKey{
		keyDB:       repo.New(db),
		orgRepo:     orgRepo.New(db),
		logger:      logger,
		billingRepo: billingRepo,
	}
}

func (k *ByKey) KeyBasedAuth(ctx context.Context, key string, requiredScopes []string) (context.Context, error) {
	logger := k.logger
	if key == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	if len(key) >= len("bearer ") && strings.ToLower(key[:len("bearer ")]) == "bearer " {
		key = key[len("bearer "):]
	}

	keyHash, err := GetAPIKeyHash(key)
	if err != nil {
		return ctx, oops.E(oops.CodeUnauthorized, err, "unauthorized: invalid api key")
	}

	apiKey, err := k.keyDB.GetAPIKeyByKeyHash(ctx, keyHash)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ctx, oops.E(oops.CodeUnauthorized, err, "unauthorized: api key not found")
	case err != nil:
		return ctx, oops.E(oops.CodeUnexpected, err, "error loading api key details")
	}

	// LiteLLM keys are touched only after project-header authorization succeeds.
	// This keeps rejected project mismatches out of customer-visible last use.
	if !IsLiteLLMAPIKeyName(apiKey.Name) {
		if err := k.keyDB.UpdateAPIKeyLastAccessedAt(ctx, apiKey.ID); err != nil {
			logger.WarnContext(ctx, "failed to update api key last accessed at",
				attr.SlogError(err),
				attr.SlogOrganizationID(apiKey.OrganizationID),
			)
		}
	}

	scopes := effectiveScopes(apiKey.Scopes)
	for _, scope := range requiredScopes {
		if !slices.Contains(scopes, scope) {
			return ctx, oops.E(oops.CodeForbidden, nil, "api key insufficient scopes")
		}
	}

	org, err := mv.DescribeOrganization(ctx, logger, k.orgRepo, k.billingRepo, apiKey.OrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error loading organization")
	}

	if org.DisabledAt.Valid {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "this organization is disabled, please reach out to support@speakeasy.com for more information")
	}

	var projectID *uuid.UUID
	if apiKey.ProjectID.Valid {
		projectID = &apiKey.ProjectID.UUID
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  apiKey.OrganizationID,
		HasActiveSubscription: org.HasActiveSubscription,
		Whitelisted:           org.Whitelisted,
		UserID:                apiKey.CreatedByUserID,
		Email:                 &apiKey.Email,
		APIKeyID:              apiKey.ID.String(),
		APIKeyName:            apiKey.Name,
		OrgWidePluginHooksKey: IsOrgWidePluginHooksAPIKey(apiKey.Name, key, apiKey.KeyPrefix),
		ProjectID:             projectID,
		OrganizationSlug:      org.Slug,
		AccountType:           org.GramAccountType,
		APIKeyScopes:          scopes,
		ExternalUserID:        "",
		SessionID:             nil,
		ProjectSlug:           nil,
		IsAdmin:               false,
		SupportOrganizationID: "",
	})

	return ctx, nil
}
