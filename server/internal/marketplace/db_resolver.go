package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
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
	logger  *slog.Logger
	queries *pluginsrepo.Queries
	tokens  InstallationTokenSource
}

func NewDBResolver(logger *slog.Logger, db *pgxpool.Pool, tokens InstallationTokenSource) *DBResolver {
	return &DBResolver{logger: logger.With(attr.SlogComponent("marketplace")), queries: pluginsrepo.New(db), tokens: tokens}
}

// NewLocalDBResolver resolves the same opaque marketplace tokens as production
// without minting a GitHub installation token. It is used only with LocalServer,
// which reads from the standard local in-memory publisher rather than GitHub.
func NewLocalDBResolver(logger *slog.Logger, db *pgxpool.Pool) *DBResolver {
	return &DBResolver{logger: logger.With(attr.SlogComponent("marketplace")), queries: pluginsrepo.New(db), tokens: nil}
}

func (r *DBResolver) Resolve(ctx context.Context, token string) (Upstream, error) {
	if token == "" {
		return Upstream{}, ErrNotFound
	}

	conn, err := r.lookup(ctx, token)
	if err != nil {
		return Upstream{}, err
	}

	instToken := ""
	if r.tokens != nil {
		instToken, err = r.tokens.InstallationToken(ctx, conn.InstallationID)
		if err != nil {
			return Upstream{}, fmt.Errorf("mint installation token (installation %d): %w", conn.InstallationID, err)
		}
	}

	return Upstream{
		Token:       token,
		Owner:       conn.RepoOwner,
		Repo:        conn.RepoName,
		AccessToken: instToken,
	}, nil
}

// lookup finds the connection a token belongs to by the token's hash. Until
// the marketplace token backfill moves every connection, a token stored in
// the deprecated plaintext column still resolves; each such lookup is logged
// so the backfill's progress shows in the logs.
func (r *DBResolver) lookup(ctx context.Context, token string) (pluginsrepo.PluginGithubConnection, error) {
	row, err := r.queries.ResolveGitHubConnectionByMarketplaceToken(ctx, pluginsrepo.ResolveGitHubConnectionByMarketplaceTokenParams{
		MarketplaceTokenHash: pgtype.Text{String: marketplacetokens.Hash(token), Valid: true},
		MarketplaceToken:     token,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return pluginsrepo.PluginGithubConnection{}, ErrNotFound
	case err != nil:
		return pluginsrepo.PluginGithubConnection{}, fmt.Errorf("lookup connection by marketplace token: %w", err)
	}

	if row.PlaintextToken {
		r.logger.InfoContext(ctx, "marketplace token resolved from the plaintext column; run the marketplace token backfill",
			attr.SlogProjectID(row.PluginGithubConnection.ProjectID.String()))
	}

	return row.PluginGithubConnection, nil
}
