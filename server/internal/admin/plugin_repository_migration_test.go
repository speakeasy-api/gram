package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
)

const testPluginRepositoryInstallation = int64(345)

type migrationGitHubRepository struct {
	installationID int64
	repositoryID   int64
	owner          string
	name           string
}

type migrationGitHub struct {
	verified []migrationGitHubRepository
	created  []migrationGitHubRepository
	verify   func(migrationGitHubRepository) error
}

func (g *migrationGitHub) ForRepository(_ context.Context, installationID, repositoryID int64, owner, name string) (plugins.GitHubRepository, error) {
	repository := migrationGitHubRepository{installationID, repositoryID, owner, name}
	g.verified = append(g.verified, repository)
	return nil, g.verify(repository)
}

func (g *migrationGitHub) CreatePrivateRepository(_ context.Context, installationID int64, owner, name string) (int64, error) {
	id := int64(777)
	g.created = append(g.created, migrationGitHubRepository{installationID, id, owner, name})
	return id, nil
}

func seedPluginRepositoryMigration(t *testing.T, ctx context.Context, db *pgxpool.Pool, slug string) *migrationOrg {
	t.Helper()
	org := newMigrationOrg(t, ctx, db, slug)
	_, err := db.Exec(ctx, //nolint:glint // notestingrawsql: seed legacy encrypted values and snapshots without invoking the new publish path.
		`INSERT INTO plugin_github_connections
		(project_id, organization_id, installation_id, repo_owner, repo_name, marketplace_token_hash, marketplace_token_encrypted,
		 marketplace_token_org_encrypted, published_fingerprint, published_mcp_fingerprints, published_hooks_version, published_hooks_config)
		VALUES ($1, $2, $3, 'publisher', $4, $4, 'sealed-token', 'org-sealed-token', 'legacy', '{"__shared__":"old"}', 'old',
		 '{"published_marketplace_name":"stable-name","server_url":"https://old.example.test"}')`,
		org.projectID, org.id, testPluginRepositoryInstallation, slug)
	require.NoError(t, err)
	return org
}

func requirePluginRepositoryError(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}

