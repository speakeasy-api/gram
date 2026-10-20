package mcpskills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	// ObservationProvider is the skill_observations.provider value of an
	// activation recorded from a hosted MCP read. Distribution reporting
	// filters on it to tell MCP loads from hook-reported ones.
	ObservationProvider = "mcp"

	// observationSource is the skill_observations.source of an MCP read. It
	// names the surface the way hook adapters name theirs.
	observationSource = "mcp"

	// observationSourceLevel is the skill_observations.source_level of an MCP
	// read. The skill reached the agent through an MCP link, not a plugin, so
	// it is not the plugin level the built-in evidence query treats as
	// vendor-delivered.
	observationSourceLevel = "mcp"

	// idempotencyKeyPrefix namespaces MCP activation keys from the keys hook
	// adapters choose, which share the per-project unique index.
	idempotencyKeyPrefix = "mcp:"

	// servedSkillLimit is the most skills one resolution serves. A listing is
	// answered in one page, so the cap bounds the work of a single call. The
	// query picks one version per skill before the cap applies, so a skill
	// linked many times takes one slot; an endpoint with more skills than
	// this has its later skills by project slug and name cut off, which the
	// resolver logs.
	servedSkillLimit = 1000
)

// ErrSkillNotFound reports that no served skill matches the requested URI for
// this caller, whether the URI is malformed, the skill does not exist, or it
// is not linked to the surface. SEP-2640 answers all three
// with the same invalid-params error, so callers need not tell them apart.
var ErrSkillNotFound = errors.New("no skill is served at this URI")

// Scope identifies the surface and caller a resolution serves.
type Scope struct {
	// OrganizationID is the organization that owns the surface. Only links
	// in it are served.
	OrganizationID string

	// Target is the surface the caller reached.
	Target Target

	// MemberMCPServerIDs are servers whose links the surface also serves: a
	// gateway passes the members the caller can reach through it. A surface
	// that is itself an MCP server leaves it empty.
	MemberMCPServerIDs []uuid.UUID

	// UserID is the authenticated caller. An empty, unknown, deleted or
	// removed caller resolves to no skills.
	UserID string
}

// Read describes one resources/read of a served skill to record as an
// activation.
type Read struct {
	// Entry is the served skill the caller read.
	Entry Entry

	// UserID is the authenticated caller, or empty when unknown.
	UserID string

	// SessionID is the MCP session the read happened in. It scopes the
	// idempotency key, so one session reading the same content twice counts
	// once; an empty session records every read.
	SessionID string

	// SeenAt is when the read happened.
	SeenAt time.Time
}

// Resolver answers which skills an endpoint serves to a caller and records
// their reads.
type Resolver struct {
	logger *slog.Logger
	db     database.DBTX
	// limit is the most skills one resolution serves; see servedSkillLimit.
	limit int32
}

// NewResolver builds a resolver over the given database handle.
func NewResolver(logger *slog.Logger, db database.DBTX) *Resolver {
	return &Resolver{logger: logger, db: db, limit: servedSkillLimit}
}

// List returns every skill the surface serves to the caller, one entry per
// skill, sorted by project slug and then name. A caller who is not a live member of the
// organization yields no entries.
func (r *Resolver) List(ctx context.Context, scope Scope) ([]Entry, error) {
	return r.resolve(ctx, scope, skillKey{
		projectSlug: pgtype.Text{String: "", Valid: false},
		name:        pgtype.Text{String: "", Valid: false},
	})
}

// skillKey narrows a resolution to one skill by its URI segments; unset
// fields match every skill.
type skillKey struct {
	projectSlug pgtype.Text
	name        pgtype.Text
}

// Get returns the served skill at uri for the caller, or [ErrSkillNotFound].
func (r *Resolver) Get(ctx context.Context, scope Scope, uri string) (*Entry, error) {
	projectSlug, name, ok := ParseSkillURI(uri)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, uri)
	}

	entries, err := r.resolve(ctx, scope, skillKey{projectSlug: conv.ToPGText(projectSlug), name: conv.ToPGText(name)})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, uri)
	}
	return &entries[0], nil
}

