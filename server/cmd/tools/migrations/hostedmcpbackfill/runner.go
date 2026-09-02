// Package hostedmcpbackfill wraps hosted (toolset-backed) MCP servers in
// mcp_servers and mcp_endpoints rows; see HOSTED_MCP_WRAPPERS_MIGRATION.md.
package hostedmcpbackfill

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Fixed UUIDv5 namespace so reruns recompute the same ids.
var idNamespace = uuid.MustParse("3c0f1c8a-6b52-4b37-9a7e-2d9e1f0a5b11")

const uniqueViolation = "23505"

// Outcome classifies what the command did (or would do) for one toolset.
type Outcome string

const (
	OutcomeWouldCreate        Outcome = "would_create"
	OutcomeWouldAdopt         Outcome = "would_adopt_existing_wrapper"
	OutcomeCreated            Outcome = "created"
	OutcomeAdopted            Outcome = "adopted_existing_wrapper"
	OutcomeAlreadyComplete    Outcome = "already_complete"
	OutcomeBlockedCollision   Outcome = "blocked_collision"
	OutcomeBlockedDrift       Outcome = "blocked_drift"
	OutcomeBlockedNoWrapper   Outcome = "blocked_no_wrapper"
	OutcomeMovedDependents    Outcome = "moved_dependents"
	OutcomeWouldMoveDependent Outcome = "would_move_dependents"
	OutcomeSkippedDependents  Outcome = "skipped_dependents"
	OutcomeRetiredGrants      Outcome = "retired_toolset_grants"
	OutcomeWouldRetireGrants  Outcome = "would_retire_toolset_grants"
)

// Phase selects which pass of the backfill a run performs.
type Phase string

const (
	PhaseWrappers     Phase = "wrappers"
	PhaseDependents   Phase = "dependents"
	PhaseRetireGrants Phase = "retire-toolset-grants"
)

// AliasKey names one custom-domain toolset whose platform-host alias survives.
type AliasKey struct {
	Slug           string
	CustomDomainID uuid.UUID
}

// Options controls one run.
type Options struct {
	ProjectID uuid.NullUUID
	// Cursor resumes after this toolset id (exclusive).
	Cursor   uuid.UUID
	Limit    int
	PageSize int
	Aliases  []AliasKey
	// Apply commits writes; otherwise every row reports a would_* outcome.
	Apply bool
	Phase Phase
}

type EndpointReport struct {
	ID             uuid.UUID  `json:"id"`
	Slug           string     `json:"slug"`
	CustomDomainID *uuid.UUID `json:"custom_domain_id,omitempty"`
	Tombstoned     bool       `json:"tombstoned"`
	Created        bool       `json:"created"`
	Moved          bool       `json:"moved,omitempty"`
}

type DependentsReport struct {
	McpMetadata           int64 `json:"mcp_metadata"`
	CollectionAttachments int64 `json:"collection_attachments"`
	PluginServers         int64 `json:"plugin_servers"`
	AssistantToolsets     int64 `json:"assistant_toolsets"`
}

func (d DependentsReport) total() int64 {
	return d.McpMetadata + d.CollectionAttachments + d.PluginServers + d.AssistantToolsets
}

// RowReport carries ids and slugs only, never organization names or emails.
type RowReport struct {
	ToolsetID         uuid.UUID         `json:"toolset_id"`
	McpSlug           string            `json:"mcp_slug"`
	WrapperID         *uuid.UUID        `json:"wrapper_id,omitempty"`
	Outcome           Outcome           `json:"outcome"`
	Reason            string            `json:"reason,omitempty"`
	Endpoints         []EndpointReport  `json:"endpoints,omitempty"`
	ClearedRootDomain []uuid.UUID       `json:"cleared_root_domain_ids,omitempty"`
	GrantsCopied      int64             `json:"grants_copied"`
	GrantsRetired     int64             `json:"grants_retired,omitempty"`
	Dependents        *DependentsReport `json:"dependents,omitempty"`
	DependentsSkipped *DependentsReport `json:"dependents_skipped,omitempty"`
}

type OauthProxyCounts struct {
	Live          int64 `json:"live"`
	Enabled       int64 `json:"enabled"`
	EnabledPublic int64 `json:"enabled_public"`
}

