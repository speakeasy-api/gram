package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"strings"

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
)

var APIKeyScopes = map[string]APIKeyScope{
	"invalid":  APIKeyScopeInvalid,
	"consumer": APIKeyScopeConsumer,
	"producer": APIKeyScopeProducer,
	"chat":     APIKeyScopeChat,
}

func (scope APIKeyScope) String() string {
	switch scope {
	case APIKeyScopeConsumer:
		return "consumer"
	case APIKeyScopeProducer:
		return "producer"
	case APIKeyScopeChat:
		return "chat"
	default:
		return "invalid"
	}
}

func GetAPIKeyHash(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:]), nil
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
	case errors.Is(err, sql.ErrNoRows):
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

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID:  apiKey.OrganizationID,
		HasActiveSubscription: org.HasActiveSubscription,
		UserID:                apiKey.CreatedByUserID,
		ExternalUserID:        "",
		APIKeyID:              apiKey.ID.String(),
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      org.Slug,
		Email:                 nil,
		AccountType:           org.GramAccountType,
		ProjectSlug:           nil,
		APIKeyScopes:          scopes,
	})

	return ctx, nil
}
