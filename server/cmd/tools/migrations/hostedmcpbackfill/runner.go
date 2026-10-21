// Package hostedmcpbackfill gives legacy hosted toolsets their canonical
// mcp_servers wrapper and endpoint through hostedmcp.Sync, then moves their
// toolset-keyed dependents onto it; see HOSTED_MCP_WRAPPERS_MIGRATION.md.
package hostedmcpbackfill

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ActorComponent is the system principal (system:<ActorComponent>) the backfill's audit entries carry.
const ActorComponent = "hosted-mcp-wrapper-backfill"

const (
	phaseWrappers   = "wrappers"
	phaseDependents = "dependents"
	phaseOrphans    = "orphans"
	phaseRemovals   = "removals"
)

// defaultPageSize bounds each candidate page; rows are processed one transaction at a time regardless.
const defaultPageSize = 100

// defaultLockTimeout bounds each dependent move's lock waits; a timeout stops the run as transient.
const defaultLockTimeout = 5 * time.Second

// Outcome classifies what the run did, or would do, for one toolset.
type Outcome string

const (
	OutcomeWouldCreate              Outcome = "would_create"
	OutcomeCreated                  Outcome = "created"
	OutcomeWouldReconcile           Outcome = "would_reconcile"
	OutcomeReconciled               Outcome = "reconciled"
	OutcomeAlreadyComplete          Outcome = "already_complete"
	OutcomeDeadCustomDomain         Outcome = "dead_custom_domain"
	OutcomeBlockedMultipleEndpoints Outcome = "blocked_multiple_endpoints"
	OutcomeBlockedSlugCollision     Outcome = "blocked_slug_collision"
	OutcomeBlockedCanonicalConflict Outcome = "blocked_canonical_conflict"
	OutcomeBlockedSyncRejected      Outcome = "blocked_sync_rejected"
	OutcomeSkipped                  Outcome = "skipped"

	OutcomeWouldMove        Outcome = "would_move"
	OutcomeMoved            Outcome = "moved"
	OutcomeBlockedTwin      Outcome = "blocked_twin"
	OutcomeBlockedNoWrapper Outcome = "blocked_no_wrapper"
	// OutcomeWouldMoveCollapsingTwin predicts removing the server-keyed twin before moving the toolset-keyed row; OutcomeMovedCollapsedTwin reports that completed move.
	OutcomeWouldMoveCollapsingTwin Outcome = "would_move_collapsing_twin"
	OutcomeMovedCollapsedTwin      Outcome = "moved_collapsed_twin"
	// OutcomeCommitUnknown is a move whose commit errored after the server may have committed it.
	OutcomeCommitUnknown Outcome = "commit_unknown"
	// OutcomeBlockedUnattachable is an assistant attachment the assistants reader does not dial (toolset MCP off or no mcp slug).
	OutcomeBlockedUnattachable Outcome = "blocked_unattachable"
	OutcomeOrphaned            Outcome = "orphaned"

	OutcomeWouldDelete Outcome = "would_delete"
	OutcomeDeleted     Outcome = "deleted"
	// OutcomeBlockedDistributed is an orphan plugin member a platform distribution references.
	OutcomeBlockedDistributed Outcome = "blocked_distributed"
	// OutcomeBlockedForeignConfig is orphan metadata with an env config in another project, which the delete would cascade to.
	OutcomeBlockedForeignConfig Outcome = "blocked_foreign_config"

	OutcomeWouldRekey Outcome = "would_rekey"
	OutcomeRekeyed    Outcome = "rekeyed"
	// OutcomeBlockedLive is a live toolset-keyed plugin member, which the removal re-key never touches.
	OutcomeBlockedLive        Outcome = "blocked_live"
	OutcomeSkippedToolsetGone Outcome = "skipped_toolset_gone"
	OutcomeSkippedNoCanonical Outcome = "skipped_no_canonical"
	OutcomeSkippedDuplicate   Outcome = "skipped_duplicate"
)