type Report struct {
	Mode       string           `json:"mode"`
	Phase      Phase            `json:"phase"`
	Scanned    int              `json:"scanned"`
	Outcomes   map[Outcome]int  `json:"outcomes"`
	OauthProxy OauthProxyCounts `json:"oauth_proxy_toolsets"`
	LastCursor uuid.UUID        `json:"last_cursor"`
	Rows       []RowReport      `json:"rows"`
}

type Runner struct {
	pool    *pgxpool.Pool
	options Options
}

func NewRunner(pool *pgxpool.Pool, options Options) *Runner {
	if options.PageSize <= 0 {
		options.PageSize = 100
	}
	if options.Phase == "" {
		options.Phase = PhaseWrappers
	}
	return &Runner{pool: pool, options: options}
}

// Run processes every candidate toolset. On error the report holds every row
// processed so far and LastCursor resumes after the last committed row.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	report := Report{
		Mode:       conv.Ternary(r.options.Apply, "apply", "dry-run"),
		Phase:      r.options.Phase,
		Scanned:    0,
		Outcomes:   map[Outcome]int{},
		OauthProxy: OauthProxyCounts{Live: 0, Enabled: 0, EnabledPublic: 0},
		LastCursor: r.options.Cursor,
		Rows:       nil,
	}

	proxy, err := New(r.pool).CountOauthProxyToolsets(ctx)
	if err != nil {
		return report, fmt.Errorf("count oauth proxy toolsets: %w", err)
	}
	report.OauthProxy = OauthProxyCounts(proxy)

	after := r.options.Cursor
	for {
		candidates, err := New(r.pool).ListCandidateToolsets(ctx, ListCandidateToolsetsParams{
			ProjectID: r.options.ProjectID,
			AfterID:   after,
			PageSize:  conv.SafeInt32(r.options.PageSize),
		})
		if err != nil {
			return report, fmt.Errorf("list candidate toolsets: %w", err)
		}
		if len(candidates) == 0 {
			return report, nil
		}
		for _, candidate := range candidates {
			if r.options.Limit > 0 && report.Scanned >= r.options.Limit {
				return report, nil
			}
			row, err := r.processOne(ctx, candidate)
			if err != nil {
				return report, fmt.Errorf("toolset %s: %w", candidate.ID, err)
			}
			report.Scanned++
			report.Outcomes[row.Outcome]++
			report.Rows = append(report.Rows, row)
			report.LastCursor = candidate.ID
			after = candidate.ID
		}
	}
}

func (r *Runner) processOne(ctx context.Context, candidate ListCandidateToolsetsRow) (RowReport, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return RowReport{}, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := New(tx)
	if err := q.LockToolsetBackfill(ctx, candidate.ID.String()); err != nil {
		return RowReport{}, fmt.Errorf("advisory lock: %w", err)
	}
	toolset, err := q.LockToolsetRow(ctx, LockToolsetRowParams(candidate))
	if err != nil {
		return RowReport{}, fmt.Errorf("lock toolset row: %w", err)
	}

	row := RowReport{
		ToolsetID: toolset.ID, McpSlug: toolset.McpSlug.String, WrapperID: nil, Outcome: "", Reason: "", Endpoints: nil,
		ClearedRootDomain: nil, GrantsCopied: 0, GrantsRetired: 0, Dependents: nil, DependentsSkipped: nil,
	}
	if toolset.Deleted || !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
		row.Outcome = OutcomeBlockedDrift
		row.Reason = "toolset no longer a live hosted server"
		return row, nil
	}

	var wrote bool
	switch r.options.Phase {
	case PhaseDependents:
		wrote, err = r.moveDependents(ctx, tx, q, toolset, &row)
	case PhaseRetireGrants:
		wrote, err = r.retireGrants(ctx, tx, q, toolset, &row)
	default:
		wrote, err = r.ensureWrapper(ctx, tx, q, toolset, &row)
	}
	if err != nil {
		return RowReport{}, err
	}
	if !wrote || !r.options.Apply {
		return row, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return RowReport{}, fmt.Errorf("commit: %w", err)
	}
	return row, nil
}

type wantedEndpoint struct {
	id          uuid.UUID
	domain      uuid.NullUUID
	slug        string
	tombstoneAt pgtype.Timestamptz
}

