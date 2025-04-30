package auth

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/keys/repo"
	"github.com/speakeasy-api/gram/internal/oops"
)

type ByKey struct {
	keyDB *repo.Queries
}

func NewKeyAuth(db *pgxpool.Pool) *ByKey {
	return &ByKey{
		keyDB: repo.New(db),
	}
}

func (k *ByKey) KeyBasedAuth(ctx context.Context, key string, requiredScopes []string) (context.Context, error) {
	if key == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	apiKey, err := k.keyDB.GetAPIKeyByToken(ctx, key)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeUnauthorized, err, "unauthorized: api key not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading api key details")
	}

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