func (r *Resolver) resolve(ctx context.Context, scope Scope, key skillKey) ([]Entry, error) {
	if err := scope.Target.Validate(); err != nil {
		return nil, fmt.Errorf("resolve served skills: %w", err)
	}

	recipient, err := r.isRecipient(ctx, scope.OrganizationID, scope.UserID)
	if err != nil || !recipient {
		return nil, err
	}

	mcpServerIDs := slices.Clone(scope.MemberMCPServerIDs)
	metaMCPServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	rowLessKind := pgtype.Text{String: "", Valid: false}
	switch {
	case scope.Target.Kind == TargetMCPServer:
		mcpServerIDs = append(mcpServerIDs, scope.Target.ID)
	case scope.Target.Kind == TargetMetaMCPServer:
		metaMCPServerID = conv.ToNullUUID(scope.Target.ID)
	case scope.Target.RowLess():
		rowLessKind = conv.ToPGText(string(scope.Target.Kind))
	}
	if mcpServerIDs == nil {
		mcpServerIDs = []uuid.UUID{}
	}

	// One row past the cap is the sentinel that tells a listing of exactly
	// the cap's size from one that was cut off.
	rows, err := repo.New(r.db).ListServedSkillsForTarget(ctx, repo.ListServedSkillsForTargetParams{
		RowLimit:          r.limit + 1,
		OrganizationID:    scope.OrganizationID,
		ProjectSlug:       key.projectSlug,
		SkillName:         key.name,
		McpServerIds:      mcpServerIDs,
		MetaMcpServerID:   metaMCPServerID,
		RowLessTargetKind: rowLessKind,
	})
	if err != nil {
		return nil, fmt.Errorf("list served skills: %w", err)
	}
	logTarget := []any{
		attr.SlogOrganizationID(scope.OrganizationID),
		attr.SlogMcpSkillTargetKind(scope.Target.Kind),
		attr.SlogMcpSkillTargetID(scope.Target.ID.String()),
	}
	if len(rows) > int(r.limit) {
		r.logger.WarnContext(ctx, "served skill listing exceeds its cap; later skills by project and name are not served", logTarget...)
		rows = rows[:r.limit]
	}

	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		// The query already kept one version per skill: a pin over latest,
		// and the newest version between pins.
		entry := Entry{
			SkillID:     row.SkillID,
			ProjectID:   row.ProjectID,
			ProjectSlug: row.ProjectSlug,
			VersionID:   row.VersionID,
			Name:        row.SkillName,
			Content:     row.Content,
			RawSHA256:   row.RawSha256,
			Frontmatter: nil,
		}
		if row.PinnedVersions > 1 {
			r.logger.WarnContext(ctx, "served skill pinned to different versions across links; serving the newest pin",
				append(logTarget,
					attr.SlogResourceURI(entry.URI()),
					attr.SlogMcpSkillPinConflictCount(int(row.PinnedVersions)-1),
				)...,
			)
		}

		frontmatter, err := skills.SkillFrontmatter(entry.Content)
		if err != nil {
			// A valid version parsed when it was written, so this is a
			// storage-level surprise; the rest of the listing still serves.
			r.logger.ErrorContext(ctx, "skip served skill with unparseable frontmatter",
				attr.SlogProjectID(entry.ProjectID.String()),
				attr.SlogResourceURI(entry.URI()),
				attr.SlogError(err),
			)
			continue
		}
		entry.Frontmatter = frontmatter
		if err := entry.CheckFrontmatterName(); err != nil {
			// Validation trims the name before checking it, so a version
			// whose author quoted a padded name is stored valid under the
			// trimmed name while its frontmatter still carries the padding.
			// SEP-2640 requires the two to agree, so it is not served.
			r.logger.ErrorContext(ctx, "skip served skill whose frontmatter name differs from its URI",
				attr.SlogProjectID(entry.ProjectID.String()),
				attr.SlogResourceURI(entry.URI()),
				attr.SlogError(err),
			)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// RecordRead records a resources/read of a served skill as an activation of
// the version served. It reports whether a new activation was written; a
// repeat of the same content in the same session is not.
func (r *Resolver) RecordRead(ctx context.Context, read Read) (bool, error) {
	written, err := repo.New(r.db).InsertMCPSkillObservation(ctx, repo.InsertMCPSkillObservationParams{
		ProjectID:      read.Entry.ProjectID,
		IdempotencyKey: conv.ToPGTextEmpty(ObservationIdempotencyKey(read.SessionID, read.Entry.RawSHA256)),
		Provider:       ObservationProvider,
		UserID:         conv.ToPGTextEmpty(read.UserID),
		SessionID:      conv.ToPGTextEmpty(read.SessionID),
		Source:         observationSource,
		SourceLevel:    observationSourceLevel,
		SourcePath:     read.Entry.URI(),
		SeenAt:         conv.ToPGTimestamptz(read.SeenAt),
		SkillID:        read.Entry.SkillID,
		SkillVersionID: read.Entry.VersionID,
	})
	if err != nil {
		return false, fmt.Errorf("record skill read: %w", err)
	}
	return written > 0, nil
}

// ObservationIdempotencyKey returns the key that dedupes reads of one content
// digest within one MCP session, or "" when there is no session to scope by.
func ObservationIdempotencyKey(sessionID, rawSHA256 string) string {
	if sessionID == "" {
		return ""
	}
	return idempotencyKeyPrefix + sessionID + ":" + rawSHA256
}

// isRecipient reports whether userID is a live user holding an active
// membership in the organization that owns the surface, checked against the
// database on every resolution: a credential can outlive the membership it
// was issued under, and a removed member must stop receiving the
// organization's skills.
func (r *Resolver) isRecipient(ctx context.Context, organizationID, userID string) (bool, error) {
	if organizationID == "" || userID == "" {
		return false, nil
	}
	recipient, err := repo.New(r.db).IsServedSkillRecipient(ctx, repo.IsServedSkillRecipientParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	if err != nil {
		return false, fmt.Errorf("check skill recipient: %w", err)
	}
	return recipient, nil
}