func TestPluginRepositoryMigrationApply(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	staff := withPlatformCredentialStaff(ctx)
	seedPlatformCredentialStaff(t, db, platformCredentialStaffEmail, true)
	first := seedPluginRepositoryMigration(t, ctx, db, "plugin-apply-first")
	second := seedPluginRepositoryMigration(t, ctx, db, "plugin-apply-second")
	third := seedPluginRepositoryMigration(t, ctx, db, "plugin-apply-third")
	github := &migrationGitHub{verify: func(repo migrationGitHubRepository) error {
		switch repo.repositoryID {
		case 12:
			return ghclient.ErrRepositoryConflict
		case 13:
			return errors.New("GitHub unavailable")
		default:
			return nil
		}
	}}
	svc.SetPluginRepositoryGitHub(&plugins.GitHubConfig{Client: github, Org: "Publisher", InstallationID: testPluginRepositoryInstallation})
	entry := func(org *migrationOrg, id string) *gen.AdminPluginRepositoryMigrationEntry {
		return &gen.AdminPluginRepositoryMigrationEntry{ProjectID: org.projectID.String(), RepositoryID: id, Evidence: "historical record reference"}
	}
	apply := func(dryRun bool, entries ...*gen.AdminPluginRepositoryMigrationEntry) []string {
		t.Helper()
		result, err := svc.ApplyPluginRepositoryMigrations(staff, &gen.ApplyPluginRepositoryMigrationsPayload{DryRun: dryRun, Entries: entries})
		require.NoError(t, err)
		require.Len(t, result.Items, len(entries))
		statuses := make([]string, len(entries))
		for i, item := range result.Items {
			require.Equal(t, entries[i].ProjectID, item.ProjectID)
			statuses[i] = item.Status
		}
		return statuses
	}

	for _, entries := range [][]*gen.AdminPluginRepositoryMigrationEntry{
		{entry(first, "11"), {ProjectID: strings.ToUpper(first.projectID.String()), RepositoryID: "22", Evidence: "record"}},
		{entry(first, "11"), entry(second, "011")},
	} {
		for _, dryRun := range []bool{true, false} {
			_, err := svc.ApplyPluginRepositoryMigrations(staff, &gen.ApplyPluginRepositoryMigrationsPayload{DryRun: dryRun, Entries: entries})
			requirePluginRepositoryError(t, err, oops.CodeBadRequest)
		}
	}
	require.Empty(t, github.verified, "duplicate batches must fail before contacting GitHub or writing any binding")
	require.Equal(t, []string{"verified"}, apply(true, entry(first, "15000000000000001")))
	_, err := pluginsrepo.New(db).GetPluginRepositoryBinding(ctx, first.projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, []string{"bound", "conflict", "unavailable"}, apply(false, entry(first, "15000000000000001"), entry(second, "12"), entry(third, "13")))
	calls := len(github.verified)
	require.Equal(t, []string{"already_bound"}, apply(false, entry(first, "15000000000000001")))
	require.Equal(t, []string{"conflict", "conflict"}, apply(false, entry(first, "999"), entry(second, "15000000000000001")))
	require.Len(t, github.verified, calls, "known conflicts and retries do not call GitHub")
	require.Equal(t, migrationGitHubRepository{testPluginRepositoryInstallation, 15000000000000001, "publisher", "plugin-apply-first"}, github.verified[0])
	require.Empty(t, github.created)

	var count int
	err = db.QueryRow(ctx, //nolint:glint // notestingrawsql: assert internal audit events and their metadata, which current list queries redact.
		`SELECT count(*) FROM audit_logs WHERE project_id = $1 AND action = $2 AND is_internal
		AND actor_id = 'sub-operator' AND subject_type = 'plugin_repository' AND acting_surface = 'admin'
		AND metadata ->> 'evidence' = 'historical record reference'`, first.projectID, string(audit.ActionPluginRepositoryBind)).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	list, err := svc.ListPluginRepositoryMigrations(staff, &gen.ListPluginRepositoryMigrationsPayload{})
	require.NoError(t, err)
	require.Len(t, list.Items, 3)
	var prev string
	for _, row := range list.Items {
		require.Greater(t, row.ProjectID, prev)
		prev = row.ProjectID
		require.Equal(t, row.ProjectID == first.projectID.String(), row.Bound)
	}
	list, err = svc.ListPluginRepositoryMigrations(staff, &gen.ListPluginRepositoryMigrationsPayload{After: new(prev)})
	require.NoError(t, err)
	require.Empty(t, list.Items)

	// A hard delete removes the project without making its historical GitHub
	// repository ID eligible to be claimed by another project.
	_, err = db.Exec(ctx, "DELETE FROM projects WHERE id = $1", first.projectID) //nolint:glint // notestingrawsql: exercise the foreign key's hard-delete behavior; the project API soft-deletes.
	require.NoError(t, err)
	_, err = pluginsrepo.New(db).GetPluginRepositoryBinding(ctx, first.projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, []string{"conflict"}, apply(false, entry(second, "15000000000000001")))
	require.Len(t, github.verified, calls)
}

