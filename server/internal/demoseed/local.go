package demoseed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/dev-idp/pkg/devidentity"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// Fixed local-only material. These are deliberately well-known constants, not
// generated values: everything that used to be written back into
// mise.local.toml after a seed run (the API key) is now a checked-in default
// in mise.toml, so a fresh clone works without a round trip through the seed.
//
// Safe because they only ever exist in a developer's local database: the
// server refuses `--local` material for any other tenant, and the prefix
// marks them as local (`gram_local_`).
const (
	// LocalAPIKeyName is the api_keys row the local fixtures maintain.
	LocalAPIKeyName = "seed-key"
	// LocalAPIKeyToken is the random half of the local API key. The full key
	// is auth.APIKeyPrefix(env) + this.
	LocalAPIKeyToken = "5eed10ca15eed10ca15eed10ca15eed10ca15eed10ca15eed10ca15eed10ca11"
)

// Fixed uuids for the local-only rows, derived from LocalSpec's prefix so they
// live in the same identifier family as everything else the seed writes and
// can be published as static environment defaults.
func localEnvironmentID() string    { return LocalSpec().FixedUUID("0000-4000-a000-000000007004") }
func localAPIKeyID() string         { return LocalSpec().FixedUUID("0000-4000-a000-000000007005") }
func localMCPAppAssetID() string    { return LocalSpec().FixedUUID("0000-4000-a000-000000007006") }
func localMCPAppFunctionID() string { return LocalSpec().FixedUUID("0000-4000-a000-000000007007") }

// LocalFixturesOptions configures RunLocalFixtures.
type LocalFixturesOptions struct {
	// DeveloperEmail identifies the developer to adopt into the local org.
	// Empty means "read it from `git config user.email`", which is exactly
	// what the dev-idp does when it bootstraps its default user.
	DeveloperEmail string
	// Environment decides the API key prefix (auth.APIKeyPrefix).
	Environment string
	// ObservedEnv holds the current values of StaleOverrideVars, so the
	// fixtures can warn when mise.local.toml is shadowing them.
	ObservedEnv map[string]string
}

