package plugins

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins/installmode"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// MaxPluginSlugLength is the longest slug the plugins table accepts; it mirrors
// the CHAR_LENGTH(slug) <= 60 check on plugins.slug.
const MaxPluginSlugLength = 60

var (
	// ErrPluginSlugInvalid is a supplied slug that does not survive slug
	// normalization unchanged.
	ErrPluginSlugInvalid = errors.New("invalid slug: must be non-empty and contain only lowercase alphanumeric characters and hyphens")

	// ErrPluginSlugTooLong is a supplied slug longer than the plugins table
	// accepts.
	ErrPluginSlugTooLong = fmt.Errorf("invalid slug: must be at most %d characters", MaxPluginSlugLength)

	// ErrPluginNameWithoutSlug is a plugin name that normalizes to an empty
	// slug when no slug was supplied.
	ErrPluginNameWithoutSlug = errors.New("plugin name must produce a valid slug")

	// ErrPluginNameEmpty is an empty plugin name, which the plugins table
	// rejects.
	ErrPluginNameEmpty = errors.New("plugin name must not be empty")

	// ErrPluginSlugConflict is a slug another plugin in the project already
	// holds.
	ErrPluginSlugConflict = errors.New("a plugin with this slug already exists")

	// ErrPluginMetadataTargetNotFound is a plugin id that matches no plugin in
	// the project.
	ErrPluginMetadataTargetNotFound = errors.New("plugin not found")
)

// PluginMetadataCore creates and updates a plugin's own metadata inside a
// caller-owned transaction: the project lock, the row write, the default
// project's everyone assignment, the audit entry, and the publication request
// commit together with whatever else the caller writes. The plugins management
// API and the Platform MCP share it so both surfaces accept and refuse the same
// inputs the same way. Callers authorize before calling it and signal a
// publish only after their transaction commits.
type PluginMetadataCore struct {
	audit       *audit.Logger
	publication PublicationRequests
}

func NewPluginMetadataCore(auditLogger *audit.Logger, publication PublicationRequests) *PluginMetadataCore {
	return &PluginMetadataCore{audit: auditLogger, publication: publication}
}

// PluginMetadataActor is the user a metadata change is attributed to.
type PluginMetadataActor struct {
	// UserID identifies the acting user on the audit entry and the
	// publication request.
	UserID string

	// DisplayName is shown beside the actor in the audit log when known.
	DisplayName *string
}

// CreatePluginMutation names one plugin to create in a project.
type CreatePluginMutation struct {
	// OrganizationID owns the project.
	OrganizationID string

	// ProjectID is the project the plugin is created in.
	ProjectID uuid.UUID

	// Name is the administrator-facing plugin name.
	Name string

	// Slug is the requested slug. Nil or empty derives one from Name.
	Slug *string

	// Description is the optional plugin description.
	Description *string

	// Actor is who the creation is attributed to.
	Actor PluginMetadataActor
}

// UpdatePluginMutation names one plugin's new metadata.
type UpdatePluginMutation struct {
	// OrganizationID owns the project.
	OrganizationID string

	// ProjectID is the project that owns the plugin.
	ProjectID uuid.UUID

	// PluginID is the plugin to update.
	PluginID uuid.UUID

	// Name is the new administrator-facing plugin name.
	Name string

	// Slug is the new slug. Nil keeps the current slug, which is what a rename
	// wants: the slug names the published package, so changing it moves the
	// plugin to a different install name.
	Slug *string

	// Description is the new description. Nil clears it unless
	// KeepDescription is set.
	Description *string

	// KeepDescription leaves the current description in place and ignores
	// Description.
	KeepDescription bool

	// Actor is who the update is attributed to.
	Actor PluginMetadataActor
}

// PluginMetadataResult is the committed-to-be state of a metadata write.
type PluginMetadataResult struct {
	// Plugin is the row as written.
	Plugin repo.Plugin

	// Before is the row before an update. It is nil for a create.
	Before *repo.Plugin

	// DeliveredToEveryone reports that a created plugin was assigned to every
	// organization member because it lives in the organization's default
	// project.
	DeliveredToEveryone bool

	// Publication is what requesting a package refresh wrote in the caller's
	// transaction.
	Publication ProjectPublicationRequestOutcome
}

