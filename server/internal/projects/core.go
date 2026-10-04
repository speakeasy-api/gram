package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/projects/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// projectNameMaxLength mirrors the projects table's CHECK on name and the
// management API's MaxLength(40) on the create payload, so a name the core
// accepts is one the database will store.
const projectNameMaxLength = 40

var (
	// ErrProjectNameInvalid reports a name that is blank, longer than
	// projectNameMaxLength characters, or contains a NUL byte.
	ErrProjectNameInvalid = errors.New("project name is invalid")

	// ErrProjectSlugEmpty reports a name with no letter or digit in it. The
	// slug is derived from the name, so such a name cannot produce a project.
	ErrProjectSlugEmpty = errors.New("project name produces an empty slug")

	// ErrProjectSlugTaken reports that a live project in the organization
	// already has the slug the name derives to.
	ErrProjectSlugTaken = errors.New("project slug already exists")

	// ErrProjectNotFound reports a rename target that is missing, deleted, or
	// owned by another organization.
	ErrProjectNotFound = errors.New("project not found")
)

// ProjectActor is the already-authorized caller a create or rename is
// attributed to. The core performs no authorization of its own: the
// management API and the Platform MCP authorize their callers differently,
// and each must have done so before calling in.
type ProjectActor struct {
	// UserID is the user every audit entry and the post-create publish are
	// attributed to.
	UserID string

	// DisplayName is shown beside the actor in the audit log when known.
	DisplayName *string
}

func (a ProjectActor) principal() urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, a.UserID)
}

// CreateProjectMutation names one project to create.
type CreateProjectMutation struct {
	// OrganizationID is the organization the project is created in.
	OrganizationID string

	// Name is the display name. The slug is always derived from it with
	// conv.ToSlug; no caller may choose a slug.
	Name string

	// Actor is the caller the creation is attributed to.
	Actor ProjectActor
}

// RenameProjectMutation names one project and its new display name. A rename
// changes the name only; the slug stays stable because dashboard routes and
// the Gram-Project header address a project by it.
type RenameProjectMutation struct {
	// OrganizationID must own the project; a project in another organization
	// is reported as not found.
	OrganizationID string

	// ProjectID is the project to rename.
	ProjectID uuid.UUID

	// Name is the new display name. Surrounding whitespace is trimmed.
	Name string

	// Actor is the caller the rename is attributed to.
	Actor ProjectActor
}

// RenamedProject is the project row before and after a rename.
type RenamedProject struct {
	// Before is the row as it was locked, ahead of the update.
	Before repo.Project

	// After is the row the update returned.
	After repo.Project
}

// Core creates and renames projects inside a caller-owned transaction, so the
// projects management API and the Platform MCP produce identical projects:
// the same slug rule, the same Default environment and Default plugin, and the
// same audit entries. A caller that adds its own writes (an idempotency
// receipt, say) commits them atomically with the project.
type Core struct {
	logger               *slog.Logger
	audit                *audit.Logger
	temporalEnv          *tenv.Environment
	pluginsGitHubEnabled bool
}

func NewCore(logger *slog.Logger, auditLogger *audit.Logger, temporalEnv *tenv.Environment, pluginsGitHubEnabled bool) *Core {
	return &Core{
		logger:               logger,
		audit:                auditLogger,
		temporalEnv:          temporalEnv,
		pluginsGitHubEnabled: pluginsGitHubEnabled,
	}
}

// ProjectSlug derives the slug a project with this name would get, refusing a
// name that is invalid or has no letter or digit to derive one from.
func ProjectSlug(name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.ContainsRune(name, '\x00') || utf8.RuneCountInString(name) > projectNameMaxLength {
		return "", ErrProjectNameInvalid
	}
	slug := conv.ToSlug(name)
	if slug == "" {
		return "", ErrProjectSlugEmpty
	}
	return slug, nil
}

// ProjectRenameName trims a proposed display name and refuses one a rename
// cannot store. Unlike creation it needs no slug, because a rename keeps the
// slug the project already has.
func ProjectRenameName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsRune(name, '\x00') || utf8.RuneCountInString(name) > projectNameMaxLength {
		return "", ErrProjectNameInvalid
	}
	return name, nil
}

