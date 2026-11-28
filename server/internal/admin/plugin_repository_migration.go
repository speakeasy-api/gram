package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	pluginRepositoryMigrationPageSize  = 100
	pluginRepositoryMigrationBatchSize = 50
	pluginRepositoryEvidenceMax        = 1024
)

func (s *Service) SetPluginRepositoryGitHub(config *plugins.GitHubConfig) {
	s.pluginRepositoryGitHub = config
}

// Listing and dry runs require a verified staff session. Both mutation routes
// additionally require a current platform administrator, independent of the
// admin session's cached identity.
func (s *Service) authorizePluginRepositoryMigration(ctx context.Context, write bool) error {
	auth, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || auth == nil || auth.SessionID == "" || auth.OIDCSubject == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	if !write {
		return nil
	}
	if strings.TrimSpace(auth.Email) == "" {
		return oops.C(oops.CodeForbidden)
	}
	admin, err := usersrepo.New(s.db).IsPlatformAdminEmail(ctx, auth.Email)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "check platform admin permission").LogError(ctx, s.logger)
	}
	if !admin {
		return oops.E(oops.CodeForbidden, nil, "platform admin required")
	}
	return nil
}

func (s *Service) ListPluginRepositoryMigrations(ctx context.Context, payload *gen.ListPluginRepositoryMigrationsPayload) (*gen.AdminPluginRepositoryMigrationList, error) {
	if err := s.authorizePluginRepositoryMigration(ctx, false); err != nil {
		return nil, err
	}
	var after uuid.UUID
	if payload.After != nil {
		var err error
		after, err = parsePluginRepositoryProjectID(*payload.After)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid project cursor")
		}
	}
	rows, err := repo.New(s.db).AdminListPluginRepositoryMigrations(ctx, repo.AdminListPluginRepositoryMigrationsParams{
		AfterProjectID: after,
		PageSize:       pluginRepositoryMigrationPageSize + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin repository migrations").LogError(ctx, s.logger)
	}
	result := &gen.AdminPluginRepositoryMigrationList{Items: make([]*gen.AdminPluginRepositoryMigration, 0, min(len(rows), pluginRepositoryMigrationPageSize))}
	if len(rows) > pluginRepositoryMigrationPageSize {
		rows = rows[:pluginRepositoryMigrationPageSize]
		cursor := rows[len(rows)-1].ProjectID.String()
		result.NextCursor = &cursor
	}
	for _, row := range rows {
		result.Items = append(result.Items, &gen.AdminPluginRepositoryMigration{
			ProjectID: row.ProjectID.String(), OrganizationID: row.OrganizationID,
			RepoOwner: row.RepoOwner, RepoName: row.RepoName, Bound: row.Bound,
		})
	}
	return result, nil
}

func (s *Service) ApplyPluginRepositoryMigrations(ctx context.Context, payload *gen.ApplyPluginRepositoryMigrationsPayload) (*gen.AdminPluginRepositoryMigrationApplyResult, error) {
	if err := s.authorizePluginRepositoryMigration(ctx, !payload.DryRun); err != nil {
		return nil, err
	}
	if len(payload.Entries) == 0 || len(payload.Entries) > pluginRepositoryMigrationBatchSize {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide between 1 and 50 repository attestations")
	}
	result := &gen.AdminPluginRepositoryMigrationApplyResult{Items: make([]*gen.AdminPluginRepositoryMigrationOutcome, 0, len(payload.Entries))}
	for _, entry := range payload.Entries {
		if entry == nil {
			result.Items = append(result.Items, &gen.AdminPluginRepositoryMigrationOutcome{Status: "conflict", Message: "An attestation is required."})
			continue
		}
		// Each row has its own timeout and transaction, so a failed row cannot
		// roll back successful rows or make the operator guess which committed.
		rowCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		status, message := s.applyPluginRepositoryMigration(rowCtx, entry, payload.DryRun)
		cancel()
		result.Items = append(result.Items, &gen.AdminPluginRepositoryMigrationOutcome{ProjectID: entry.ProjectID, Status: status, Message: message})
	}
	return result, nil
}