// ResolveCreatePluginSlug returns the slug a new plugin gets: the requested
// one when it is already a normalized slug of at most MaxPluginSlugLength
// characters, otherwise one derived from name. A derived slug is cut to
// MaxPluginSlugLength rather than refused, because a long display name is
// legitimate and the caller did not choose the slug; a supplied slug that is
// too long is refused, because silently changing a slug the caller chose would
// publish the plugin under an install name they never saw.
func ResolveCreatePluginSlug(name string, requested *string) (string, error) {
	if name == "" {
		return "", ErrPluginNameEmpty
	}
	if requested != nil && *requested != "" {
		if err := validateSuppliedPluginSlug(*requested); err != nil {
			return "", err
		}
		return *requested, nil
	}
	slug := conv.ToSlug(name)
	if len(slug) > MaxPluginSlugLength {
		// ToSlug yields ASCII only, so cutting by byte cannot split a rune.
		slug = strings.TrimRight(slug[:MaxPluginSlugLength], "-")
	}
	if slug == "" {
		return "", ErrPluginNameWithoutSlug
	}
	return slug, nil
}

func validateSuppliedPluginSlug(requested string) error {
	slug := conv.ToSlug(requested)
	if slug == "" || slug != requested {
		return ErrPluginSlugInvalid
	}
	if len(slug) > MaxPluginSlugLength {
		return ErrPluginSlugTooLong
	}
	return nil
}

// CreateInTransaction creates an empty plugin. In the organization's default
// project the plugin is assigned to every member, because that project is the
// organization-wide baseline; elsewhere it reaches no one until assigned.
func (c *PluginMetadataCore) CreateInTransaction(ctx context.Context, tx pgx.Tx, mutation CreatePluginMutation) (PluginMetadataResult, error) {
	slug, err := ResolveCreatePluginSlug(mutation.Name, mutation.Slug)
	if err != nil {
		return PluginMetadataResult{}, err
	}

	// Serialize name changes with role setup selection before taking plugin row locks.
	if err := admission.LockProject(ctx, tx, mutation.ProjectID); err != nil {
		return PluginMetadataResult{}, fmt.Errorf("lock plugin selection: %w", err)
	}

	queries := repo.New(tx)
	plugin, err := queries.CreatePlugin(ctx, repo.CreatePluginParams{
		OrganizationID: mutation.OrganizationID,
		ProjectID:      mutation.ProjectID,
		Name:           mutation.Name,
		Slug:           slug,
		Description:    conv.PtrToPGText(mutation.Description),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return PluginMetadataResult{}, ErrPluginSlugConflict
		}
		return PluginMetadataResult{}, fmt.Errorf("create plugin: %w", err)
	}

	isDefaultProject, err := queries.IsDefaultProject(ctx, repo.IsDefaultProjectParams{
		OrganizationID: mutation.OrganizationID,
		ProjectID:      mutation.ProjectID,
	})
	if err != nil {
		return PluginMetadataResult{}, fmt.Errorf("check default project: %w", err)
	}
	if isDefaultProject {
		// agent.getPlugins scopes delivery by assignment; "*" (all org members)
		// is the closest "everyone" primitive Speakeasy has, since there's no
		// project-scoped membership.
		if _, err := queries.AddPluginAssignment(ctx, repo.AddPluginAssignmentParams{
			PluginID:       plugin.ID,
			OrganizationID: mutation.OrganizationID,
			PrincipalUrn:   urn.PrincipalWildcard,
			InstallMode:    string(installmode.Default),
		}); err != nil {
			return PluginMetadataResult{}, fmt.Errorf("assign new plugin to org: %w", err)
		}
	}

	if err := c.audit.LogPluginCreate(ctx, tx, audit.LogPluginCreateEvent{
		OrganizationID:   mutation.OrganizationID,
		ProjectID:        mutation.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, mutation.Actor.UserID),
		ActorDisplayName: mutation.Actor.DisplayName,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
	}); err != nil {
		return PluginMetadataResult{}, fmt.Errorf("audit log plugin create: %w", err)
	}

	outcome, err := c.publication.ProjectWithOutcome(ctx, tx, mutation.OrganizationID, mutation.ProjectID, mutation.Actor.UserID)
	if err != nil {
		return PluginMetadataResult{}, fmt.Errorf("enqueue plugin publication: %w", err)
	}
	return PluginMetadataResult{Plugin: plugin, Before: nil, DeliveredToEveryone: isDefaultProject, Publication: outcome}, nil
}