// CreateInTransaction creates the project with its Default environment and
// Default plugin and records the audit entries for both. A slug collision is
// reported as ErrProjectSlugTaken; the failed insert aborts dbtx, so the
// caller must roll back.
func (c *Core) CreateInTransaction(ctx context.Context, dbtx pgx.Tx, mutation CreateProjectMutation) (repo.Project, error) {
	slug, err := ProjectSlug(mutation.Name)
	if err != nil {
		return repo.Project{}, err
	}

	prj, err := repo.New(dbtx).CreateProject(ctx, repo.CreateProjectParams{
		OrganizationID: mutation.OrganizationID,
		Name:           mutation.Name,
		Slug:           slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return repo.Project{}, fmt.Errorf("%w: %w", ErrProjectSlugTaken, err)
		}
		return repo.Project{}, fmt.Errorf("create project: %w", err)
	}

	if _, err := envrepo.New(dbtx).CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
		OrganizationID: mutation.OrganizationID,
		ProjectID:      prj.ID,
		Name:           "Default",
		Slug:           "default",
		Description:    conv.ToPGText("Default project for organization"),
	}); err != nil {
		return repo.Project{}, fmt.Errorf("create default environment: %w", err)
	}

	// Provision the project's Default plugin through the shared helper so it
	// takes the same create-and-seed path (including the org-wildcard audience
	// default) as the lazy-heal callers — no separate seeding to drift.
	ensured, err := plugins.EnsureDefaultPlugin(ctx, dbtx, mutation.OrganizationID, prj.ID)
	if err != nil {
		return repo.Project{}, fmt.Errorf("create default plugin: %w", err)
	}

	if ensured.Created {
		if err := c.audit.LogPluginCreate(ctx, dbtx, audit.LogPluginCreateEvent{
			OrganizationID:   mutation.OrganizationID,
			ProjectID:        prj.ID,
			Actor:            mutation.Actor.principal(),
			ActorDisplayName: mutation.Actor.DisplayName,
			ActorSlug:        nil,
			PluginID:         ensured.Plugin.ID,
			PluginName:       ensured.Plugin.Name,
			PluginSlug:       ensured.Plugin.Slug,
		}); err != nil {
			return repo.Project{}, fmt.Errorf("log default plugin creation: %w", err)
		}
	}

	if err := c.audit.LogProjectCreate(ctx, dbtx, audit.LogProjectCreateEvent{
		OrganizationID:   mutation.OrganizationID,
		ProjectID:        prj.ID,
		Actor:            mutation.Actor.principal(),
		ActorDisplayName: mutation.Actor.DisplayName,
		ActorSlug:        nil,
		ProjectName:      prj.Name,
		ProjectSlug:      prj.Slug,
	}); err != nil {
		return repo.Project{}, fmt.Errorf("log project creation: %w", err)
	}

	return prj, nil
}

// AfterCreateCommitted runs the best-effort follow-up a committed creation
// needs. The marketplace repo isn't required for the project to exist. No
// GitHub collaborators are added here — there is no customer GitHub username
// yet; that's supplied later via the dashboard publish/marketplace-settings
// flow. The enqueue uses a non-cancelable derived context, so the caller
// returning right after commit can't drop it.
func (c *Core) AfterCreateCommitted(ctx context.Context, projectID uuid.UUID, actor ProjectActor) {
	if c.pluginsGitHubEnabled {
		background.TriggerPluginPublish(ctx, c.temporalEnv, c.logger, projectID, actor.UserID, true)
	}
}

// RenameInTransaction changes a project's display name and records the
// update with before and after snapshots. The slug is never changed.
func (c *Core) RenameInTransaction(ctx context.Context, dbtx pgx.Tx, mutation RenameProjectMutation) (RenamedProject, error) {
	name, err := ProjectRenameName(mutation.Name)
	if err != nil {
		return RenamedProject{}, err
	}

	pr := repo.New(dbtx)
	existing, err := pr.GetProjectByIDForUpdate(ctx, mutation.ProjectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return RenamedProject{}, ErrProjectNotFound
	case err != nil:
		return RenamedProject{}, fmt.Errorf("lock project: %w", err)
	case existing.OrganizationID != mutation.OrganizationID:
		return RenamedProject{}, ErrProjectNotFound
	}

	updated, err := pr.UpdateProject(ctx, repo.UpdateProjectParams{
		ProjectID: mutation.ProjectID,
		Name:      name,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return RenamedProject{}, ErrProjectNotFound
	case err != nil:
		return RenamedProject{}, fmt.Errorf("update project: %w", err)
	}

	if err := c.audit.LogProjectUpdate(ctx, dbtx, audit.LogProjectUpdateEvent{
		OrganizationID:        updated.OrganizationID,
		ProjectID:             updated.ID,
		Actor:                 mutation.Actor.principal(),
		ActorDisplayName:      mutation.Actor.DisplayName,
		ActorSlug:             nil,
		ProjectName:           updated.Name,
		ProjectSlug:           updated.Slug,
		ProjectSnapshotBefore: toProject(existing),
		ProjectSnapshotAfter:  toProject(updated),
	}); err != nil {
		return RenamedProject{}, fmt.Errorf("log project update: %w", err)
	}

	return RenamedProject{Before: existing, After: updated}, nil
}