func parsePluginRepositoryProjectID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != strings.ToLower(value) {
		return uuid.Nil, fmt.Errorf("expected a canonical project UUID")
	}
	return id, nil
}

func parsePluginRepositoryID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strings.TrimLeft(value, "0123456789") != "" {
		return 0, fmt.Errorf("expected a positive decimal GitHub repository ID")
	}
	return id, nil
}

func (s *Service) applyPluginRepositoryMigration(ctx context.Context, entry *gen.AdminPluginRepositoryMigrationEntry, dryRun bool) (string, string) {
	projectID, err := parsePluginRepositoryProjectID(entry.ProjectID)
	if err != nil {
		return "conflict", "A canonical project UUID is required."
	}
	repositoryID, err := parsePluginRepositoryID(entry.RepositoryID)
	if err != nil {
		return "conflict", "A positive decimal GitHub repository ID is required."
	}
	evidence := strings.TrimSpace(entry.Evidence)
	if evidence == "" || len(evidence) > pluginRepositoryEvidenceMax {
		return "conflict", "A reference to independent historical evidence is required (at most 1024 bytes)."
	}
	config := s.pluginRepositoryGitHub

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	defer rollbackPluginRepositoryMigration(ctx, tx)
	q, bindings := repo.New(tx), pluginsrepo.New(tx)
	if err := q.AdminLockPluginRepositoryMigration(ctx, projectID); err != nil {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	existing, err := bindings.GetPluginRepositoryBinding(ctx, projectID)
	if err == nil {
		if existing.RepositoryID == repositoryID {
			return "already_bound", "This project is already bound to the specified repository ID."
		}
		return "conflict", "This project already has a different repository binding."
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	connection, err := q.AdminGetPluginRepositoryMigration(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "conflict", "The project has no existing plugin repository to migrate."
	}
	if err != nil {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	if config == nil {
		return "unavailable", "Plugin GitHub publishing is not configured."
	}
	if connection.InstallationID != config.InstallationID || !strings.EqualFold(connection.RepoOwner, config.Org) {
		return "conflict", "The stored destination does not match the configured GitHub installation and owner."
	}
	if _, err := q.AdminFindPluginRepositoryID(ctx, repositoryID); err == nil {
		return "conflict", "This repository ID is already bound to another project."
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	// The input is the only source of the ID. Using an ID-scoped token ensures
	// a name that was reused for a different repository cannot be verified.
	owner := strings.ToLower(config.Org)
	name := strings.ToLower(connection.RepoName)
	if _, err := config.Client.ForRepository(ctx, config.InstallationID, repositoryID, owner, name); err != nil {
		if errors.Is(err, ghclient.ErrRepositoryConflict) {
			return "conflict", "GitHub did not verify the repository identity or privacy."
		}
		return "unavailable", "GitHub could not verify this repository with the specified ID."
	}
	if dryRun {
		return "verified", "GitHub verified this repository; no binding was written."
	}
	binding := pluginsrepo.InsertPluginRepositoryBindingParams{
		ProjectID: projectID, RepositoryID: repositoryID, RepoOwner: owner,
		RepoName: name, InstallationID: config.InstallationID,
	}
	if err := bindings.InsertPluginRepositoryBinding(ctx, binding); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return "conflict", "This project or repository ID was bound by another operation."
		}
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	if err := logPluginRepositoryMigration(ctx, tx, connection.OrganizationID, binding, evidence, false); err != nil {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return s.pluginRepositoryMigrationUnavailable(ctx, err)
	}
	return "bound", "Repository binding recorded."
}

func (s *Service) pluginRepositoryMigrationUnavailable(ctx context.Context, err error) (string, string) {
	s.logger.ErrorContext(ctx, "plugin repository migration database operation failed", "error", err)
	return "unavailable", "The database operation did not confirm a result. Check the current binding before retrying."
}

func rollbackPluginRepositoryMigration(ctx context.Context, tx pgx.Tx) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(rollbackCtx)
}

func logPluginRepositoryMigration(ctx context.Context, tx pgx.Tx, organizationID string, binding pluginsrepo.InsertPluginRepositoryBindingParams, evidence string, recovery bool) error {
	actor, displayName, _ := adminActor(ctx)
	return audit.NewLogger().LogPluginRepositoryBinding(ctx, tx, audit.LogPluginRepositoryBindingEvent{
		OrganizationID: organizationID, ProjectID: binding.ProjectID, Actor: actor, ActorDisplayName: displayName,
		RepositoryID: strconv.FormatInt(binding.RepositoryID, 10), InstallationID: strconv.FormatInt(binding.InstallationID, 10),
		RepositoryOwner: binding.RepoOwner, RepositoryName: binding.RepoName, Evidence: evidence, Reprovisioned: recovery,
	})
}

func (s *Service) ReprovisionPluginRepository(ctx context.Context, payload *gen.ReprovisionPluginRepositoryPayload) (*gen.AdminPluginRepositoryReprovisionResult, error) {
	if err := s.authorizePluginRepositoryMigration(ctx, true); err != nil {
		return nil, err
	}
	projectID, err := parsePluginRepositoryProjectID(payload.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid project UUID")
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" || len(reason) > pluginRepositoryEvidenceMax {
		return nil, oops.E(oops.CodeBadRequest, nil, "provide a recovery reason of at most 1024 bytes")
	}
	config := s.pluginRepositoryGitHub
	if config == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "plugin GitHub publishing is not configured")
	}

	// Finish recording an externally created repository even if the request
	// disconnects. This bound also limits lock time if GitHub stalls.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	defer rollbackPluginRepositoryMigration(ctx, tx)
	q, bindings := repo.New(tx), pluginsrepo.New(tx)
	if err := q.AdminLockPluginRepositoryMigration(ctx, projectID); err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	if _, err := bindings.GetPluginRepositoryBinding(ctx, projectID); err == nil {
		return nil, oops.E(oops.CodeConflict, nil, "this project already has a repository binding")
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	connection, err := q.AdminGetPluginRepositoryMigration(ctx, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, nil, "no existing plugin repository to reprovision")
	}
	if err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}

	owner := strings.ToLower(config.Org)
	name := "plugins-" + projectID.String() + "-recovery-" + uuid.NewString()[:8]
	// Do not query the old repository. A name collision (including after a
	// GitHub-side timeout) never authorizes reuse. A retry creates a new name.
	newID, err := config.Client.CreatePrivateRepository(ctx, config.InstallationID, owner, name)
	if err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	if _, err := config.Client.ForRepository(ctx, config.InstallationID, newID, owner, name); err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	binding := pluginsrepo.InsertPluginRepositoryBindingParams{
		ProjectID: projectID, RepositoryID: newID, RepoOwner: owner,
		RepoName: name, InstallationID: config.InstallationID,
	}
	if err := bindings.InsertPluginRepositoryBinding(ctx, binding); err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	count, err := q.AdminReprovisionPluginRepository(ctx, repo.AdminReprovisionPluginRepositoryParams{
		ProjectID: projectID, RepoOwner: owner, RepoName: name, InstallationID: config.InstallationID,
	})
	if err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	if count != 1 {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, fmt.Errorf("unexpected number of plugin repository connections updated: %d", count))
	}
	if err := logPluginRepositoryMigration(ctx, tx, connection.OrganizationID, binding, reason, true); err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, s.pluginRepositoryRecoveryFailure(ctx, err)
	}
	return &gen.AdminPluginRepositoryReprovisionResult{
		Status:  "publish_needed",
		Message: "A new private repository is bound. The marketplace install URL is unchanged. Publish plugins for this project to populate the new repository.",
	}, nil
}

func (s *Service) pluginRepositoryRecoveryFailure(ctx context.Context, err error) error {
	return oops.E(oops.CodeUnavailable, err, "could not confirm recovery; check the current binding before retrying").LogError(ctx, s.logger)
}