// ensureWrapper creates or adopts the wrapper, reconciles endpoints, and copies
// toolset-keyed grants onto the wrapper. It reports whether anything was written.
func (r *Runner) ensureWrapper(ctx context.Context, tx pgx.Tx, q *Queries, toolset LockToolsetRowRow, row *RowReport) (bool, error) {
	var domain *GetCustomDomainForBackfillRow
	if toolset.CustomDomainID.Valid {
		found, err := q.GetCustomDomainForBackfill(ctx, GetCustomDomainForBackfillParams{
			ID:             toolset.CustomDomainID.UUID,
			OrganizationID: toolset.OrganizationID,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return false, blocked(row, OutcomeBlockedDrift, "custom domain not found in organization")
		case err != nil:
			return false, fmt.Errorf("load custom domain: %w", err)
		}
		domain = &found
	}
	deadDomain := domain != nil && domain.Deleted

	var tombstoneAt pgtype.Timestamptz
	if deadDomain {
		tombstoneAt = domain.DeletedAt
	}
	wants := []wantedEndpoint{{
		id:          uuid.NewSHA1(idNamespace, []byte("mcp_endpoint:primary:"+toolset.ID.String())),
		domain:      toolset.CustomDomainID,
		slug:        toolset.McpSlug.String,
		tombstoneAt: tombstoneAt,
	}}
	aliased := domain != nil && !deadDomain && slices.Contains(r.options.Aliases, AliasKey{Slug: toolset.McpSlug.String, CustomDomainID: domain.ID})
	if aliased {
		wants = append(wants, wantedEndpoint{
			id:          uuid.NewSHA1(idNamespace, []byte("mcp_endpoint:platform-alias:"+toolset.ID.String())),
			domain:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			slug:        toolset.McpSlug.String,
			tombstoneAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		})
	}
	for _, want := range wants {
		if want.tombstoneAt.Valid {
			continue
		}
		if err := mcpendpoints.LockSlugScope(ctx, tx, want.domain, want.slug); err != nil {
			return false, fmt.Errorf("lock slug scope: %w", err)
		}
		if want.domain.Valid {
			continue
		}
		owned, err := q.PlatformSlugOwnedByOtherToolset(ctx, PlatformSlugOwnedByOtherToolsetParams{Slug: toolset.McpSlug, ToolsetID: toolset.ID})
		if err != nil {
			return false, fmt.Errorf("check platform slug owner: %w", err)
		}
		if owned {
			return false, blocked(row, OutcomeBlockedCollision, "platform slug owned by another toolset")
		}
	}

	wrote := false
	wantVisibility := hostedmcp.VisibilityForToolset(toolset.McpEnabled, toolset.McpIsPublic)
	wrapperID := uuid.NewSHA1(idNamespace, []byte("mcp_server:"+toolset.ID.String()))
	existing, err := mcpserversrepo.New(tx).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: toolset.ProjectID,
	})
	adopted := err == nil
	switch {
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return false, fmt.Errorf("load existing wrapper: %w", err)
	case adopted:
		wrapperID = existing.ID
		slug, err := mcpservers.ComputeServerSlug(toolset.Name, wrapperID)
		if err != nil {
			return false, fmt.Errorf("compute wrapper slug: %w", err)
		}
		wantName := conv.ToPGText(toolset.Name)
		if existing.Name != wantName {
			taken, err := q.WrapperSlugTaken(ctx, WrapperSlugTakenParams{ProjectID: toolset.ProjectID, Slug: conv.ToPGText(slug), ID: wrapperID})
			if err != nil {
				return false, fmt.Errorf("check wrapper slug: %w", err)
			}
			if taken {
				return false, blocked(row, OutcomeBlockedCollision, "wrapper slug already used in project")
			}
			row.Reason = "name_drift"
		}
		if existing.Name != wantName || existing.Visibility != wantVisibility || existing.UserSessionIssuerID != toolset.UserSessionIssuerID || existing.ToolVariationsGroupID != toolset.ToolVariationsGroupID {
			if err := q.ReconcileWrapper(ctx, ReconcileWrapperParams{
				Name:                  wantName,
				Slug:                  conv.ToPGText(slug),
				UserSessionIssuerID:   toolset.UserSessionIssuerID,
				ToolVariationsGroupID: toolset.ToolVariationsGroupID,
				Visibility:            wantVisibility,
				ID:                    wrapperID,
				ProjectID:             toolset.ProjectID,
			}); err != nil {
				return false, fmt.Errorf("reconcile wrapper: %w", err)
			}
			wrote = true
		}
		if wantVisibility == visibility.Disabled {
			cleared, err := q.ClearRootEndpoints(ctx, ClearRootEndpointsParams{McpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true}, ProjectID: toolset.ProjectID})
			if err != nil {
				return false, fmt.Errorf("clear root endpoints: %w", err)
			}
			for _, d := range cleared {
				row.ClearedRootDomain = append(row.ClearedRootDomain, d.UUID)
			}
			wrote = wrote || len(cleared) > 0
		}
	default:
		prior, err := q.GetWrapperByID(ctx, GetWrapperByIDParams{ID: wrapperID, ProjectID: toolset.ProjectID})
		if err == nil {
			return false, blocked(row, OutcomeBlockedDrift, conv.Ternary(prior.Deleted, "backfilled wrapper was deleted", "backfill wrapper id in use"))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("load wrapper by id: %w", err)
		}
		slug, err := mcpservers.ComputeServerSlug(toolset.Name, wrapperID)
		if err != nil {
			return false, fmt.Errorf("compute wrapper slug: %w", err)
		}
		taken, err := q.WrapperSlugTaken(ctx, WrapperSlugTakenParams{ProjectID: toolset.ProjectID, Slug: conv.ToPGText(slug), ID: wrapperID})
		if err != nil {
			return false, fmt.Errorf("check wrapper slug: %w", err)
		}
		if taken {
			return false, blocked(row, OutcomeBlockedCollision, "wrapper slug already used in project")
		}
		if err := q.InsertWrapper(ctx, InsertWrapperParams{
			ID:                    wrapperID,
			ProjectID:             toolset.ProjectID,
			Name:                  conv.ToPGText(toolset.Name),
			Slug:                  conv.ToPGText(slug),
			UserSessionIssuerID:   toolset.UserSessionIssuerID,
			ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
			ToolVariationsGroupID: toolset.ToolVariationsGroupID,
			Visibility:            wantVisibility,
		}); err != nil {
			return false, fmt.Errorf("insert wrapper: %w", err)
		}
		wrote = true
	}
	row.WrapperID = &wrapperID

	have, err := q.ListWrapperEndpoints(ctx, ListWrapperEndpointsParams{
		McpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true},
		ProjectID:   toolset.ProjectID,
	})
	if err != nil {
		return false, fmt.Errorf("list wrapper endpoints: %w", err)
	}
	for _, want := range wants {
		wroteEndpoint, err := r.ensureEndpoint(ctx, q, toolset, wrapperID, want, have, row)
		if err != nil {
			return false, err
		}
		if row.Outcome != "" {
			return false, nil
		}
		wrote = wrote || wroteEndpoint
	}

	copied, err := q.CopyMCPGrantsToWrapper(ctx, CopyMCPGrantsToWrapperParams{
		WrapperID:      wrapperID.String(),
		OrganizationID: toolset.OrganizationID,
		ToolsetID:      toolset.ID.String(),
	})
	if err != nil {
		return false, fmt.Errorf("copy grants: %w", err)
	}
	row.GrantsCopied = copied
	wrote = wrote || copied > 0

	switch {
	case !wrote:
		row.Outcome = OutcomeAlreadyComplete
	case adopted:
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeAdopted, OutcomeWouldAdopt)
	default:
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeCreated, OutcomeWouldCreate)
	}
	return wrote, nil
}