// Options controls one run.
type Options struct {
	// ProjectID limits the run to one project when valid.
	ProjectID uuid.NullUUID

	// Cursor resumes after this toolset id (exclusive).
	Cursor uuid.UUID

	// Limit caps the toolsets processed; 0 means all.
	Limit int

	// PageSize is the candidate page size; 0 selects defaultPageSize.
	PageSize int

	// Apply commits writes; otherwise every candidate transaction rolls back.
	Apply bool

	// MoveDependents re-keys toolset-keyed dependents onto the canonical wrapper instead of syncing wrappers.
	MoveDependents bool

	// CleanupOrphans removes toolset-keyed dependents whose toolset or project is deleted.
	CleanupOrphans bool

	// RekeyRemovals re-keys soft-deleted toolset-keyed plugin_servers rows onto the canonical server key.
	RekeyRemovals bool

	// Publication enqueues a plugin publication request when the cleanup removes a member of a live project.
	Publication plugins.PublicationRequests

	// Journal receives each dependent write before and after its commit; required to apply MoveDependents, CleanupOrphans or RekeyRemovals.
	Journal Journal

	// LockTimeout bounds each dependent move's lock waits; 0 selects defaultLockTimeout.
	LockTimeout time.Duration
}

// RowReport carries ids and slugs only, never names or emails.
type RowReport struct {
	ToolsetID uuid.UUID `json:"toolset_id"`

	// Table and RowID name the dependent row in a -move-dependents run.
	Table string     `json:"table,omitempty"`
	RowID *uuid.UUID `json:"row_id,omitempty"`

	ProjectID uuid.UUID `json:"project_id"`

	McpSlug string `json:"mcp_slug"`

	CustomDomainID *uuid.UUID `json:"custom_domain_id,omitempty"`

	Outcome Outcome `json:"outcome"`

	// Reason explains a blocked or skipped outcome.
	Reason string `json:"reason,omitempty"`

	// Wrote reports whether the sync changed (or, in a dry run, would change) any row.
	Wrote bool `json:"wrote"`

	// LiveEndpoints counts the canonical wrapper's live endpoints after the run.
	LiveEndpoints int `json:"live_endpoints"`

	// FreshIDServers counts other live toolset-backed servers for this toolset; never touched.
	FreshIDServers int64 `json:"fresh_id_servers"`

	// DomainsToReconcile are custom domains whose root the sync cleared; they need a domain reconcile.
	DomainsToReconcile []uuid.UUID `json:"domains_to_reconcile,omitempty"`

	// CollapsedTwin is the server-keyed row a dependent move removed, as rollback needs it.
	CollapsedTwin *RowImage `json:"collapsed_twin,omitempty"`

	// Removed names the orphan a -cleanup-orphans run deleted; row images go to the journal only.
	Removed *RowImage `json:"removed,omitempty"`

	// PublicationRequested reports a plugin publication request enqueued with an applied cleanup.
	PublicationRequested bool `json:"publication_requested,omitempty"`
}

// Report summarizes a run.
type Report struct {
	Mode string `json:"mode"`

	Phase string `json:"phase"`

	Scanned int `json:"scanned"`

	// Toolsets counts the toolsets walked; -limit and -cursor count these.
	Toolsets int `json:"toolsets"`

	Outcomes map[Outcome]int `json:"outcomes"`

	// Writes counts transactions that committed (apply) or would commit (dry run): one per toolset, or per dependent row.
	Writes int `json:"writes"`

	// FreshIDServersPresent counts toolsets that also back fresh-id servers.
	FreshIDServersPresent int `json:"fresh_id_servers_present"`

	// LastCursor is the last toolset id processed.
	LastCursor uuid.UUID `json:"last_cursor"`

	Rows []RowReport `json:"rows"`
}

// Runner walks candidate toolsets in id order.
type Runner struct {
	pool    *pgxpool.Pool
	audit   *audit.Logger
	options Options
}

func NewRunner(pool *pgxpool.Pool, options Options) *Runner {
	if options.PageSize <= 0 {
		options.PageSize = defaultPageSize
	}
	if options.LockTimeout <= 0 {
		options.LockTimeout = defaultLockTimeout
	}
	return &Runner{pool: pool, audit: audit.NewLogger(audit.WithoutOutbox()), options: options}
}

