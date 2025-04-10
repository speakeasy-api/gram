package auth

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/keys/repo"
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
		return nil, errors.New("unauthorized: api key not provided")
	}

	apiKey, err := k.keyDB.GetAPIKeyByToken(ctx, key)
	if err != nil {
		return nil, errors.New("unauthorized: api key no key found")
	}
	for _, scope := range requiredScopes {
		if !slices.Contains(apiKey.Scopes, scope) {
			return nil, errors.New("unauthorized: api key insufficient scopes")
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
