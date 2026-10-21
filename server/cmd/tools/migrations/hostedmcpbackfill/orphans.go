package hostedmcpbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// OrphansActorComponent is the system principal (system:<OrphansActorComponent>) the orphan cleanup audits as.
const OrphansActorComponent = "hosted-mcp-orphan-cleanup"

const (
	reasonRowChanged    = "row changed concurrently; rerun"
	reasonTwinExists    = "a server-keyed twin exists under the toolset's canonical id; resolve by hand"
	reasonForeignConfig = "an env config of this metadata belongs to another project; the delete would cascade to it"
	reasonDistributed   = "a platform distribution references the member"
)

type orphanCleanup struct {
	toolset   LockOrphanToolsetRow
	rowID     uuid.UUID
	actor     urn.Principal
	removed   *RowImage
	published bool
}

// cleanupOne removes one toolset-keyed dependent whose toolset or project is deleted, re-checked under the move's locks.
func (r *Runner) cleanupOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetDependentsRow) (RowReport, error) {
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
	if dep.TableName == tablePluginServers {
		if err := admission.LockProject(ctx, tx, dep.ProjectID); err != nil {
			return row, fmt.Errorf("lock project distribution admission: %w", err)
		}
	}
	toolset, err := q.LockOrphanToolset(ctx, LockOrphanToolsetParams{ID: toolsetID, ProjectID: dep.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeBlockedNoWrapper, "toolset is not in the row's project"), nil
	case err != nil:
		return row, fmt.Errorf("lock toolset: %w", err)
	}
	row.McpSlug = toolset.McpSlug.String

	if !toolset.Deleted && !toolset.ProjectDeleted {
		wrapped, err := q.CanonicalWrapperLive(ctx, CanonicalWrapperLiveParams{ToolsetID: toolset.ID, ProjectID: toolset.ProjectID})
		switch {
		case err != nil:
			return row, fmt.Errorf("check canonical wrapper: %w", err)
		case wrapped:
			return skipped(row, "toolset is live; -move-dependents moves the row"), nil
		case !toolset.McpSlug.Valid || toolset.McpSlug.String == "":
			return blocked(row, OutcomeBlockedNoWrapper, "live toolset without wrapper: it has no mcp slug"), nil
		default:
			return blocked(row, OutcomeBlockedNoWrapper, "live toolset without wrapper: run the wrapper phase first"), nil
		}
	}

	c := &orphanCleanup{toolset: toolset, rowID: dep.ID, actor: urn.NewSystemPrincipal(OrphansActorComponent), removed: nil, published: false}
	var outcome Outcome
	var reason string
	switch dep.TableName {
	case tableMcpMetadata:
		outcome, reason, err = r.cleanupMetadata(ctx, tx, c)
	case tablePluginServers:
		outcome, reason, err = r.cleanupPluginServer(ctx, tx, c)
	case tableAssistantToolsets:
		outcome, reason, err = cleanupAssistantAttachment(ctx, tx, c)
	default:
		return row, fmt.Errorf("unknown dependent table %q", dep.TableName)
	}
	if err != nil {
		return row, err
	}
	if outcome == OutcomeSkipped {
		return skipped(row, reason), nil
	}
	if outcome != "" {
		return blocked(row, outcome, reason), nil
	}

	row.Reason = conv.Ternary(toolset.ProjectDeleted, "project deleted", "toolset deleted")
	row.Removed = c.removed.withoutImages()
	row.Wrote = true
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeDeleted, OutcomeWouldDelete)
	if !r.options.Apply {
		return row, nil
	}
	row.PublicationRequested = c.published
	return r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: dep.ID, Outcome: row.Outcome,
		From: RowKey{Table: dep.TableName, Column: "toolset_id", ID: toolsetID}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
		CollapsedTwin: nil, Removed: c.removed,
	})
}

