package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	dsecurity "github.com/speakeasy-api/gram/design/security"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"goa.design/goa/v3/security"
)

type Auth struct {
	sessions *sessions.Sessions
	keys     *ByKey
}

func New(logger *slog.Logger, db *pgxpool.Pool) *Auth {
	return &Auth{
		keys:     NewKeyAuth(db),
		sessions: sessions.NewSessionAuth(logger),
	}
}

func (s *Auth) Authorize(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	if schema == nil {
		panic("GOA has not passed a scheme") // TODO: figure something out here
	}

	switch schema.Name {
	case dsecurity.GramKeySecurityScheme:
		return s.keys.KeyBasedAuth(ctx, key, schema.Scopes)
	case dsecurity.GramSessionSecurityScheme:
		return s.sessions.SessionAuth(ctx, key)
	default:
		return ctx, errors.New("unsupported security scheme")
	}
}
