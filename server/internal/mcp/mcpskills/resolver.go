package mcpskills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/skills"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
	// read. The skill reached the agent through a plugin distribution, which
	// is the level the hook reconciler already treats as plugin-delivered.
	observationSourceLevel = "plugin"

	// idempotencyKeyPrefix namespaces MCP activation keys from the keys hook
	// adapters choose, which share the per-project unique index.
	idempotencyKeyPrefix = "mcp:"
)

// ErrSkillNotFound reports that no served skill matches the requested URI for
// this caller, whether the URI is malformed, the skill does not exist, or the
// caller is not assigned a plugin that carries it. SEP-2640 answers all three
// with the same invalid-params error, so callers need not tell them apart.
var ErrSkillNotFound = errors.New("no skill is served at this URI")

// Scope identifies the endpoint and caller a resolution serves.
type Scope struct {
	// ProjectID is the project that owns the endpoint and the skills.
	ProjectID uuid.UUID

	// ToolsetID is the toolset the endpoint serves.
	ToolsetID uuid.UUID

	// MCPServerID is the mcp_servers row fronting the toolset, when the
	// endpoint has one. Plugins may contain that server instead of the toolset.
	MCPServerID *uuid.UUID

	// PrincipalURNs are the caller's delivery principals. An empty list is an
	// anonymous caller and resolves to no skills.
	PrincipalURNs []string
}

// Read describes one resources/read of a served skill to record as an
// activation.
type Read struct {
	// ProjectID is the project the endpoint belongs to.
	ProjectID uuid.UUID

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
}

// NewResolver builds a resolver over the given database handle.
func NewResolver(logger *slog.Logger, db database.DBTX) *Resolver {
	return &Resolver{logger: logger, db: db}
}

// List returns every skill the endpoint serves to the caller, one entry per
// skill name, sorted by name. An anonymous scope yields no entries.
func (r *Resolver) List(ctx context.Context, scope Scope) ([]Entry, error) {
	return r.resolve(ctx, scope, pgtype.Text{String: "", Valid: false})
}

// Get returns the served skill at uri for the caller, or [ErrSkillNotFound].
func (r *Resolver) Get(ctx context.Context, scope Scope, uri string) (*Entry, error) {
	name, ok := ParseSkillURI(uri)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, uri)
	}

	entries, err := r.resolve(ctx, scope, conv.ToPGText(name))
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrSkillNotFound, uri)
	}
	return &entries[0], nil
}

func (r *Resolver) resolve(ctx context.Context, scope Scope, name pgtype.Text) ([]Entry, error) {
	if len(scope.PrincipalURNs) == 0 {
		return nil, nil
	}

	mcpServerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if scope.MCPServerID != nil {
		mcpServerID = conv.ToNullUUID(*scope.MCPServerID)
	}

	rows, err := repo.New(r.db).ListServedSkillsForEndpoint(ctx, repo.ListServedSkillsForEndpointParams{
		ProjectID:     scope.ProjectID,
		SkillName:     name,
		ToolsetID:     scope.ToolsetID,
		McpServerID:   mcpServerID,
		PrincipalUrns: scope.PrincipalURNs,
	})
	if err != nil {
		return nil, fmt.Errorf("list served skills: %w", err)
	}

	entries := make([]Entry, 0, len(rows))
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].SkillName == rows[start].SkillName {
			end++
		}

		candidates := make([]candidate, 0, end-start)
		for _, row := range rows[start:end] {
			candidates = append(candidates, candidate{
				entry: Entry{
					SkillID:     row.SkillID,
					VersionID:   row.VersionID,
					PluginID:    row.PluginID,
					Name:        row.SkillName,
					Content:     row.Content,
					RawSHA256:   row.RawSha256,
					Frontmatter: nil,
					Pinned:      row.Pinned,
				},
				effectiveAt: row.EffectiveAt.Time,
			})
		}
		start = end

		winner, conflicts := pickWinner(candidates)
		if len(conflicts) > 0 {
			r.logger.WarnContext(ctx, "served skill pinned to different versions across plugins; serving the newest pin",
				attr.SlogProjectID(scope.ProjectID.String()),
				attr.SlogToolsetID(scope.ToolsetID.String()),
				attr.SlogResourceURI(winner.URI()),
				attr.SlogMcpSkillPinConflictCount(len(conflicts)),
			)
		}

		frontmatter, err := skills.SkillFrontmatter(winner.Content)
		if err != nil {
			// A valid version parsed when it was written, so this is a
			// storage-level surprise; the rest of the listing still serves.
			r.logger.ErrorContext(ctx, "skip served skill with unparseable frontmatter",
				attr.SlogProjectID(scope.ProjectID.String()),
				attr.SlogResourceURI(winner.URI()),
				attr.SlogError(err),
			)
			continue
		}
		winner.Frontmatter = frontmatter
		entries = append(entries, winner)
	}
	return entries, nil
}

// candidate is one distribution's resolved version of a skill, before the
// resolver picks the version the endpoint serves.
type candidate struct {
	entry       Entry
	effectiveAt time.Time
}

// pickWinner selects the version served among the candidates for one skill
// name: a pinned candidate beats one that tracks the latest version, and among
// pinned candidates the newest version (by effective time, then id) wins. The
// returned conflicts are the version ids of pinned candidates that lost to a
// different pinned version. Callers pass at least one candidate.
func pickWinner(candidates []candidate) (Entry, []uuid.UUID) {
	best := candidates[0]
	for _, other := range candidates[1:] {
		if beats(other, best) {
			best = other
		}
	}

	var conflicts []uuid.UUID
	if best.entry.Pinned {
		for _, other := range candidates {
			if other.entry.Pinned && other.entry.VersionID != best.entry.VersionID {
				conflicts = append(conflicts, other.entry.VersionID)
			}
		}
	}
	return best.entry, conflicts
}

// beats reports whether a should be served instead of b.
func beats(a, b candidate) bool {
	if a.entry.Pinned != b.entry.Pinned {
		return a.entry.Pinned
	}
	if !a.effectiveAt.Equal(b.effectiveAt) {
		return a.effectiveAt.After(b.effectiveAt)
	}
	return a.entry.VersionID.String() > b.entry.VersionID.String()
}

// RecordRead records a resources/read of a served skill as an activation of
// the version served. It reports whether a new activation was written; a
// repeat of the same content in the same session is not.
func (r *Resolver) RecordRead(ctx context.Context, read Read) (bool, error) {
	written, err := repo.New(r.db).InsertMCPSkillObservation(ctx, repo.InsertMCPSkillObservationParams{
		ProjectID:      read.ProjectID,
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

// PrincipalURNsForCaller returns the delivery principals of the authenticated
// caller, which decide the plugins assigned to them. An unknown or anonymous
// caller has no principals and so resolves to no skills.
func PrincipalURNsForCaller(ctx context.Context, db database.DBTX, organizationID, userID string) ([]string, error) {
	if organizationID == "" || userID == "" {
		return nil, nil
	}

	user, err := usersrepo.New(db).GetUser(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load skill recipient: %w", err)
	}

	principals, err := plugins.ResolveDeliveryPrincipals(ctx, db, organizationID, user.Email, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve skill delivery principals: %w", err)
	}
	return principals, nil
}