// cleanupMetadata deletes the row; its mcp_environment_configs are locked and imaged first, then cascade.
func (r *Runner) cleanupMetadata(ctx context.Context, tx pgx.Tx, c *orphanCleanup) (Outcome, string, error) {
	q := New(tx)
	locked, err := q.LockOrphanMetadata(ctx, LockOrphanMetadataParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, reasonRowChanged, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock metadata: %w", err)
	}
	_, err = q.LockServerKeyedMetadataTwin(ctx, LockServerKeyedMetadataTwinParams{McpServerID: uuid.NullUUID{UUID: c.toolset.ID, Valid: true}, ProjectID: c.toolset.ProjectID})
	switch {
	case err == nil:
		return OutcomeBlockedTwin, reasonTwinExists, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", "", fmt.Errorf("lock metadata twin: %w", err)
	}
	// The locked metadata row blocks new configs (their FK needs it), so this check and the image stay exact.
	foreign, err := q.MetadataHasForeignEnvironmentConfig(ctx, MetadataHasForeignEnvironmentConfigParams{McpMetadataID: c.rowID, ProjectID: c.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("check foreign environment configs: %w", err)
	}
	if foreign {
		return OutcomeBlockedForeignConfig, reasonForeignConfig, nil
	}
	configs, err := q.LockMetadataEnvironmentConfigs(ctx, LockMetadataEnvironmentConfigsParams{McpMetadataID: c.rowID, ProjectID: c.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("lock metadata environment configs: %w", err)
	}
	configsJSON, err := q.MetadataEnvironmentConfigsJSON(ctx, MetadataEnvironmentConfigsJSONParams{McpMetadataID: c.rowID, ProjectID: c.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("image metadata environment configs: %w", err)
	}
	beforeSnapshot, err := metadataSnapshot(locked.McpMetadatum, configs)
	if err != nil {
		return "", "", err
	}
	deleted, err := q.DeleteOrphanMetadata(ctx, DeleteOrphanMetadataParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if err != nil {
		return "", "", fmt.Errorf("delete metadata: %w", err)
	}
	if deleted != 1 {
		return "", "", fmt.Errorf("delete metadata: %d rows deleted", deleted)
	}
	// No metadata delete action exists; an update with no after snapshot records the removal.
	if err := r.audit.LogMCPMetadataUpdate(ctx, tx, audit.LogMCPMetadataUpdateEvent{
		OrganizationID: c.toolset.OrganizationID, ProjectID: c.toolset.ProjectID,
		Actor: c.actor, ActorDisplayName: nil, ActorSlug: nil,
		ToolsetURN: urn.NewToolset(c.toolset.ID), ToolsetName: c.toolset.Name, ToolsetSlug: c.toolset.Slug,
		MCPMetadataSnapshotBefore: beforeSnapshot,
		MCPMetadataSnapshotAfter:  nil,
	}); err != nil {
		return "", "", fmt.Errorf("audit metadata delete: %w", err)
	}
	c.removed = &RowImage{Table: tableMcpMetadata, ID: c.rowID, Deletion: "deleted", Row: locked.RowJson, EnvironmentConfigs: configsJSON, SoftDeletedAt: ""}
	return "", "", nil
}

// cleanupPluginServer soft-deletes the member and requests a package refresh, as toolset deletion does.
func (r *Runner) cleanupPluginServer(ctx context.Context, tx pgx.Tx, c *orphanCleanup) (Outcome, string, error) {
	q := New(tx)
	member, err := q.LockOrphanPluginServer(ctx, LockOrphanPluginServerParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, reasonRowChanged, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock plugin server: %w", err)
	}
	if member.Distributed {
		return OutcomeBlockedDistributed, reasonDistributed, nil
	}
	_, err = q.LockServerKeyedPluginServerTwin(ctx, LockServerKeyedPluginServerTwinParams{PluginID: member.PluginID, ProjectID: c.toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: c.toolset.ID, Valid: true}})
	switch {
	case err == nil:
		return OutcomeBlockedTwin, reasonTwinExists, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", "", fmt.Errorf("lock plugin server twin: %w", err)
	}
	deletedAt, err := q.SoftDeleteOrphanPluginServer(ctx, SoftDeleteOrphanPluginServerParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if err != nil {
		return "", "", fmt.Errorf("soft-delete plugin server: %w", err)
	}
	toolsetURN := urn.NewToolset(c.toolset.ID)
	if err := r.audit.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
		OrganizationID: c.toolset.OrganizationID, ProjectID: c.toolset.ProjectID,
		Actor: c.actor, ActorDisplayName: nil, ActorSlug: nil,
		PluginID: member.PluginID, PluginName: member.PluginName, PluginSlug: member.PluginSlug,
		ServerID: member.ID, ToolsetURN: &toolsetURN, McpServerURN: nil, MetaMcpServerURN: nil,
	}); err != nil {
		return "", "", fmt.Errorf("audit plugin server removal: %w", err)
	}
	if !c.toolset.ProjectDeleted {
		outcome, err := r.options.Publication.ProjectWithOutcome(ctx, tx, c.toolset.OrganizationID, c.toolset.ProjectID, "")
		if err != nil {
			return "", "", fmt.Errorf("request plugin publication: %w", err)
		}
		c.published = outcome == plugins.ProjectPublicationEnqueued
	}
	c.removed = &RowImage{Table: tablePluginServers, ID: c.rowID, Deletion: "soft_deleted", Row: member.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: deletedAt}
	return "", "", nil
}

// cleanupAssistantAttachment deletes the attachment, as toolset deletion does; attachment changes are not audited on any path.
func cleanupAssistantAttachment(ctx context.Context, tx pgx.Tx, c *orphanCleanup) (Outcome, string, error) {
	q := New(tx)
	owner, err := q.GetAssistantToolsetOwner(ctx, GetAssistantToolsetOwnerParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, reasonRowChanged, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load assistant toolset: %w", err)
	}
	_, err = q.LockAssistantForMove(ctx, LockAssistantForMoveParams{ID: owner, ProjectID: c.toolset.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeOrphaned, "assistant is not in the row's project", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock assistant: %w", err)
	}
	attachment, err := q.LockOrphanAssistantToolset(ctx, LockOrphanAssistantToolsetParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, reasonRowChanged, nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock assistant toolset: %w", err)
	}
	_, err = q.LockAssistantMcpServerTwin(ctx, LockAssistantMcpServerTwinParams{AssistantID: attachment.AssistantID, McpServerID: c.toolset.ID, ProjectID: c.toolset.ProjectID})
	switch {
	case err == nil:
		return OutcomeBlockedTwin, reasonTwinExists, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", "", fmt.Errorf("lock assistant twin: %w", err)
	}
	deleted, err := q.DeleteOrphanAssistantToolset(ctx, DeleteOrphanAssistantToolsetParams{ID: c.rowID, ProjectID: c.toolset.ProjectID, ToolsetID: c.toolset.ID})
	if err != nil {
		return "", "", fmt.Errorf("delete assistant toolset: %w", err)
	}
	if deleted != 1 {
		return "", "", fmt.Errorf("delete assistant toolset: %d rows deleted", deleted)
	}
	c.removed = &RowImage{Table: tableAssistantToolsets, ID: c.rowID, Deletion: "deleted", Row: attachment.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: ""}
	return "", "", nil
}
