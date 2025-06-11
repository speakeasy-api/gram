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
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/keys/repo"
	"github.com/speakeasy-api/gram/internal/oops"
)

func GetAPIKeyHash(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:]), nil
}

type ByKey struct {
	keyDB *repo.Queries
}

func NewKeyAuth(db *pgxpool.Pool) *ByKey {
	return &ByKey{
		keyDB: repo.New(db),
	}
}

func (k *ByKey) KeyBasedAuth(ctx context.Context, logger *slog.Logger, key string, requiredScopes []string) (context.Context, error) {
	if key == "" {
		return ctx, oops.C(oops.CodeUnauthorized)
	}

	if len(key) >= len("bearer ") && strings.ToLower(key[:len("bearer ")]) == "bearer " {
		key = key[len("bearer "):]
	}

	keyHash, err := GetAPIKeyHash(key)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized: invalid api key")
	}

	apiKey, err := k.keyDB.GetAPIKeyByKeyHash(ctx, keyHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized: api key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading api key details")
	}

	// TODO: Temporary
	logger.InfoContext(ctx, "checking key scopes", slog.String("key_scopes", strings.Join(apiKey.Scopes, ",")), slog.String("required_scopes", strings.Join(requiredScopes, ",")))
	for _, scope := range requiredScopes {
		if !slices.Contains(apiKey.Scopes, scope) {
			return nil, oops.E(oops.CodeForbidden, nil, "api key insufficient scopes")
		}
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: apiKey.OrganizationID,
		UserID:               apiKey.CreatedByUserID,
		SessionID:            nil,
		ProjectID:            nil,
	})

	return ctx, nil
}
