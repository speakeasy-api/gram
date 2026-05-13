package marketplace

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// InstallationTokenSource mints short-lived GitHub App installation tokens.
// Implemented by *thirdparty/github.Client. Decoupled here so the marketplace
// package doesn't depend on the wider github client surface.
type InstallationTokenSource interface {
	InstallationToken(ctx context.Context, installationID int64) (string, error)
}

// DBResolver resolves a marketplace URL token to an upstream by reading
// plugin_github_connections and minting a fresh GitHub App installation token
// for use as the git fetch credential.
type DBResolver struct {
	queries *pluginsrepo.Queries
	tokens  InstallationTokenSource
}

func NewDBResolver(db *pgxpool.Pool, tokens InstallationTokenSource) *DBResolver {
	return &DBResolver{queries: pluginsrepo.New(db), tokens: tokens}
}

func (r *DBResolver) Resolve(ctx context.Context, token string) (Upstream, error) {
	if token == "" {
		return Upstream{}, ErrNotFound
	}

	conn, err := r.queries.GetGitHubConnectionByMarketplaceToken(ctx, pgtype.Text{String: token, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Upstream{}, ErrNotFound
		}
		return Upstream{}, fmt.Errorf("lookup connection by marketplace token: %w", err)
	}

	instToken, err := r.tokens.InstallationToken(ctx, conn.InstallationID)
	if err != nil {
		return Upstream{}, fmt.Errorf("mint installation token (installation %d): %w", conn.InstallationID, err)
	}

	return Upstream{
		Token:       token,
		Owner:       conn.RepoOwner,
		Repo:        conn.RepoName,
		AccessToken: instToken,
	}, nil
}