// ensureEndpoint matches a wanted endpoint by address, then by deterministic id
// (moving it to the current address), and only then inserts.
func (r *Runner) ensureEndpoint(ctx context.Context, q *Queries, toolset LockToolsetRowRow, wrapperID uuid.UUID, want wantedEndpoint, have []ListWrapperEndpointsRow, row *RowReport) (bool, error) {
	wantLive := !want.tombstoneAt.Valid
	idx := slices.IndexFunc(have, func(e ListWrapperEndpointsRow) bool {
		return e.Slug == want.slug && e.CustomDomainID == want.domain && e.Deleted != wantLive
	})
	if idx >= 0 {
		row.Endpoints = append(row.Endpoints, EndpointReport{
			ID: have[idx].ID, Slug: want.slug, CustomDomainID: nullUUIDPtr(want.domain), Tombstoned: have[idx].Deleted, Created: false, Moved: false,
		})
		return false, nil
	}

	if wantLive {
		taken, err := q.EndpointAddressTaken(ctx, EndpointAddressTakenParams{Slug: want.slug, CustomDomainID: want.domain, McpServerID: wrapperID})
		if err != nil {
			return false, fmt.Errorf("check endpoint address: %w", err)
		}
		if taken {
			return false, blocked(row, OutcomeBlockedCollision, "endpoint address held by another server")
		}
	}

	prior, err := q.GetEndpointByID(ctx, GetEndpointByIDParams{ID: want.id, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		err = q.InsertEndpoint(ctx, InsertEndpointParams{
			ID:             want.id,
			ProjectID:      toolset.ProjectID,
			CustomDomainID: want.domain,
			McpServerID:    uuid.NullUUID{UUID: wrapperID, Valid: true},
			Slug:           want.slug,
			DeletedAt:      want.tombstoneAt,
		})
		if isUniqueViolation(err) {
			return false, blocked(row, OutcomeBlockedCollision, "endpoint address claimed concurrently")
		}
		if err != nil {
			return false, fmt.Errorf("insert endpoint: %w", err)
		}
		row.Endpoints = append(row.Endpoints, EndpointReport{
			ID: want.id, Slug: want.slug, CustomDomainID: nullUUIDPtr(want.domain), Tombstoned: !wantLive, Created: true, Moved: false,
		})
		return true, nil
	case err != nil:
		return false, fmt.Errorf("load endpoint by id: %w", err)
	case prior.McpServerID != (uuid.NullUUID{UUID: wrapperID, Valid: true}):
		return false, blocked(row, OutcomeBlockedDrift, "backfill endpoint id belongs to another server")
	}
	if err := q.MoveEndpointAddress(ctx, MoveEndpointAddressParams{
		CustomDomainID: want.domain, Slug: want.slug, DeletedAt: want.tombstoneAt, ID: want.id, ProjectID: toolset.ProjectID,
	}); err != nil {
		return false, fmt.Errorf("move endpoint address: %w", err)
	}
	row.Endpoints = append(row.Endpoints, EndpointReport{
		ID: want.id, Slug: want.slug, CustomDomainID: nullUUIDPtr(want.domain), Tombstoned: !wantLive, Created: false, Moved: true,
	})
	return true, nil
}

// moveDependents re-keys the toolset's dependents onto its wrapper.
func (r *Runner) moveDependents(ctx context.Context, tx pgx.Tx, q *Queries, toolset LockToolsetRowRow, row *RowReport) (bool, error) {
	wrapper, err := mcpserversrepo.New(tx).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: toolset.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, blocked(row, OutcomeBlockedNoWrapper, "toolset has no live wrapper; run the wrapper phase first")
	case err != nil:
		return false, fmt.Errorf("load wrapper: %w", err)
	}
	row.WrapperID = &wrapper.ID
	serverID := uuid.NullUUID{UUID: wrapper.ID, Valid: true}
	toolsetID := uuid.NullUUID{UUID: toolset.ID, Valid: true}

	var moved DependentsReport
	if moved.McpMetadata, err = q.MoveMcpMetadata(ctx, MoveMcpMetadataParams{McpServerID: serverID, ToolsetID: toolsetID, ProjectID: toolset.ProjectID}); err != nil {
		return false, fmt.Errorf("move mcp metadata: %w", err)
	}
	if moved.CollectionAttachments, err = q.MoveCollectionAttachments(ctx, MoveCollectionAttachmentsParams{McpServerID: serverID, OrganizationID: toolset.OrganizationID, ToolsetID: toolsetID}); err != nil {
		return false, fmt.Errorf("move collection attachments: %w", err)
	}
	if moved.PluginServers, err = q.MovePluginServers(ctx, MovePluginServersParams{McpServerID: serverID, ProjectID: toolset.ProjectID, ToolsetID: toolsetID}); err != nil {
		return false, fmt.Errorf("move plugin servers: %w", err)
	}
	movedIDs, err := q.MoveAssistantToolsets(ctx, MoveAssistantToolsetsParams{McpServerID: wrapper.ID, ToolsetID: toolset.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return false, fmt.Errorf("move assistant toolsets: %w", err)
	}
	if len(movedIDs) > 0 {
		if moved.AssistantToolsets, err = q.DeleteAssistantToolsetsByIDs(ctx, DeleteAssistantToolsetsByIDsParams{Ids: movedIDs, ToolsetID: toolset.ID, ProjectID: toolset.ProjectID}); err != nil {
			return false, fmt.Errorf("delete moved assistant toolsets: %w", err)
		}
	}
	row.Dependents = &moved

	var skipped DependentsReport
	if skipped.McpMetadata, err = q.CountSkippedMcpMetadata(ctx, CountSkippedMcpMetadataParams{ToolsetID: toolsetID, ProjectID: toolset.ProjectID}); err != nil {
		return false, fmt.Errorf("count skipped mcp metadata: %w", err)
	}
	if skipped.CollectionAttachments, err = q.CountSkippedCollectionAttachments(ctx, CountSkippedCollectionAttachmentsParams{OrganizationID: toolset.OrganizationID, ToolsetID: toolsetID}); err != nil {
		return false, fmt.Errorf("count skipped collection attachments: %w", err)
	}
	if skipped.PluginServers, err = q.CountSkippedPluginServers(ctx, CountSkippedPluginServersParams{ProjectID: toolset.ProjectID, ToolsetID: toolsetID}); err != nil {
		return false, fmt.Errorf("count skipped plugin servers: %w", err)
	}
	if skipped.AssistantToolsets, err = q.CountSkippedAssistantToolsets(ctx, CountSkippedAssistantToolsetsParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID}); err != nil {
		return false, fmt.Errorf("count skipped assistant toolsets: %w", err)
	}
	if skipped.total() > 0 {
		row.DependentsSkipped = &skipped
	}

	switch {
	case moved.total() == 0 && skipped.total() > 0:
		row.Outcome = OutcomeSkippedDependents
		row.Reason = "server-keyed twin already exists"
		return false, nil
	case moved.total() == 0:
		row.Outcome = OutcomeAlreadyComplete
		return false, nil
	}
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeMovedDependents, OutcomeWouldMoveDependent)
	return true, nil
}

