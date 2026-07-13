package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
)

// PluginAPIKeyNamePrefix is reserved for keys minted by plugin distribution
// flows. Historical user-created keys may still carry this prefix, so callers
// classifying org-wide hook keys must use IsOrgWidePluginHooksAPIKeyName.
const PluginAPIKeyNamePrefix = "plugins-"

// IsOrgWidePluginHooksAPIKeyName recognizes the names minted by plugin publish
// and observability-download flows. Prefix-only checks are unsafe because API
// key names were unrestricted before PluginAPIKeyNamePrefix became reserved;
// a legacy personal hooks key may legitimately be named "plugins-hooks".
func IsOrgWidePluginHooksAPIKeyName(name string) bool {
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
	if len(parts) != 3 || len(parts[2]) != 6 {
		return false
	}
	if _, err := time.Parse("20060102-150405", parts[0]+"-"+parts[1]); err != nil {
		return false
	}
	tokenSuffix, err := hex.DecodeString(parts[2])
	return err == nil && len(tokenSuffix) == 3
}

var APIKeyScopes = map[string]APIKeyScope{
	"invalid":  APIKeyScopeInvalid,
	"consumer": APIKeyScopeConsumer,
	"producer": APIKeyScopeProducer,
	"chat":     APIKeyScopeChat,
	"hooks":    APIKeyScopeHooks,
	"agent":    APIKeyScopeAgent,
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
	default:
		return "invalid"
	}
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

	// Best-effort update of last accessed timestamp - don't fail auth if this fails
	if err := k.keyDB.UpdateAPIKeyLastAccessedAt(ctx, apiKey.ID); err != nil {
		logger.WarnContext(ctx, "failed to update api key last accessed at",
			attr.SlogError(err),
			attr.SlogOrganizationID(apiKey.OrganizationID),
		)
	}

	// a bit of a hack right now, the product intends to allow producer keys to act as a superset of consumer and chat keys
	scopes := slices.Clone(apiKey.Scopes)
	if slices.Contains(scopes, APIKeyScopeProducer.String()) && !slices.Contains(scopes, APIKeyScopeConsumer.String()) {
		scopes = append(scopes, APIKeyScopeConsumer.String())
	}
	if slices.Contains(scopes, APIKeyScopeProducer.String()) && !slices.Contains(scopes, APIKeyScopeChat.String()) {
		scopes = append(scopes, APIKeyScopeChat.String())
	}

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
		ProjectID:             projectID,
		OrganizationSlug:      org.Slug,
		AccountType:           org.GramAccountType,
		APIKeyScopes:          scopes,
		ExternalUserID:        "",
		SessionID:             nil,
		ProjectSlug:           nil,
		IsAdmin:               false,
	})

	return ctx, nil
}
