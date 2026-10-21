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
	reasonLiveMember       = "the toolset-keyed member is live; this mode only re-keys removal records"
	reasonDuplicateRemoval = "the plugin already has a server-keyed removal record for the canonical server, which keeps the server out"
)

// rekeyRemovalOne moves one soft-deleted toolset-keyed plugin_servers row onto the canonical server key, keeping deleted_at.
// Nothing user-visible changes (role delivery honours both keys), so it is not audited and requests no publication.
func (r *Runner) rekeyRemovalOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetDependentsRow) (RowReport, error) {
	row := RowReport{
		ToolsetID: toolsetID, Table: dep.TableName, RowID: &dep.ID, ProjectID: dep.ProjectID, McpSlug: "", CustomDomainID: nil,
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
		return blocked(row, OutcomeSkippedToolsetGone, "project deleted"), nil
	case toolset.Deleted:
		return blocked(row, OutcomeSkippedToolsetGone, "toolset deleted"), nil
	}

	wrapper, err := q.GetLiveCanonicalWrapper(ctx, GetLiveCanonicalWrapperParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeSkippedNoCanonical, "toolset has no live canonical server"), nil
	case err != nil:
		return row, fmt.Errorf("load canonical server: %w", err)
	}
	row.McpSlug = wrapper.Slug.String

	_, err = q.LockServerKeyedRemovalRecord(ctx, LockServerKeyedRemovalRecordParams{PluginID: record.PluginID, ProjectID: toolset.ProjectID, McpServerID: wrapper.ID})
	switch {
	case err == nil:
		return blocked(row, OutcomeSkippedDuplicate, reasonDuplicateRemoval), nil
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
		return row, nil
	}
	from, to := moveKeys(tablePluginServers, toolsetID)
	return r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: record.ID, Outcome: row.Outcome,
		From: from, To: to, CollapsedTwin: nil, Removed: nil,
		Rekeyed: &RowImage{Table: tablePluginServers, ID: record.ID, Deletion: "soft_deleted", Row: record.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: record.DeletedAt},
	})
}