// UpdateInTransaction rewrites a plugin's name, slug, and description. It never
// touches the plugin's members, assignments, or published packages directly;
// the publication request it writes regenerates packages from the new
// metadata.
func (c *PluginMetadataCore) UpdateInTransaction(ctx context.Context, tx pgx.Tx, mutation UpdatePluginMutation) (PluginMetadataResult, error) {
	if mutation.Name == "" {
		return PluginMetadataResult{}, ErrPluginNameEmpty
	}
	if mutation.Slug != nil {
		if err := validateSuppliedPluginSlug(*mutation.Slug); err != nil {
			return PluginMetadataResult{}, err
		}
	}

	// Serialize name changes with role setup selection before taking plugin row locks.
	if err := admission.LockProject(ctx, tx, mutation.ProjectID); err != nil {
		return PluginMetadataResult{}, fmt.Errorf("lock plugin selection: %w", err)
	}

	queries := repo.New(tx)
	before, err := queries.GetPlugin(ctx, repo.GetPluginParams{
		ID:             mutation.PluginID,
		OrganizationID: mutation.OrganizationID,
		ProjectID:      mutation.ProjectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginMetadataResult{}, ErrPluginMetadataTargetNotFound
	}
	if err != nil {
		return PluginMetadataResult{}, fmt.Errorf("load plugin: %w", err)
	}

	slug := before.Slug
	if mutation.Slug != nil {
		slug = *mutation.Slug
	}
	description := conv.PtrToPGText(mutation.Description)
	if mutation.KeepDescription {
		description = before.Description
	}
	plugin, err := queries.UpdatePlugin(ctx, repo.UpdatePluginParams{
		ID:             mutation.PluginID,
		OrganizationID: mutation.OrganizationID,
		ProjectID:      mutation.ProjectID,
		Name:           mutation.Name,
		Slug:           slug,
		Description:    description,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PluginMetadataResult{}, ErrPluginMetadataTargetNotFound
	}
	if err != nil {
		if isUniqueViolation(err) {
			return PluginMetadataResult{}, ErrPluginSlugConflict
		}
		return PluginMetadataResult{}, fmt.Errorf("update plugin: %w", err)
	}

	if err := c.audit.LogPluginUpdate(ctx, tx, audit.LogPluginUpdateEvent{
		OrganizationID:   mutation.OrganizationID,
		ProjectID:        mutation.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, mutation.Actor.UserID),
		ActorDisplayName: mutation.Actor.DisplayName,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
		SnapshotBefore: &audit.PluginSnapshot{
			Name:        before.Name,
			Slug:        before.Slug,
			Description: conv.FromPGText[string](before.Description),
		},
		SnapshotAfter: &audit.PluginSnapshot{
			Name:        plugin.Name,
			Slug:        plugin.Slug,
			Description: conv.FromPGText[string](plugin.Description),
		},
	}); err != nil {
		return PluginMetadataResult{}, fmt.Errorf("audit log plugin update: %w", err)
	}

	outcome, err := c.publication.ProjectWithOutcome(ctx, tx, mutation.OrganizationID, mutation.ProjectID, mutation.Actor.UserID)
	if err != nil {
		return PluginMetadataResult{}, fmt.Errorf("enqueue plugin publication: %w", err)
	}
	return PluginMetadataResult{Plugin: plugin, Before: &before, DeliveredToEveryone: false, Publication: outcome}, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
