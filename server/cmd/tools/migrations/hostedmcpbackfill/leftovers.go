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
	reasonLeftoverLive              = "the toolset-keyed member is live; this mode only deletes removal records"
	reasonNeedsRekey                = "the canonical server has no server-keyed removal record for this plugin; run -rekey-removal-records first"
	reasonLeftoverDuplicate         = "the plugin's server-keyed removal record for the canonical server keeps the server out"
	reasonLeftoverNoCanonical       = "toolset has no live canonical server whose removal record would keep it out"
	reasonDeletedToolsetLiveWrapper = "deleted toolset still has a live wrapper server"
	reasonDuplicateLiveWrapper      = "a live non-canonical wrapper server has no server-keyed removal record for this plugin"
)

// Eligibility cases DeleteLeftoverRemovalRecord rechecks.
const (
	eligibilityProjectDeleted = "project_deleted"
	eligibilityToolsetDeleted = "toolset_deleted"
	eligibilityDuplicate      = "duplicate"
)

// leftoversPhase walks the same rows as the removal re-key.
func (r *Runner) leftoversPhase() dependentPhase[ListToolsetRemovalRecordsRow] {
	phase := r.removalsPhase()
	phase.process = r.deleteLeftoverOne
	return phase
}

// deleteLeftoverOne hard-deletes one toolset-keyed removal record the re-key cannot move.
func (r *Runner) deleteLeftoverOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetRemovalRecordsRow) (RowReport, error) {
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
	// Admission also fences distribution inserts, so the distribution check below stays exact until commit.
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
		return blocked(row, OutcomeBlockedLive, reasonLeftoverLive), nil
	}

	var eligibility string
	switch {
	case toolset.ProjectDeleted:
		row.Reason, eligibility = "project deleted", eligibilityProjectDeleted
	default:
		// Role delivery still lists live servers of a deleted toolset, so this record may be what keeps them out.
		servers, err := q.LockLiveToolsetServers(ctx, LockLiveToolsetServersParams{ProjectID: toolset.ProjectID, ToolsetID: toolset.ID})
		if err != nil {
			return row, fmt.Errorf("lock live toolset servers: %w", err)
		}
		if toolset.Deleted {
			if len(servers) > 0 {
				return blocked(row, OutcomeBlockedLiveWrapper, reasonDeletedToolsetLiveWrapper), nil
			}
			row.Reason, eligibility = "toolset deleted", eligibilityToolsetDeleted
			break
		}
		wrapper, err := q.GetLiveCanonicalWrapper(ctx, GetLiveCanonicalWrapperParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return blocked(row, OutcomeBlockedNoWrapper, reasonLeftoverNoCanonical), nil
		case err != nil:
			return row, fmt.Errorf("load canonical server: %w", err)
		}
		row.McpSlug = wrapper.Slug.String
		_, err = q.LockServerKeyedRemovalRecord(ctx, LockServerKeyedRemovalRecordParams{PluginID: record.PluginID, ProjectID: toolset.ProjectID, McpServerID: wrapper.ID})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return blocked(row, OutcomeBlockedNeedsRekey, reasonNeedsRekey), nil
		case err != nil:
			return row, fmt.Errorf("lock server-keyed removal record: %w", err)
		}
		for _, server := range servers {
			if server == wrapper.ID {
				continue
			}
			_, err := q.LockServerKeyedRemovalRecord(ctx, LockServerKeyedRemovalRecordParams{PluginID: record.PluginID, ProjectID: toolset.ProjectID, McpServerID: server})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return blocked(row, OutcomeBlockedLiveWrapper, reasonDuplicateLiveWrapper), nil
			case err != nil:
				return row, fmt.Errorf("lock wrapper server-keyed removal record: %w", err)
			}
		}
		row.Reason, eligibility = reasonLeftoverDuplicate, eligibilityDuplicate
	}

	// The FK is ON DELETE NO ACTION, so a referenced row would fail the delete; report it instead.
	if record.Distributed {
		return blocked(row, OutcomeBlockedDistributed, reasonDistributed), nil
	}

	deleted, err := q.DeleteLeftoverRemovalRecord(ctx, DeleteLeftoverRemovalRecordParams{ID: record.ID, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, Eligibility: eligibility})
	switch {
	case err != nil:
		return row, fmt.Errorf("delete removal record: %w", err)
	case deleted == 0:
		return skipped(row, reasonRowChanged), nil
	case deleted != 1:
		return row, fmt.Errorf("delete removal record: %d rows deleted", deleted)
	}

	removed := &RowImage{Table: tablePluginServers, ID: record.ID, Deletion: "deleted", Row: record.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: record.DeletedAt, RekeyedUpdatedAt: ""}
	row.Removed = removed.withoutImages()
	row.Wrote = true
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeDeleted, OutcomeWouldDelete)
	if !r.options.Apply {
		return row, nil
	}
	return r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: record.ID, Outcome: row.Outcome,
		From: RowKey{Table: tablePluginServers, Column: "toolset_id", ID: toolsetID}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
		CollapsedTwin: nil, Removed: removed, Rekeyed: nil,
	})
}