// Run processes every candidate. On error the report holds the rows processed so far.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	report := Report{
		Mode:                  conv.Ternary(r.options.Apply, "apply", "dry-run"),
		Phase:                 phaseWrappers,
		Scanned:               0,
		Toolsets:              0,
		Outcomes:              map[Outcome]int{},
		Writes:                0,
		FreshIDServersPresent: 0,
		LastCursor:            r.options.Cursor,
		Rows:                  nil,
	}
	modes := 0
	for _, on := range []bool{r.options.MoveDependents, r.options.CleanupOrphans, r.options.RekeyRemovals} {
		if on {
			modes++
		}
	}
	switch {
	case modes > 1:
		return report, errors.New("the dependent move, the orphan cleanup and the removal re-key are separate phases")
	case r.options.MoveDependents:
		report.Phase = phaseDependents
		return r.runDependents(ctx, report, r.moveOne)
	case r.options.CleanupOrphans:
		report.Phase = phaseOrphans
		return r.runDependents(ctx, report, r.cleanupOne)
	case r.options.RekeyRemovals:
		report.Phase = phaseRemovals
		return r.runDependents(ctx, report, r.rekeyRemovalOne)
	}
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
			report.Toolsets++
			report.Outcomes[row.Outcome]++
			if row.Wrote {
				report.Writes++
			}
			if row.FreshIDServers > 0 {
				report.FreshIDServersPresent++
			}
			report.Rows = append(report.Rows, row)
			report.LastCursor = candidate.ID
			after = candidate.ID
		}
	}
}

type canonicalState struct {
	server    *mcpserversrepo.McpServer
	endpoints []mcpendpointsrepo.McpEndpoint
}

func loadCanonical(ctx context.Context, tx pgx.Tx, toolset toolsetsrepo.Toolset) (canonicalState, error) {
	state := canonicalState{server: nil, endpoints: nil}
	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return state, nil
	case err != nil:
		return state, fmt.Errorf("load canonical server: %w", err)
	}
	state.server = &server
	state.endpoints, err = mcpendpointsrepo.New(tx).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: toolset.ProjectID, McpServerID: toolset.ID})
	if err != nil {
		return state, fmt.Errorf("load canonical endpoints: %w", err)
	}
	return state, nil
}

func (r *Runner) processOne(ctx context.Context, candidate ListCandidateToolsetsRow) (RowReport, error) {
	row := RowReport{
		ToolsetID: candidate.ID, Table: "", RowID: nil, ProjectID: candidate.ProjectID, McpSlug: "", CustomDomainID: nil, Outcome: "", Reason: "",
		Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil, CollapsedTwin: nil, Removed: nil, PublicationRequested: false,
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return row, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := New(tx)
	if err := q.LockToolsetBackfill(ctx, candidate.ID.String()); err != nil {
		return row, fmt.Errorf("advisory lock: %w", err)
	}

	// Toolset row first, as every hostedmcp.Sync caller holds it before Sync locks domains, endpoints, then the server.
	toolsets := toolsetsrepo.New(tx)
	current, err := toolsets.GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: candidate.ID, ProjectID: candidate.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return skipped(row, "toolset deleted"), nil
	}
	if err != nil {
		return row, fmt.Errorf("load toolset: %w", err)
	}
	toolset, err := toolsets.GetToolsetForUpdate(ctx, toolsetsrepo.GetToolsetForUpdateParams{Slug: current.Slug, ProjectID: candidate.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && toolset.ID != candidate.ID) {
		return skipped(row, "toolset changed concurrently; rerun"), nil
	}
	if err != nil {
		return row, fmt.Errorf("lock toolset: %w", err)
	}
	live, err := q.LiveProjectExists(ctx, LiveProjectExistsParams{ID: toolset.ProjectID, OrganizationID: toolset.OrganizationID})
	if err != nil {
		return row, fmt.Errorf("check project: %w", err)
	}
	if !live {
		return skipped(row, "project deleted"), nil
	}
	row.McpSlug = toolset.McpSlug.String
	if toolset.CustomDomainID.Valid {
		row.CustomDomainID = &toolset.CustomDomainID.UUID
	}
	if !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
		return skipped(row, "toolset has no mcp slug"), nil
	}

	row.FreshIDServers, err = q.CountFreshIDServers(ctx, CountFreshIDServersParams{ProjectID: toolset.ProjectID, ToolsetID: toolset.ID})
	if err != nil {
		return row, fmt.Errorf("count fresh-id servers: %w", err)
	}

	identity, err := q.GetServerIdentity(ctx, GetServerIdentityParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case err == nil && identity.Deleted:
		return blocked(row, OutcomeBlockedCanonicalConflict, "canonical id is tombstoned"), nil
	case err == nil && identity.ToolsetID != (uuid.NullUUID{UUID: toolset.ID, Valid: true}):
		return blocked(row, OutcomeBlockedCanonicalConflict, "canonical id belongs to another backend"), nil
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return row, fmt.Errorf("load canonical identity: %w", err)
	}

	dead := false
	if toolset.CustomDomainID.Valid {
		live, err := q.LiveCustomDomainExists(ctx, LiveCustomDomainExistsParams{ID: toolset.CustomDomainID.UUID, OrganizationID: toolset.OrganizationID})
		if err != nil {
			return row, fmt.Errorf("check custom domain: %w", err)
		}
		dead = !live
	}

	before, err := loadCanonical(ctx, tx, toolset)
	if err != nil {
		return row, err
	}
	row.LiveEndpoints = len(before.endpoints)
	if before.server != nil && len(before.endpoints) > 1 {
		return blocked(row, OutcomeBlockedMultipleEndpoints, "canonical wrapper has multiple live endpoints"), nil
	}
	if !dead {
		held, err := q.EndpointAddressHeldElsewhere(ctx, EndpointAddressHeldElsewhereParams{Slug: toolset.McpSlug.String, CustomDomainID: toolset.CustomDomainID, McpServerID: toolset.ID})
		if err != nil {
			return row, fmt.Errorf("check endpoint address: %w", err)
		}
		if held {
			return blocked(row, OutcomeBlockedSlugCollision, "endpoint address held by another server"), nil
		}
	}
	if before.server == nil || before.server.Slug != toolset.McpSlug {
		held, err := q.ServerSlugHeldElsewhere(ctx, ServerSlugHeldElsewhereParams{ProjectID: toolset.ProjectID, Slug: toolset.McpSlug, ID: toolset.ID})
		if err != nil {
			return row, fmt.Errorf("check server slug: %w", err)
		}
		if held {
			return blocked(row, OutcomeBlockedSlugCollision, "server slug held by another server in the project"), nil
		}
	}

	domains, err := hostedmcp.Sync(ctx, tx, r.audit, hostedmcp.SystemActor(ActorComponent), toolset, nil)
	if err != nil {
		outcome, ok := classifySyncError(err)
		if !ok {
			return row, fmt.Errorf("sync hosted wrapper: %w", err)
		}
		return blocked(row, outcome, syncErrorReason(err)), nil
	}

	after, err := loadCanonical(ctx, tx, toolset)
	if err != nil {
		return row, err
	}
	if before.server == nil && after.server == nil {
		return blocked(row, OutcomeBlockedSlugCollision, "sync left the toolset legacy: address or project server slug held by another server"), nil
	}
	row.LiveEndpoints = len(after.endpoints)
	row.DomainsToReconcile = domains
	row.Wrote = !reflect.DeepEqual(before, after)
	switch {
	case dead:
		row.Outcome = OutcomeDeadCustomDomain
	case before.server == nil:
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeCreated, OutcomeWouldCreate)
	case row.Wrote:
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeReconciled, OutcomeWouldReconcile)
	default:
		row.Outcome = OutcomeAlreadyComplete
	}

	if !row.Wrote || !r.options.Apply {
		return row, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return row, fmt.Errorf("commit: %w", err)
	}
	return row, nil
}