func TestPluginRepositoryMigrationPagination(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	staff := withPlatformCredentialStaff(ctx)
	org := newMigrationOrg(t, ctx, db, "plugin-repository-pages")
	const lastProjectID = "00000000-0000-0000-0000-000000000101"
	// Model a first publish that already created its binding but has not yet
	// persisted a connection, alongside older connections without a binding.
	_, err := db.Exec(ctx, //nolint:glint // notestingrawsql: seed a complete page of legacy rows with deterministic ordering.
		`WITH created AS (
		INSERT INTO projects (id, organization_id, name, slug)
		SELECT ('00000000-0000-0000-0000-' || lpad(n::text, 12, '0'))::uuid, $1, 'project-' || n, 'project-' || n
		FROM generate_series(1, 101) AS n
		RETURNING id, slug
	)
	INSERT INTO plugin_github_connections (project_id, organization_id, installation_id, repo_owner, repo_name)
	SELECT id, $1, $2, 'publisher', slug FROM created WHERE id <> $3`, org.id, testPluginRepositoryInstallation, lastProjectID)
	require.NoError(t, err)
	require.NoError(t, pluginsrepo.New(db).InsertPluginRepositoryBinding(ctx, pluginsrepo.InsertPluginRepositoryBindingParams{
		ProjectID: uuid.MustParse(lastProjectID), RepositoryID: 123, InstallationID: testPluginRepositoryInstallation,
		RepoOwner: "publisher", RepoName: "binding-only",
	}))
	first, err := svc.ListPluginRepositoryMigrations(staff, &gen.ListPluginRepositoryMigrationsPayload{})
	require.NoError(t, err)
	require.Len(t, first.Items, 100)
	require.NotNil(t, first.NextCursor)
	require.Equal(t, first.Items[99].ProjectID, *first.NextCursor)
	second, err := svc.ListPluginRepositoryMigrations(staff, &gen.ListPluginRepositoryMigrationsPayload{After: first.NextCursor})
	require.NoError(t, err)
	require.Equal(t, []*gen.AdminPluginRepositoryMigration{{ProjectID: lastProjectID, OrganizationID: org.id,
		RepoOwner: "publisher", RepoName: "binding-only", Bound: true}}, second.Items)
	require.Nil(t, second.NextCursor)
}

func TestPluginRepositoryMigrationAuthorization(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	staff := withPlatformCredentialStaff(ctx)
	seedPlatformCredentialStaff(t, db, platformCredentialStaffEmail, false)
	org := seedPluginRepositoryMigration(t, ctx, db, "plugin-denied")

	_, err := svc.ListPluginRepositoryMigrations(ctx, &gen.ListPluginRepositoryMigrationsPayload{})
	requirePluginRepositoryError(t, err, oops.CodeUnauthorized)
	_, err = svc.ListPluginRepositoryMigrations(staff, &gen.ListPluginRepositoryMigrationsPayload{})
	require.NoError(t, err)
	entries := []*gen.AdminPluginRepositoryMigrationEntry{{ProjectID: org.projectID.String(), RepositoryID: "123", Evidence: "evidence"}}
	preview, err := svc.ApplyPluginRepositoryMigrations(staff, &gen.ApplyPluginRepositoryMigrationsPayload{Entries: entries, DryRun: true})
	require.NoError(t, err)
	require.Equal(t, "unavailable", preview.Items[0].Status)
	_, err = svc.ApplyPluginRepositoryMigrations(staff, &gen.ApplyPluginRepositoryMigrationsPayload{Entries: make([]*gen.AdminPluginRepositoryMigrationEntry, 51), DryRun: true})
	requirePluginRepositoryError(t, err, oops.CodeBadRequest)
	_, err = svc.ApplyPluginRepositoryMigrations(staff, &gen.ApplyPluginRepositoryMigrationsPayload{Entries: entries})
	requirePluginRepositoryError(t, err, oops.CodeForbidden)
	_, err = svc.ReprovisionPluginRepository(staff, &gen.ReprovisionPluginRepositoryPayload{ProjectID: org.projectID.String(), Reason: "unresolved"})
	requirePluginRepositoryError(t, err, oops.CodeForbidden)
}