// retireGrants deletes toolset-keyed grants whose wrapper-keyed twin exists.
func (r *Runner) retireGrants(ctx context.Context, tx pgx.Tx, q *Queries, toolset LockToolsetRowRow, row *RowReport) (bool, error) {
	wrapper, err := mcpserversrepo.New(tx).GetMCPServerByToolsetID(ctx, mcpserversrepo.GetMCPServerByToolsetIDParams{
		ToolsetID: toolset.ID,
		ProjectID: toolset.ProjectID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return false, blocked(row, OutcomeBlockedNoWrapper, "toolset has no live wrapper; run the wrapper phase first")
	case err != nil:
		return false, fmt.Errorf("load wrapper: %w", err)
	}
	row.WrapperID = &wrapper.ID
	retired, err := q.RetireToolsetMCPGrants(ctx, RetireToolsetMCPGrantsParams{
		OrganizationID: toolset.OrganizationID,
		ToolsetID:      toolset.ID.String(),
		WrapperID:      wrapper.ID.String(),
	})
	if err != nil {
		return false, fmt.Errorf("retire toolset grants: %w", err)
	}
	row.GrantsRetired = retired
	if retired == 0 {
		row.Outcome = OutcomeAlreadyComplete
		return false, nil
	}
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeRetiredGrants, OutcomeWouldRetireGrants)
	return true, nil
}

func blocked(row *RowReport, outcome Outcome, reason string) error {
	row.Outcome = outcome
	row.Reason = reason
	row.WrapperID = nil
	row.Endpoints = nil
	row.ClearedRootDomain = nil
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func nullUUIDPtr(v uuid.NullUUID) *uuid.UUID {
	if !v.Valid {
		return nil
	}
	return &v.UUID
}
