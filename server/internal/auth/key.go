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

func (k *ByKey) KeyBasedAuth(ctx context.Context, key string, scopes []string) (context.Context, error) {
	if key == "" {
		return nil, errors.New("unauthorized: api key not provided")
	}

	apiKey, err := k.keyDB.GetAPIKeyByToken(ctx, key)
	if err != nil {
		return nil, err
	}
	for _, scope := range scopes {
		if !slices.Contains(apiKey.Scopes, scope) {
			return nil, errors.New("unauthorized: api key scope does not match")
		}
	}

	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
		ActiveOrganizationID: apiKey.OrganizationID,
		UserID:               apiKey.CreatedByUserID,
	})

	return ctx, nil
}
