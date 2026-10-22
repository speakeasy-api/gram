package hostedmcpbackfill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DependentsActorComponent is the system principal (system:<DependentsActorComponent>) the dependent move audits as.
const DependentsActorComponent = "hosted-mcp-dependent-move"

const (
	tableMcpMetadata         = "mcp_metadata"
	tablePluginServers       = "plugin_servers"
	tableAssistantToolsets   = "assistant_toolsets"
	tableAssistantMcpServers = "assistant_mcp_servers"
)

// twinConstraints are the only unique violations a move maps to blocked_twin; any other stops the run.
var twinConstraints = map[string]string{
	tableMcpMetadata:       "mcp_metadata_mcp_server_id_key",
	tablePluginServers:     "plugin_servers_plugin_id_mcp_server_id_key",
	tableAssistantToolsets: "assistant_mcp_servers_assistant_id_mcp_server_id_key",
}

// twinViolation reports the violated twin constraint when err is the expected unique violation for table.
func twinViolation(table string, err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation {
		return "", false
	}
	want, ok := twinConstraints[table]
	if !ok || pgErr.ConstraintName != want {
		return "", false
	}
	return want, true
}

// RowImage names a row a run removed: a move's collapsed server-keyed twin, or a cleaned-up orphan.
// Row (to_jsonb) and EnvironmentConfigs are carried only where a rollback needs them.
type RowImage struct {
	Table string    `json:"table"`
	ID    uuid.UUID `json:"id"`

	// Deletion is "deleted" (hard delete) or "soft_deleted" (plugin_servers).
	Deletion string          `json:"deletion"`
	Row      json.RawMessage `json:"row,omitempty"`

	// EnvironmentConfigs are the mcp_environment_configs rows deleted with a metadata row.
	EnvironmentConfigs json.RawMessage `json:"environment_configs,omitempty"`

	// SoftDeletedAt is the deleted_at the orphan cleanup set, so a restore undoes only that delete.
	SoftDeletedAt string `json:"soft_deleted_at,omitempty"`

	// RekeyedUpdatedAt is the updated_at a re-key set, so a restore skips a row changed since.
	RekeyedUpdatedAt string `json:"rekeyed_updated_at,omitempty"`
}

// withoutImages keeps the row's identity only, for reports.
func (i *RowImage) withoutImages() *RowImage {
	if i == nil {
		return nil
	}
	return &RowImage{Table: i.Table, ID: i.ID, Deletion: i.Deletion, Row: nil, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""}
}

// RowKey names the column a dependent row is keyed by.
type RowKey struct {
	Table  string    `json:"table"`
	Column string    `json:"column"`
	ID     uuid.UUID `json:"id"`
}

// JournalEvent is the stage of one row's journal entry.
type JournalEvent string

const (
	// JournalIntent is synced before the commit; without a later committed or commit_unknown entry the row did not commit and its rollback is a no-op.
	JournalIntent        JournalEvent = "intent"
	JournalCommitted     JournalEvent = "committed"
	JournalCommitUnknown JournalEvent = "commit_unknown"
)

// JournalRecord carries what a rollback needs to reverse one row's move.
type JournalRecord struct {
	Event     JournalEvent `json:"event"`
	ProjectID uuid.UUID    `json:"project_id"`
	ToolsetID uuid.UUID    `json:"toolset_id"`
	RowID     uuid.UUID    `json:"row_id"`
	Outcome   Outcome      `json:"outcome"`

	From RowKey `json:"from"`
	To   RowKey `json:"to,omitzero"`

	CollapsedTwin *RowImage `json:"collapsed_twin,omitempty"`

	// Removed is the deleted row in a -cleanup-orphans or -delete-leftover-removal-records journal, with its row image; To is then empty.
	Removed *RowImage `json:"removed,omitempty"`

	// Rekeyed is the removal record's row image before a -rekey-removal-records re-key.
	Rekeyed *RowImage `json:"rekeyed,omitempty"`

	// Dependents are the rows a -delete-orphan-wrapper-servers delete changed with the server in Removed.
	Dependents *WrapperDependents `json:"dependents,omitempty"`
}

// Journal records each dependent row's move durably around its commit.
type Journal interface {
	Append(record JournalRecord) error
}

// FileJournal appends one JSON line per record and fsyncs it before returning.
type FileJournal struct {
	file *os.File
}