func TestPluginRepositoryMigrationReprovision(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	staff := withPlatformCredentialStaff(ctx)
	seedPlatformCredentialStaff(t, db, platformCredentialStaffEmail, true)
	org := seedPluginRepositoryMigration(t, ctx, db, "plugin-recovery")
	github := &migrationGitHub{verify: func(repository migrationGitHubRepository) error {
		require.Equal(t, int64(777), repository.repositoryID)
		require.Contains(t, repository.name, org.projectID.String())
		return nil
	}}
	svc.SetPluginRepositoryGitHub(&plugins.GitHubConfig{Client: github, Org: "Publisher", InstallationID: testPluginRepositoryInstallation})
	payload := &gen.ReprovisionPluginRepositoryPayload{ProjectID: org.projectID.String(), Reason: "historical identity unavailable"}

	// Simulate another staff migration holding the per-project lock and committing a
	// binding while reprovision waits. Recovery must recheck before creation.
	locked := seedPluginRepositoryMigration(t, ctx, db, "plugin-recovery-locked")
	tx := testenv.BeginTx(t, ctx, db)
	require.NoError(t, pluginsrepo.New(tx).LockPluginRepositoryBinding(ctx, locked.projectID))
	pending := make(chan error, 1)
	go func() {
		_, err := svc.ReprovisionPluginRepository(staff, &gen.ReprovisionPluginRepositoryPayload{ProjectID: locked.projectID.String(), Reason: payload.Reason})
		pending <- err
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, db, testenv.BackendPID(tx), "%pg_advisory_xact_lock%")
	require.NoError(t, pluginsrepo.New(tx).InsertPluginRepositoryBinding(ctx, pluginsrepo.InsertPluginRepositoryBindingParams{
		ProjectID: locked.projectID, RepositoryID: 778, InstallationID: testPluginRepositoryInstallation,
		RepoOwner: "publisher", RepoName: "plugin-recovery-locked",
	}))
	require.NoError(t, tx.Commit(ctx))
	requirePluginRepositoryError(t, <-pending, oops.CodeConflict)
	require.Empty(t, github.created)

	result, err := svc.ReprovisionPluginRepository(staff, payload)
	require.NoError(t, err)
	require.Equal(t, "publish_needed", result.Status)
	require.Len(t, github.created, 1)
	require.Equal(t, github.created, github.verified, "the replacement is the only repository contacted")
	created := github.created[0]
	require.True(t, strings.HasPrefix(created.name, "plugins-"+org.projectID.String()+"-recovery-"))
	binding, err := pluginsrepo.New(db).GetPluginRepositoryBinding(ctx, org.projectID)
	require.NoError(t, err)
	require.Equal(t, created.repositoryID, binding.RepositoryID)
	require.Equal(t, created.name, binding.RepoName)
	var owner, name, tokenHash, tokenEncrypted, tokenOrgEncrypted, hooksSnapshot string
	var cleared bool
	err = db.QueryRow(ctx, //nolint:glint // notestingrawsql: inspect both stored encrypted variants and verify the recovery leaves them unchanged.
		`SELECT repo_owner, repo_name, marketplace_token_hash, marketplace_token_encrypted, marketplace_token_org_encrypted,
		published_hooks_config::text, published_fingerprint IS NULL AND published_mcp_fingerprints IS NULL AND published_hooks_version IS NULL
		FROM plugin_github_connections WHERE project_id = $1`, org.projectID).Scan(&owner, &name, &tokenHash, &tokenEncrypted, &tokenOrgEncrypted, &hooksSnapshot, &cleared)
	require.NoError(t, err)
	require.Equal(t, created.owner, owner)
	require.Equal(t, created.name, name)
	require.Equal(t, "plugin-recovery", tokenHash)
	require.Equal(t, "sealed-token", tokenEncrypted)
	require.Equal(t, "org-sealed-token", tokenOrgEncrypted)
	require.True(t, cleared)
	require.JSONEq(t, `{"published_marketplace_name":"stable-name"}`, hooksSnapshot)
	_, err = svc.ReprovisionPluginRepository(staff, payload)
	requirePluginRepositoryError(t, err, oops.CodeConflict)
	require.Len(t, github.created, 1)

	var count int
	err = db.QueryRow(ctx, //nolint:glint // notestingrawsql: assert internal audit events and their metadata, which current list queries redact.
		`SELECT count(*) FROM audit_logs WHERE project_id = $1 AND action = $2 AND is_internal AND metadata ->> 'evidence' = $3`, org.projectID, string(audit.ActionPluginRepositoryReprovision), payload.Reason).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestPluginRepositoryMigrationIDs(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "0", "-1", "+1", "012.0", " 1", "9223372036854775808"} {
		_, err := parsePluginRepositoryID(value)
		require.Error(t, err, value)
	}
	id, err := parsePluginRepositoryID("9223372036854775807")
	require.NoError(t, err)
	require.Equal(t, int64(9223372036854775807), id)
	for _, value := range []string{"", uuid.Nil.String(), "urn:uuid:" + uuid.NewString(), strings.ReplaceAll(uuid.NewString(), "-", "")} {
		_, err := parsePluginRepositoryProjectID(value)
		require.Error(t, err, value)
	}
}
