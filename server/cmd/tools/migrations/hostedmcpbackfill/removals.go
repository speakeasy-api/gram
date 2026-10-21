package hostedmcpbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

const (
	reasonLiveMember         = "the toolset-keyed member is live; this mode only re-keys removal records"
	reasonNoCanonicalServer  = "toolset has no live canonical server to re-key the removal onto"
	reasonDuplicateRemoval   = "the plugin already has a server-keyed removal record for the canonical server, which keeps the server out"
	reasonDuplicateInThisRun = "an earlier removal record of this plugin is re-keyed onto the same canonical server in this run"
)

func (d ListToolsetRemovalRecordsRow) label() string { return tablePluginServers + " " + d.ID.String() }

// removalPair is one plugin's removal of one canonical server.
type removalPair struct {
	pluginID, serverID uuid.UUID
}

// removalsPhase tracks re-keyed pairs in memory so the dry run reports the same duplicates the apply finds in the database.
func (r *Runner) removalsPhase() dependentPhase[ListToolsetRemovalRecordsRow] {
	rekeyed := make(map[removalPair]struct{})
	return dependentPhase[ListToolsetRemovalRecordsRow]{
		listToolsets: func(ctx context.Context, after uuid.UUID) ([]uuid.UUID, error) {
			page, err := New(r.pool).ListRemovalRecordToolsets(ctx, ListRemovalRecordToolsetsParams{ProjectID: r.options.ProjectID, AfterID: after, PageSize: conv.SafeInt32(r.options.PageSize)})
			if err != nil {
				return nil, fmt.Errorf("list removal record toolsets: %w", err)
			}
			return page, nil
		},
		listDependents: func(ctx context.Context, toolsetID uuid.UUID) ([]ListToolsetRemovalRecordsRow, error) {
			records, err := New(r.pool).ListToolsetRemovalRecords(ctx, ListToolsetRemovalRecordsParams{ToolsetID: toolsetID, ProjectID: r.options.ProjectID})
			if err != nil {
				return nil, fmt.Errorf("list removal records: %w", err)
			}
			return records, nil
		},
		process: func(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetRemovalRecordsRow) (RowReport, error) {
			return r.rekeyRemovalOne(ctx, toolsetID, dep, rekeyed)
		},
	}
}

// rekeyRemovalOne moves one soft-deleted toolset-keyed plugin_servers row onto the canonical server key, keeping deleted_at.
// A soft-deleted row is in no package or listing, so it is not audited and requests no publication.
func (r *Runner) rekeyRemovalOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetRemovalRecordsRow, rekeyed map[removalPair]struct{}) (RowReport, error) {
	row := RowReport{
		ToolsetID: toolsetID, Table: tablePluginServers, RowID: &dep.ID, ProjectID: dep.ProjectID, McpSlug: "", CustomDomainID: nil,
		Outcome: "", Reason: "", Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil, CollapsedTwin: nil, Removed: nil, PublicationRequested: false,
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return row, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := New(tx)
	if err := q.SetMoveLockTimeout(ctx, fmt.Sprintf("%dms", r.options.LockTimeout.Milliseconds())); err != nil {
		return row, fmt.Errorf("set lock timeout: %w", err)
	}
	if err := q.LockToolsetBackfill(ctx, toolsetID.String()); err != nil {
		return row, fmt.Errorf("advisory lock: %w", err)
	}
	// Role delivery holds project admission before its plugin locks, so it sees the record under one key or the other.
	if err := admission.LockProject(ctx, tx, dep.ProjectID); err != nil {
		return row, fmt.Errorf("lock project distribution admission: %w", err)
	}
	toolset, err := q.LockOrphanToolset(ctx, LockOrphanToolsetParams{ID: toolsetID, ProjectID: dep.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeBlockedNoWrapper, "toolset is not in the row's project"), nil
	case err != nil:
		return row, fmt.Errorf("lock toolset: %w", err)
	}
	row.McpSlug = toolset.McpSlug.String

	record, err := q.LockToolsetKeyedRemovalRecord(ctx, LockToolsetKeyedRemovalRecordParams{ID: dep.ID, ProjectID: dep.ProjectID, ToolsetID: toolsetID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return skipped(row, reasonRowChanged), nil
	case err != nil:
		return row, fmt.Errorf("lock plugin server: %w", err)
	case !record.Deleted:
		return blocked(row, OutcomeBlockedLive, reasonLiveMember), nil
	case toolset.ProjectDeleted:
		return blocked(row, OutcomeOrphaned, "project deleted"), nil
	case toolset.Deleted:
		return blocked(row, OutcomeOrphaned, "toolset deleted"), nil
	}

	wrapper, err := q.GetLiveCanonicalWrapper(ctx, GetLiveCanonicalWrapperParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeBlockedNoWrapper, reasonNoCanonicalServer), nil
	case err != nil:
		return row, fmt.Errorf("load canonical server: %w", err)
	}
	row.McpSlug = wrapper.Slug.String

	pair := removalPair{pluginID: record.PluginID, serverID: wrapper.ID}
	if _, ok := rekeyed[pair]; ok {
		return blocked(row, OutcomeBlockedDuplicateRemoval, reasonDuplicateInThisRun), nil
	}
	_, err = q.LockServerKeyedRemovalRecord(ctx, LockServerKeyedRemovalRecordParams{PluginID: record.PluginID, ProjectID: toolset.ProjectID, McpServerID: wrapper.ID})
	switch {
	case err == nil:
		return blocked(row, OutcomeBlockedDuplicateRemoval, reasonDuplicateRemoval), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return row, fmt.Errorf("lock server-keyed removal record: %w", err)
	}

	deletedAt, err := q.RekeyRemovalRecord(ctx, RekeyRemovalRecordParams{McpServerID: wrapper.ID, ID: record.ID, ProjectID: toolset.ProjectID})
	if err != nil {
		return row, fmt.Errorf("re-key removal record: %w", err)
	}
	if deletedAt != record.DeletedAt {
		return row, fmt.Errorf("re-key removal record: deleted_at changed from %s to %s", record.DeletedAt, deletedAt)
	}

	row.Wrote = true
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeRekeyed, OutcomeWouldRekey)
	if !r.options.Apply {
		rekeyed[pair] = struct{}{}
		return row, nil
	}
	from, to := moveKeys(tablePluginServers, toolsetID)
	row, err = r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: record.ID, Outcome: row.Outcome,
		From: from, To: to, CollapsedTwin: nil, Removed: nil,
		Rekeyed: &RowImage{Table: tablePluginServers, ID: record.ID, Deletion: "soft_deleted", Row: record.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: record.DeletedAt},
	})
	if err == nil {
		rekeyed[pair] = struct{}{}
	}
	return row, err
}
