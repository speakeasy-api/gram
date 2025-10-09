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
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

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

	for _, scope := range requiredScopes {
		if !slices.Contains(apiKey.Scopes, scope) {
			return ctx, oops.E(oops.CodeForbidden, nil, "api key insufficient scopes")
		}
	}

	org, err := mv.DescribeOrganization(ctx, k.logger, k.orgRepo, k.billingRepo, apiKey.OrganizationID)
	if err != nil {
		return ctx, oops.E(oops.CodeUnexpected, err, "error loading organization")
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: apiKey.OrganizationID,
		UserID:               apiKey.CreatedByUserID,
		SessionID:            nil,
		ProjectID:            nil,
		OrganizationSlug:     org.Slug,
		Email:                nil,
		AccountType:          org.GramAccountType,
		ProjectSlug:          nil,
		APIKeyScopes:         apiKey.Scopes,
	})

	return ctx, nil
}
