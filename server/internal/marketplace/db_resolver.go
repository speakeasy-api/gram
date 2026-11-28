package marketplace

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

// RepositoryClient verifies the current GitHub repository identity and returns
// a client whose installation token is scoped to that repository ID.
type RepositoryClient interface {
	ForRepository(ctx context.Context, installationID, repositoryID int64, owner, name string) (*ghclient.Client, error)
}

// DBResolver resolves a marketplace URL token to a verified repository binding
// and obtains a repository-scoped credential for Git fetches.
type DBResolver struct {
	logger  *slog.Logger
	queries *pluginsrepo.Queries
	github  RepositoryClient
	local   bool
}

func NewDBResolver(logger *slog.Logger, db *pgxpool.Pool, github RepositoryClient) *DBResolver {
	return &DBResolver{logger: logger.With(attr.SlogComponent("marketplace")), queries: pluginsrepo.New(db), github: github, local: false}
}

// NewLocalDBResolver resolves the same opaque marketplace tokens as production
// without minting a GitHub installation token. It is used only with LocalServer,
// which reads from the standard local in-memory publisher rather than GitHub.
func NewLocalDBResolver(logger *slog.Logger, db *pgxpool.Pool) *DBResolver {
	return &DBResolver{logger: logger.With(attr.SlogComponent("marketplace")), queries: pluginsrepo.New(db), github: nil, local: true}
}

func (r *DBResolver) Resolve(ctx context.Context, token string) (Upstream, error) {
	if token == "" {
		return Upstream{}, ErrNotFound
	}

	conn, err := r.lookup(ctx, token)
	if err != nil {
		return Upstream{}, err
	}

	if r.local {
		return Upstream{Token: token, Owner: conn.RepoOwner, Repo: conn.RepoName, AccessToken: ""}, nil
	}

	binding, err := r.queries.GetPluginRepositoryBinding(ctx, conn.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Upstream{}, ErrNotFound
	}
	if err != nil {
		return Upstream{}, fmt.Errorf("get marketplace repository binding: %w", err)
	}
	if binding.InstallationID != conn.InstallationID ||
		!strings.EqualFold(binding.RepoOwner, conn.RepoOwner) ||
		!strings.EqualFold(binding.RepoName, conn.RepoName) {
		return Upstream{}, ErrNotFound
	}

	client, err := r.github.ForRepository(ctx, binding.InstallationID, binding.RepositoryID, binding.RepoOwner, binding.RepoName)
	if err != nil {
		return Upstream{}, fmt.Errorf("verify marketplace repository binding: %w", err)
	}
	accessToken, err := client.InstallationToken(ctx, binding.InstallationID)
	if err != nil {
		return Upstream{}, fmt.Errorf("get marketplace repository token: %w", err)
	}
	return Upstream{
		Token:       token,
		Owner:       binding.RepoOwner,
		Repo:        binding.RepoName,
		AccessToken: accessToken,
	}, nil
}

// lookup finds the connection a token belongs to by the token's hash. A token
// whose organization is disabled is not found, so no installation token is
// minted for it. Until the marketplace token backfill moves every connection,
// a token stored in the deprecated plaintext column still resolves; each such
// lookup is logged so the backfill's progress shows in the logs.
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

	if row.OrganizationDisabled {
		r.logger.InfoContext(ctx, "refuse marketplace token for disabled organization",
			attr.SlogProjectID(row.PluginGithubConnection.ProjectID.String()), attr.SlogReason("organization_disabled"))
		return pluginsrepo.PluginGithubConnection{}, ErrNotFound
	}

	if row.PlaintextToken {
		r.logger.InfoContext(ctx, "marketplace token resolved from the plaintext column; run the marketplace token backfill",
			attr.SlogProjectID(row.PluginGithubConnection.ProjectID.String()))
	}

	return row.PluginGithubConnection, nil
}