func NewFileJournal(file *os.File) *FileJournal {
	return &FileJournal{file: file}
}

func (j *FileJournal) Append(record JournalRecord) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode journal record: %w", err)
	}
	if _, err := j.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write journal record: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("sync journal: %w", err)
	}
	return nil
}

func moveKeys(table string, key uuid.UUID) (RowKey, RowKey) {
	if table == tableAssistantToolsets {
		return RowKey{Table: tableAssistantToolsets, Column: "toolset_id", ID: key}, RowKey{Table: tableAssistantMcpServers, Column: "mcp_server_id", ID: key}
	}
	return RowKey{Table: table, Column: "toolset_id", ID: key}, RowKey{Table: table, Column: "mcp_server_id", ID: key}
}

// dependentPhase lists a phase's toolsets and their rows and processes each row.
type dependentPhase[D dependentRow] struct {
	listToolsets   func(ctx context.Context, after uuid.UUID) ([]uuid.UUID, error)
	listDependents func(ctx context.Context, toolsetID uuid.UUID) ([]D, error)
	process        func(ctx context.Context, toolsetID uuid.UUID, dep D) (RowReport, error)
}

type dependentRow interface {
	label() string
}

func (d ListToolsetDependentsRow) label() string { return d.TableName + " " + d.ID.String() }

func (r *Runner) dependentsPhase(process func(context.Context, uuid.UUID, ListToolsetDependentsRow) (RowReport, error)) dependentPhase[ListToolsetDependentsRow] {
	return dependentPhase[ListToolsetDependentsRow]{
		listToolsets: func(ctx context.Context, after uuid.UUID) ([]uuid.UUID, error) {
			page, err := New(r.pool).ListDependentToolsets(ctx, ListDependentToolsetsParams{ProjectID: r.options.ProjectID, AfterID: after, PageSize: conv.SafeInt32(r.options.PageSize)})
			if err != nil {
				return nil, fmt.Errorf("list dependent toolsets: %w", err)
			}
			return page, nil
		},
		listDependents: func(ctx context.Context, toolsetID uuid.UUID) ([]ListToolsetDependentsRow, error) {
			deps, err := New(r.pool).ListToolsetDependents(ctx, ListToolsetDependentsParams{ToolsetID: toolsetID, ProjectID: r.options.ProjectID})
			if err != nil {
				return nil, fmt.Errorf("list dependents: %w", err)
			}
			return deps, nil
		},
		process: process,
	}
}

// runDependents walks toolsets that still have toolset-keyed rows; Limit and the cursor count toolsets.
func runDependents[D dependentRow](ctx context.Context, r *Runner, report Report, phase dependentPhase[D]) (Report, error) {
	if r.options.Apply && r.options.Journal == nil {
		return report, fmt.Errorf("applying the %s phase requires a journal", report.Phase)
	}
	after := r.options.Cursor
	for {
		page, err := phase.listToolsets(ctx, after)
		if err != nil {
			return report, err
		}
		if len(page) == 0 {
			return report, nil
		}
		for _, toolsetID := range page {
			if r.options.Limit > 0 && report.Toolsets >= r.options.Limit {
				return report, nil
			}
			deps, err := phase.listDependents(ctx, toolsetID)
			if err != nil {
				return report, fmt.Errorf("toolset %s: %w", toolsetID, err)
			}
			for _, dep := range deps {
				row, err := phase.process(ctx, toolsetID, dep)
				if err != nil && row.Outcome == "" {
					return report, fmt.Errorf("toolset %s: %s: %w", toolsetID, dep.label(), err)
				}
				report.Scanned++
				report.Outcomes[row.Outcome]++
				if row.Wrote {
					report.Writes++
				}
				report.Rows = append(report.Rows, row)
				if err != nil {
					return report, fmt.Errorf("toolset %s: %s: %w", toolsetID, dep.label(), err)
				}
			}
			report.Toolsets++
			report.LastCursor = toolsetID
			after = toolsetID
		}
	}
}

type dependentMove struct {
	toolset LockDependentToolsetRow
	wrapper *GetLiveCanonicalWrapperRow
	rowID   uuid.UUID
	actor   urn.Principal
	twin    *RowImage
}

