package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"

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
)

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

	// Best-effort update of last accessed timestamp - don't fail auth if this fails
	if err := k.keyDB.UpdateAPIKeyLastAccessedAt(ctx, apiKey.ID); err != nil {
		logger.WarnContext(ctx, "failed to update api key last accessed at",
			attr.SlogError(err),
			attr.SlogOrganizationID(apiKey.OrganizationID),
		)
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