// RunLocalFixtures adds everything the local development organization needs on
// top of the seeded data, none of which belongs in the shared demo org:
//
//   - you, as a real member with the Admin role and platform super-admin;
//   - the system roles and their grants, plus a direct chat:read grant so the
//     Agent Sessions list shows the seeded chats rather than only your own;
//   - a fixed API key and default environment, both with well-known ids so
//     mise.toml can hardcode them;
//   - the global MCP registry row, which is not tenant-scoped and so cannot
//     live in the seed proper.
//
// Every write is idempotent, and every one is scoped to LocalSpec's org.
// Call it only after Run has completed with LocalSpec: the seed deletes and
// recreates the org's memberships, so adopting you has to come after it.
func RunLocalFixtures(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, blob assets.BlobStore, cache *redis.Client, opts LocalFixturesOptions) error {
	spec := LocalSpec()
	logger = logger.With(attr.SlogComponent("demoseed.local"), attr.SlogOrganizationID(spec.OrgID))

	dev, err := resolveDeveloper(ctx, opts.DeveloperEmail)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "adopting developer into the local org", attr.SlogUserID(dev.ID))

	// The system roles are a prerequisite for the Admin assignment below and
	// are not org-specific data the seed can fabricate — provisioning owns
	// them in every other environment, so reuse its seeder rather than
	// duplicating the grant lists.
	if err := authz.SeedSystemRoleGrants(ctx, db, spec.OrgID); err != nil {
		return fmt.Errorf("seed system role grants: %w", err)
	}

	env := opts.Environment
	if env == "" {
		env = "local"
	}
	apiKey := auth.APIKeyPrefix(env) + LocalAPIKeyToken
	apiKeyHash, err := auth.GetAPIKeyHash(apiKey)
	if err != nil {
		return fmt.Errorf("hash local api key: %w", err)
	}
	// Upload the MCP App bundle before the transaction: the assets row has to
	// point at a blob that already exists, and the write is idempotent by
	// object name.
	app, err := uploadMCPAppArchive(ctx, blob)
	if err != nil {
		return err
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin local fixtures transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			logger.ErrorContext(ctx, "roll back local fixtures", attr.SlogError(err))
		}
	}()

	for _, step := range []struct {
		name string
		sql  string
		args []any
	}{
		{"link org to the dev-idp", localLinkWorkOSOrgSQL, []any{spec.OrgID, spec.WorkOSOrgID}},
		{"adopt developer", localAdoptDeveloperSQL, []any{spec.OrgID, dev.ID, dev.Email, dev.Name}},
		{"grant session visibility", localSessionVisibilitySQL, []any{spec.OrgID, dev.ID}},
		{"enable platform mcp", localPlatformMCPFeatureSQL, []any{spec.OrgID}},
		{"api key", localAPIKeySQL, []any{spec.OrgID, spec.ProjectID(), dev.ID, localAPIKeyID(), LocalAPIKeyName, apiKeyHash, apiKey[:len(auth.APIKeyPrefix(env))+5]}},
		{"default environment", localEnvironmentSQL, []any{spec.OrgID, spec.ProjectID(), localEnvironmentID()}},
		{"mcp registry", localMCPRegistrySQL, nil},
		{"mcp app asset", localMCPAppAssetSQL, []any{
			localMCPAppAssetID(), spec.ProjectID(), spec.OrgID, app.url, app.size, app.sha256,
		}},
		{"mcp app function", localMCPAppSQL, []any{
			spec.ProjectID(), spec.DeploymentID(), localMCPAppFunctionID(), localMCPAppAssetID(),
			MCPAppFunctionSlug, MCPAppRuntime, mcpAppToolURN(), MCPAppToolName,
			mcpAppToolDescription, mcpAppInputSchema, MCPAppResourceURI, mcpAppResourceURN(),
		}},
		{"mcp app toolset entry", localMCPAppToolsetSQL, []any{spec.OrgID, mcpAppToolURN(), mcpAppResourceURN(), MCPAppToolsetSlug}},
	} {
		if _, err := tx.Exec(ctx, step.sql, step.args...); err != nil {
			return fmt.Errorf("local fixture %q: %w", step.name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit local fixtures: %w", err)
	}

	logger.InfoContext(ctx, "local fixtures applied")

	warnOnStaleOverrides(ctx, logger, opts.ObservedEnv, apiKey)
	bustLocalCaches(ctx, logger, cache, spec.OrgID, dev.ID)
	return nil
}

// warnOnStaleOverrides catches the one way this seed can leave a developer
// stranded. The values below used to be GENERATED per machine and written into
// mise.local.toml by the old seed script; they are now fixed and ship in
// mise.toml. mise.local.toml wins over mise.toml, so a worktree that predates
// this change keeps serving the old values — an API key that no longer exists
// — with nothing to explain why.
//
// The observed environment is passed in rather than read here: the caller in
// cmd/ owns environment access.
func warnOnStaleOverrides(ctx context.Context, logger *slog.Logger, env map[string]string, apiKey string) {
	for _, v := range []struct{ name, want string }{
		{"GRAM_API_KEY", apiKey},
	} {
		if got := env[v.name]; got != "" && got != v.want {
			logger.WarnContext(ctx, fmt.Sprintf(
				"%s holds a stale value from a previous seed and is overriding this one; delete the line from mise.local.toml (it is a checked-in default in mise.toml now)",
				v.name))
		}
	}
}

// StaleOverrideVars names the environment variables RunLocalFixtures checks
// for stale values left behind by the seed script this one replaced.
func StaleOverrideVars() []string {
	return []string{
		"GRAM_API_KEY",
	}
}

// developer is the local user the fixtures adopt into the org.
type developer struct {
	ID    string
	Email string
	Name  string
}

// resolveDeveloper mirrors the dev-idp's default-user bootstrap: the git
// committer identity, with the user id derived from the email. Deriving it
// rather than asking the dev-idp means the seed can run before anyone has
// logged in — which is the normal case on a fresh database.
func resolveDeveloper(ctx context.Context, email string) (developer, error) {
	if email == "" {
		out, err := exec.CommandContext(ctx, "git", "config", "--get", "user.email").Output()
		if err != nil {
			return developer{}, fmt.Errorf("the local seed identifies you by `git config user.email`, pass --local-user-email to override: %w", err)
		}
		email = strings.TrimSpace(string(out))
	}
	if email == "" {
		return developer{}, errors.New("the local seed identifies you by `git config user.email`, which is empty; pass --local-user-email")
	}

	name := email
	if out, err := exec.CommandContext(ctx, "git", "config", "--get", "user.name").Output(); err == nil {
		if n := strings.TrimSpace(string(out)); n != "" {
			name = n
		}
	}

	return developer{
		ID:    devidentity.DeterministicUserID(email).String(),
		Email: email,
		Name:  name,
	}, nil
}

// bustLocalCaches drops the cached entitlement and user-info entries the
// fixtures just invalidated. Best effort: a stale cache only means a running
// server needs a restart, so a failure is logged rather than returned.
func bustLocalCaches(ctx context.Context, logger *slog.Logger, cache *redis.Client, orgID, userID string) {
	if cache == nil {
		return
	}

	// Built through the owning packages' key functions rather than assembled
	// by hand: redis.Del matches exactly, so a key that is even one character
	// off deletes nothing and reports success.
	keys := []string{sessions.UserInfoCacheKey(userID)}
	for _, feature := range []productfeatures.Feature{
		productfeatures.FeatureLogs,
		productfeatures.FeatureToolIOLogs,
		productfeatures.FeatureSessionCapture,
		productfeatures.FeatureSkills,
		productfeatures.FeaturePlatformMCP,
	} {
		keys = append(keys, productfeatures.FeatureCacheKey(orgID, feature))
	}
	if err := cache.Del(ctx, keys...).Err(); err != nil {
		logger.WarnContext(ctx, "could not clear the local feature cache; restart the server if entitlements look stale", attr.SlogError(err))
	}
}

// Stamp the WorkOS link the auth callback would otherwise add on first login.
// Setting it up front means org lookups by workos_id resolve before anyone has
// logged in, and the callback finds an already-linked row to update rather
// than a bare one. It is local-only because the shared demo org has no WorkOS
// counterpart at all.
const localLinkWorkOSOrgSQL = `
UPDATE organization_metadata
SET workos_id = $2, updated_at = clock_timestamp()
WHERE id = $1 AND workos_id IS DISTINCT FROM $2
`

// You, as a real member. users.workos_id doubles as the WorkOS subject the
// dev-idp will present at login, and the dev-idp uses the same user id there,
// so the row survives your first real login unchanged.
const localAdoptDeveloperSQL = `
WITH dev AS (
  INSERT INTO users (id, email, display_name, workos_id, admin)
  VALUES ($2, $3, $4, $2, TRUE)
  ON CONFLICT (id) DO UPDATE
    SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
        workos_id = COALESCE(users.workos_id, EXCLUDED.workos_id),
        admin = TRUE, updated_at = clock_timestamp()
  RETURNING id, workos_id
),
membership AS (
  INSERT INTO organization_user_relationships
    (organization_id, user_id, workos_user_id, workos_membership_id)
  SELECT $1, dev.id, dev.workos_id, 'devidp_mem_' || dev.id
  FROM dev
  ON CONFLICT (organization_id, user_id) WHERE deleted IS FALSE
  DO UPDATE SET
    workos_user_id = EXCLUDED.workos_user_id,
    workos_membership_id = EXCLUDED.workos_membership_id,
    deleted_at = NULL, updated_at = clock_timestamp()
  RETURNING user_id, workos_user_id, workos_membership_id
),
admin_role AS (
  SELECT id FROM global_roles
  WHERE workos_slug = 'admin' AND deleted IS FALSE AND workos_deleted IS FALSE
  LIMIT 1
)
INSERT INTO organization_role_assignments
  (organization_id, workos_user_id, user_id, role_urn, workos_membership_id, workos_updated_at)
SELECT $1, membership.workos_user_id, membership.user_id,
       'role:global:' || admin_role.id::text, membership.workos_membership_id, clock_timestamp()
FROM membership CROSS JOIN admin_role
ON CONFLICT (organization_id, workos_user_id, role_urn) WHERE deleted_at IS NULL
DO UPDATE SET deleted_at = NULL, updated_at = clock_timestamp()
`

// chat:read is deliberately absent from the Admin system role, so the seeded
// chats (owned by the fictional teammates) would otherwise be invisible:
// without it chatVisibilityScope falls back to own-sessions-only.
const localSessionVisibilitySQL = `
INSERT INTO principal_grants (organization_id, principal_urn, scope, effect, selectors)
VALUES ($1, 'user:' || $2, 'chat:read', NULL,
        jsonb_build_object('resource_kind', 'chat', 'resource_id', '*'))
ON CONFLICT (organization_id, principal_urn, scope, COALESCE(effect, 'allow'), selectors) DO NOTHING
`

// platform_mcp is the default-on organization entitlement locally; the shared
// demo org deliberately does not enable it (its Plugins pages stay empty).
const localPlatformMCPFeatureSQL = `
INSERT INTO organization_features (organization_id, feature_name)
VALUES ($1, 'platform_mcp')
ON CONFLICT (organization_id, feature_name) WHERE deleted IS FALSE DO NOTHING
`

// A key with a well-known hash. The plaintext is never stored, so the row is
// upserted by id and the value lives in mise.toml.
const localAPIKeySQL = `
INSERT INTO api_keys (id, organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes)
VALUES ($4, $1, $2, $3, $5, $7, $6, ARRAY['producer', 'chat'])
ON CONFLICT (id) DO UPDATE
  SET organization_id = EXCLUDED.organization_id, project_id = EXCLUDED.project_id,
      created_by_user_id = EXCLUDED.created_by_user_id, name = EXCLUDED.name,
      key_prefix = EXCLUDED.key_prefix, key_hash = EXCLUDED.key_hash,
      scopes = EXCLUDED.scopes, deleted_at = NULL, updated_at = clock_timestamp()
`

const localEnvironmentSQL = `
INSERT INTO environments (id, organization_id, project_id, name, slug)
VALUES ($3, $1, $2, 'Default', 'default')
ON CONFLICT (project_id, slug) WHERE deleted IS FALSE
DO UPDATE SET name = EXCLUDED.name, updated_at = clock_timestamp()
`

// The default registry backing the Catalog page. Global, not tenant-scoped,
// which is precisely why it cannot live in the tenant seed.
const localMCPRegistrySQL = `
INSERT INTO mcp_registries (name, url)
VALUES ('Gram Recommended', 'https://api.pulsemcp.com')
ON CONFLICT (url) WHERE deleted IS FALSE DO NOTHING
`