// loadWrapper runs after the dependent row is locked, matching the writers' toolset -> dependent -> server order.
func (m *dependentMove) loadWrapper(ctx context.Context, q *Queries) (Outcome, string, error) {
	wrapper, err := q.GetLiveCanonicalWrapper(ctx, GetLiveCanonicalWrapperParams{ToolsetID: m.toolset.ID, ProjectID: m.toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows) && (!m.toolset.McpSlug.Valid || m.toolset.McpSlug.String == ""):
		return OutcomeBlockedNoWrapper, "toolset has no mcp slug, so it never gets a wrapper", nil
	case errors.Is(err, pgx.ErrNoRows):
		return OutcomeBlockedNoWrapper, "toolset has no live canonical wrapper", nil
	case err != nil:
		return "", "", fmt.Errorf("load canonical wrapper: %w", err)
	}
	m.wrapper = &wrapper
	return "", "", nil
}

func (r *Runner) moveOne(ctx context.Context, toolsetID uuid.UUID, dep ListToolsetDependentsRow) (RowReport, error) {
	row := RowReport{
		ToolsetID: toolsetID, Table: dep.TableName, RowID: &dep.ID, ProjectID: dep.ProjectID, McpSlug: "", CustomDomainID: nil,
		Outcome: "", Reason: "", Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil, CollapsedTwin: nil, Removed: nil, PublicationRequested: false, Detached: nil,
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
	// Admission before the toolset row (UpdateToolset order); it also fences distribute inserts.
	if dep.TableName == tablePluginServers {
		if err := admission.LockProject(ctx, tx, dep.ProjectID); err != nil {
			return row, fmt.Errorf("lock project distribution admission: %w", err)
		}
	}
	toolset, err := q.LockDependentToolset(ctx, LockDependentToolsetParams{ID: toolsetID, ProjectID: dep.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return blocked(row, OutcomeBlockedNoWrapper, "toolset is not in the row's project"), nil
	case err != nil:
		return row, fmt.Errorf("lock toolset: %w", err)
	case toolset.ProjectDeleted:
		return blocked(row, OutcomeOrphaned, "project deleted"), nil
	case toolset.Deleted:
		return blocked(row, OutcomeOrphaned, "toolset deleted"), nil
	}

	move := &dependentMove{toolset: toolset, wrapper: nil, rowID: dep.ID, actor: urn.NewSystemPrincipal(DependentsActorComponent), twin: nil}
	var outcome Outcome
	var reason string
	switch dep.TableName {
	case tableMcpMetadata:
		outcome, reason, err = r.moveMetadata(ctx, tx, move)
	case tablePluginServers:
		outcome, reason, err = r.movePluginServer(ctx, tx, move)
	case tableAssistantToolsets:
		outcome, reason, err = moveAssistantAttachment(ctx, tx, move)
	default:
		return row, fmt.Errorf("unknown dependent table %q", dep.TableName)
	}
	if move.wrapper != nil {
		row.McpSlug = move.wrapper.Slug.String
	}
	if constraint, ok := twinViolation(dep.TableName, err); ok {
		return blocked(row, OutcomeBlockedTwin, fmt.Sprintf("a server-keyed twin appeared outside the collapse (constraint %s); rerun", constraint)), nil
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

	row.CollapsedTwin = move.twin
	if move.twin != nil && !r.options.Apply {
		// A dry-run report may reuse an existing file, so it carries no row snapshot.
		row.CollapsedTwin = move.twin.withoutImages()
	}
	row.Wrote = true
	if move.twin != nil {
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeMovedCollapsedTwin, OutcomeWouldMoveCollapsingTwin)
	} else {
		row.Outcome = conv.Ternary(r.options.Apply, OutcomeMoved, OutcomeWouldMove)
	}
	if !r.options.Apply {
		return row, nil
	}

	from, to := moveKeys(dep.TableName, toolsetID)
	return r.commitJournaled(ctx, tx, row, JournalRecord{
		Event: JournalIntent, ProjectID: toolset.ProjectID, ToolsetID: toolsetID, RowID: dep.ID, Outcome: row.Outcome,
		From: from, To: to, CollapsedTwin: move.twin, Removed: nil, Rekeyed: nil, Dependents: nil,
	})
}

// commitJournaled syncs the intent, commits, then records whether the commit landed.
func (r *Runner) commitJournaled(ctx context.Context, tx pgx.Tx, row RowReport, record JournalRecord) (RowReport, error) {
	if err := r.options.Journal.Append(record); err != nil {
		row.Outcome, row.Wrote = "", false
		return row, fmt.Errorf("journal intent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		record.Event = JournalCommitUnknown
		row.Outcome, row.Wrote = OutcomeCommitUnknown, false
		row.Reason = "commit failed after the server may have committed; the journal intent covers rollback"
		return row, errors.Join(fmt.Errorf("commit outcome unknown: %w", err), r.options.Journal.Append(record))
	}
	record.Event = JournalCommitted
	if err := r.options.Journal.Append(record); err != nil {
		return row, fmt.Errorf("journal commit: %w", err)
	}
	return row, nil
}

// moveMetadata re-keys in place, so mcp_environment_configs keep pointing at the same row.
func (r *Runner) moveMetadata(ctx context.Context, tx pgx.Tx, m *dependentMove) (Outcome, string, error) {
	q := New(tx)
	before, err := q.LockToolsetKeyedMetadata(ctx, LockToolsetKeyedMetadataParams{ID: m.rowID, ProjectID: m.toolset.ProjectID, ToolsetID: uuid.NullUUID{UUID: m.toolset.ID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, "row changed concurrently; rerun", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock metadata: %w", err)
	}
	if outcome, reason, err := m.loadWrapper(ctx, q); outcome != "" || err != nil {
		return outcome, reason, err
	}
	serverKey := uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}
	twin, err := q.LockServerKeyedMetadataTwin(ctx, LockServerKeyedMetadataTwinParams{McpServerID: serverKey, ProjectID: m.toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", "", fmt.Errorf("lock metadata twin: %w", err)
	default:
		// The toolset-keyed row wins, as phase A readers prefer it; the twin's env configs go with it.
		configs, err := q.MetadataEnvironmentConfigsJSON(ctx, MetadataEnvironmentConfigsJSONParams{McpMetadataID: twin.ID, ProjectID: m.toolset.ProjectID})
		if err != nil {
			return "", "", fmt.Errorf("snapshot metadata twin environment configs: %w", err)
		}
		deleted, err := q.DeleteMetadataTwin(ctx, DeleteMetadataTwinParams{ID: twin.ID, ProjectID: m.toolset.ProjectID, McpServerID: serverKey})
		if err != nil {
			return "", "", fmt.Errorf("delete metadata twin: %w", err)
		}
		if deleted != 1 {
			return "", "", fmt.Errorf("delete metadata twin: %d rows deleted", deleted)
		}
		m.twin = &RowImage{Table: tableMcpMetadata, ID: twin.ID, Deletion: "deleted", Row: twin.RowJson, EnvironmentConfigs: configs, SoftDeletedAt: "", RekeyedUpdatedAt: ""}
	}
	after, err := q.RekeyMetadata(ctx, RekeyMetadataParams{McpServerID: serverKey, ID: m.rowID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("re-key metadata: %w", err)
	}
	configs, err := q.ListMetadataEnvironmentConfigs(ctx, ListMetadataEnvironmentConfigsParams{McpMetadataID: m.rowID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("list metadata environment configs: %w", err)
	}
	beforeSnapshot, err := metadataSnapshot(before, configs)
	if err != nil {
		return "", "", err
	}
	afterSnapshot, err := metadataSnapshot(after, configs)
	if err != nil {
		return "", "", err
	}
	if err := r.audit.LogMCPMetadataUpdateForMcpServer(ctx, tx, audit.LogMCPMetadataUpdateForMcpServerEvent{
		OrganizationID: m.toolset.OrganizationID, ProjectID: m.toolset.ProjectID,
		Actor: m.actor, ActorDisplayName: nil, ActorSlug: nil,
		McpServerURN: urn.NewMcpServer(m.wrapper.ID), McpServerName: m.wrapper.Name.String, McpServerSlug: m.wrapper.Slug.String,
		MCPMetadataSnapshotBefore: beforeSnapshot,
		MCPMetadataSnapshotAfter:  afterSnapshot,
	}); err != nil {
		return "", "", fmt.Errorf("audit metadata move: %w", err)
	}
	return "", "", nil
}

// metadataSnapshot mirrors the mcpmetadata view, including deprecated header display names.
func metadataSnapshot(record McpMetadatum, configs []McpEnvironmentConfig) (*types.McpMetadata, error) {
	headerDisplayNames := map[string]string{}
	if len(record.HeaderDisplayNames) > 0 {
		if err := json.Unmarshal(record.HeaderDisplayNames, &headerDisplayNames); err != nil {
			return nil, fmt.Errorf("decode header display names: %w", err)
		}
	}
	environmentConfigs := make([]*types.McpEnvironmentConfig, 0, len(configs)+len(headerDisplayNames))
	seen := make(map[string]bool, len(configs))
	for _, config := range configs {
		seen[config.VariableName] = true
		headerDisplayName := conv.FromPGText[string](config.HeaderDisplayName)
		if deprecated, ok := headerDisplayNames[config.VariableName]; headerDisplayName == nil && ok {
			headerDisplayName = &deprecated
		}
		environmentConfigs = append(environmentConfigs, &types.McpEnvironmentConfig{
			ID: config.ID.String(), VariableName: config.VariableName, HeaderDisplayName: headerDisplayName, ProvidedBy: config.ProvidedBy,
			CreatedAt: config.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: config.UpdatedAt.Time.Format(time.RFC3339),
		})
	}
	for _, name := range slices.Sorted(maps.Keys(headerDisplayNames)) {
		if seen[name] {
			continue
		}
		displayName := headerDisplayNames[name]
		environmentConfigs = append(environmentConfigs, &types.McpEnvironmentConfig{
			ID: uuid.Nil.String(), VariableName: name, HeaderDisplayName: &displayName, ProvidedBy: "user",
			CreatedAt: record.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: record.UpdatedAt.Time.Format(time.RFC3339),
		})
	}
	return &types.McpMetadata{
		ID:                        record.ID.String(),
		ToolsetID:                 conv.FromNullableUUID(record.ToolsetID),
		McpServerID:               conv.FromNullableUUID(record.McpServerID),
		LogoAssetID:               conv.FromNullableUUID(record.LogoID),
		ExternalDocumentationURL:  conv.FromPGText[string](record.ExternalDocumentationUrl),
		ExternalDocumentationText: conv.FromPGText[string](record.ExternalDocumentationText),
		Instructions:              conv.FromPGText[string](record.Instructions),
		DefaultEnvironmentID:      conv.FromNullableUUID(record.DefaultEnvironmentID),
		InstallationOverrideURL:   conv.FromPGText[string](record.InstallationOverrideUrl),
		EnvironmentConfigs:        environmentConfigs,
		CreatedAt:                 record.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                 record.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

// movePluginServer re-keys in place, so platform distributions referencing the member keep it.
func (r *Runner) movePluginServer(ctx context.Context, tx pgx.Tx, m *dependentMove) (Outcome, string, error) {
	q := New(tx)
	member, err := q.LockToolsetKeyedPluginServer(ctx, LockToolsetKeyedPluginServerParams{ID: m.rowID, ProjectID: m.toolset.ProjectID, ToolsetID: uuid.NullUUID{UUID: m.toolset.ID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, "row changed concurrently; rerun", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock plugin server: %w", err)
	}
	if outcome, reason, err := m.loadWrapper(ctx, q); outcome != "" || err != nil {
		return outcome, reason, err
	}
	serverKey := uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}
	serverURN, toolsetURN := urn.NewMcpServer(m.wrapper.ID), urn.NewToolset(m.toolset.ID)
	twin, err := q.LockServerKeyedPluginServerTwin(ctx, LockServerKeyedPluginServerTwinParams{PluginID: member.PluginID, ProjectID: m.toolset.ProjectID, McpServerID: serverKey})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", "", fmt.Errorf("lock plugin server twin: %w", err)
	case twin.Distributed:
		return OutcomeBlockedTwin, "the plugin's server-keyed member for the wrapper is referenced by a platform distribution", nil
	default:
		// The toolset-keyed member wins, as phase A readers prefer it; the twin is soft-deleted like a removal.
		deleted, err := q.SoftDeletePluginServerTwin(ctx, SoftDeletePluginServerTwinParams{ID: twin.ID, ProjectID: m.toolset.ProjectID, McpServerID: serverKey})
		if err != nil {
			return "", "", fmt.Errorf("soft-delete plugin server twin: %w", err)
		}
		if deleted != 1 {
			return "", "", fmt.Errorf("soft-delete plugin server twin: %d rows updated", deleted)
		}
		if err := r.audit.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
			OrganizationID: m.toolset.OrganizationID, ProjectID: m.toolset.ProjectID,
			Actor: m.actor, ActorDisplayName: nil, ActorSlug: nil,
			PluginID: member.PluginID, PluginName: member.PluginName, PluginSlug: member.PluginSlug,
			ServerID: twin.ID, ToolsetURN: nil, McpServerURN: &serverURN, MetaMcpServerURN: nil,
		}); err != nil {
			return "", "", fmt.Errorf("audit plugin server twin removal: %w", err)
		}
		m.twin = &RowImage{Table: tablePluginServers, ID: twin.ID, Deletion: "soft_deleted", Row: twin.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""}
	}
	moved, err := q.RekeyPluginServer(ctx, RekeyPluginServerParams{McpServerID: serverKey, ID: m.rowID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("re-key plugin server: %w", err)
	}
	if moved != 1 {
		return "", "", fmt.Errorf("re-key plugin server: %d rows updated", moved)
	}
	if err := r.audit.LogPluginServerUpdate(ctx, tx, audit.LogPluginServerUpdateEvent{
		OrganizationID: m.toolset.OrganizationID, ProjectID: m.toolset.ProjectID,
		Actor: m.actor, ActorDisplayName: nil, ActorSlug: nil,
		PluginID: member.PluginID, PluginName: member.PluginName, PluginSlug: member.PluginSlug,
		ServerID: member.ID, ServerDisplayName: member.DisplayName, ServerPolicy: member.Policy, ServerSortOrder: member.SortOrder,
		McpServerURN: &serverURN, PreviousToolsetURN: &toolsetURN,
	}); err != nil {
		return "", "", fmt.Errorf("audit plugin server move: %w", err)
	}
	return "", "", nil
}

// moveAssistantAttachment moves the attachment to assistant_mcp_servers under the same id; attachment changes are not audited on any path.
func moveAssistantAttachment(ctx context.Context, tx pgx.Tx, m *dependentMove) (Outcome, string, error) {
	q := New(tx)
	owner, err := q.GetAssistantToolsetOwner(ctx, GetAssistantToolsetOwnerParams{ID: m.rowID, ProjectID: m.toolset.ProjectID, ToolsetID: m.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, "row changed concurrently; rerun", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("load assistant toolset: %w", err)
	}
	_, err = q.LockAssistantForMove(ctx, LockAssistantForMoveParams{ID: owner, ProjectID: m.toolset.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeOrphaned, "assistant is not in the row's project", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock assistant: %w", err)
	}
	attachment, err := q.LockAssistantToolset(ctx, LockAssistantToolsetParams{ID: m.rowID, ProjectID: m.toolset.ProjectID, ToolsetID: m.toolset.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return OutcomeSkipped, "row changed concurrently; rerun", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("lock assistant toolset: %w", err)
	}
	if outcome, reason, err := m.loadWrapper(ctx, q); outcome != "" || err != nil {
		return outcome, reason, err
	}
	// The assistants reader dials a hosted attachment only when its toolset has MCP on and an mcp slug.
	if !m.toolset.McpEnabled || !m.toolset.McpSlug.Valid || m.toolset.McpSlug.String == "" {
		return OutcomeBlockedUnattachable, "toolset MCP is off or has no mcp slug, so the assistant does not dial it", nil
	}
	twin, err := q.LockAssistantMcpServerTwin(ctx, LockAssistantMcpServerTwinParams{AssistantID: attachment.AssistantID, McpServerID: m.wrapper.ID, ProjectID: m.toolset.ProjectID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", "", fmt.Errorf("lock assistant twin: %w", err)
	default:
		// The toolset-keyed attachment wins, keeping its environment, as phase A readers prefer it.
		deleted, err := q.DeleteAssistantMcpServerTwin(ctx, DeleteAssistantMcpServerTwinParams{ID: twin.ID, ProjectID: m.toolset.ProjectID, McpServerID: m.wrapper.ID})
		if err != nil {
			return "", "", fmt.Errorf("delete assistant twin: %w", err)
		}
		if deleted != 1 {
			return "", "", fmt.Errorf("delete assistant twin: %d rows deleted", deleted)
		}
		m.twin = &RowImage{Table: tableAssistantMcpServers, ID: twin.ID, Deletion: "deleted", Row: twin.RowJson, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""}
	}
	moved, err := q.MoveAssistantToolset(ctx, MoveAssistantToolsetParams{McpServerID: m.wrapper.ID, ID: m.rowID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("move assistant toolset: %w", err)
	}
	if moved != 1 {
		return "", "", fmt.Errorf("move assistant toolset: %d rows moved", moved)
	}
	return "", "", nil
}
