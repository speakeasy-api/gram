// Package hostedmcpbackfill gives legacy hosted toolsets their canonical
// mcp_servers wrapper and endpoint through hostedmcp.Sync; see
// HOSTED_MCP_WRAPPERS_MIGRATION.md.
package hostedmcpbackfill

import (
	"context"
	"errors"
	"fmt"
	"reflect"

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
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ActorComponent is the system principal (system:<ActorComponent>) the backfill's audit entries carry.
const ActorComponent = "hosted-mcp-wrapper-backfill"

// defaultPageSize bounds each candidate page; rows are processed one transaction at a time regardless.
const defaultPageSize = 100

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
}

// RowReport carries ids and slugs only, never names or emails.
type RowReport struct {
	ToolsetID uuid.UUID `json:"toolset_id"`

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
}

// Report summarizes a run.
type Report struct {
	Mode string `json:"mode"`

	Scanned int `json:"scanned"`

	Outcomes map[Outcome]int `json:"outcomes"`

	// Writes counts toolsets whose transaction committed (apply) or would commit (dry run).
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
	return &Runner{pool: pool, audit: audit.NewLogger(), options: options}
}

// Run processes every candidate. On error the report holds the rows processed so far.
func (r *Runner) Run(ctx context.Context) (Report, error) {
	report := Report{
		Mode:                  conv.Ternary(r.options.Apply, "apply", "dry-run"),
		Scanned:               0,
		Outcomes:              map[Outcome]int{},
		Writes:                0,
		FreshIDServersPresent: 0,
		LastCursor:            r.options.Cursor,
		Rows:                  nil,
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
		ToolsetID: candidate.ID, ProjectID: candidate.ProjectID, McpSlug: "", CustomDomainID: nil, Outcome: "", Reason: "",
		Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil,
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
		return blocked(row, outcome, err.Error()), nil
	}

	after, err := loadCanonical(ctx, tx, toolset)
	if err != nil {
		return row, err
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