func skipped(row RowReport, reason string) RowReport {
	row.Outcome = OutcomeSkipped
	row.Reason = reason
	return row
}

func blocked(row RowReport, outcome Outcome, reason string) RowReport {
	row.Outcome = outcome
	row.Reason = reason
	return row
}

// classifySyncError maps a Sync refusal to a blocked outcome; ok is false for errors that must stop the run.
func classifySyncError(err error) (Outcome, bool) {
	if isTransient(err) {
		return "", false
	}
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) || shareable.Code == oops.CodeUnexpected || shareable.Code == oops.CodeUnauthorized {
		return "", false
	}
	var pgErr *pgconn.PgError
	if errors.Is(err, hostedmcp.ErrAddressInUse) || (errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation) {
		return OutcomeBlockedSlugCollision, true
	}
	return OutcomeBlockedSyncRejected, true
}

// isTransient reports errors a rerun can get past (deadlocks, lock or statement timeouts, cancellation, lost connections).
func isTransient(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) || pgconn.SafeToRetry(err) {
		return true
	}
	var netErr net.Error
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || errors.As(err, &netErr) {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgerrcode.IsTransactionRollback(pgErr.Code) ||
		pgerrcode.IsConnectionException(pgErr.Code) ||
		pgerrcode.IsOperatorIntervention(pgErr.Code) ||
		pgErr.Code == pgerrcode.LockNotAvailable ||
		pgErr.Code == pgerrcode.IdleInTransactionSessionTimeout
}

// syncErrorReason keeps the violated constraint, which the public message hides, so blocked rows name the colliding address.
func syncErrorReason(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName != "" {
		return fmt.Sprintf("%s (constraint %s)", err.Error(), pgErr.ConstraintName)
	}
	return err.Error()
}
