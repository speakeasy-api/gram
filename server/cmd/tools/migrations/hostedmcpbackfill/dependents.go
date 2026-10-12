package hostedmcpbackfill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// DependentsActorComponent is the system principal (system:<DependentsActorComponent>) the dependent move audits as.
const DependentsActorComponent = "hosted-mcp-dependent-move"

const (
	tableMcpMetadata       = "mcp_metadata"
	tablePluginServers     = "plugin_servers"
	tableAssistantToolsets = "assistant_toolsets"
)

// runDependents walks toolsets that still have toolset-keyed dependents; Limit and the cursor count toolsets.
func (r *Runner) runDependents(ctx context.Context, report Report) (Report, error) {
	after := r.options.Cursor
	for {
		page, err := New(r.pool).ListDependentToolsets(ctx, ListDependentToolsetsParams{
			ProjectID: r.options.ProjectID,
			AfterID:   after,
			PageSize:  conv.SafeInt32(r.options.PageSize),
		})
		if err != nil {
			return report, fmt.Errorf("list dependent toolsets: %w", err)
		}
		if len(page) == 0 {
			return report, nil
		}
		for _, toolsetID := range page {
			if r.options.Limit > 0 && report.Toolsets >= r.options.Limit {
				return report, nil
			}
			deps, err := New(r.pool).ListToolsetDependents(ctx, ListToolsetDependentsParams{ToolsetID: toolsetID, ProjectID: r.options.ProjectID})
			if err != nil {
				return report, fmt.Errorf("toolset %s: list dependents: %w", toolsetID, err)
			}
			for _, dep := range deps {
				row, err := r.moveOne(ctx, toolsetID, dep)
				if err != nil {
					return report, fmt.Errorf("toolset %s: %s %s: %w", toolsetID, dep.TableName, dep.ID, err)
				}
				report.Scanned++
				report.Outcomes[row.Outcome]++
				if row.Wrote {
					report.Writes++
				}
				report.Rows = append(report.Rows, row)
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
		Outcome: "", Reason: "", Wrote: false, LiveEndpoints: 0, FreshIDServers: 0, DomainsToReconcile: nil,
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return row, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	q := New(tx)
	if err := q.LockToolsetBackfill(ctx, toolsetID.String()); err != nil {
		return row, fmt.Errorf("advisory lock: %w", err)
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

	move := &dependentMove{toolset: toolset, wrapper: nil, rowID: dep.ID, actor: urn.NewSystemPrincipal(DependentsActorComponent)}
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return blocked(row, OutcomeBlockedTwin, fmt.Sprintf("server-keyed twin exists (constraint %s)", pgErr.ConstraintName)), nil
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

	row.Wrote = true
	row.Outcome = conv.Ternary(r.options.Apply, OutcomeMoved, OutcomeWouldMove)
	if !r.options.Apply {
		return row, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return row, fmt.Errorf("commit: %w", err)
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
	twin, err := q.ServerKeyedMetadataExists(ctx, ServerKeyedMetadataExistsParams{McpServerID: uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("check metadata twin: %w", err)
	}
	if twin {
		return OutcomeBlockedTwin, "server-keyed mcp_metadata already exists for the wrapper", nil
	}
	after, err := q.RekeyMetadata(ctx, RekeyMetadataParams{McpServerID: uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}, ID: m.rowID, ProjectID: m.toolset.ProjectID})
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
	twin, err := q.ServerKeyedPluginServerExists(ctx, ServerKeyedPluginServerExistsParams{PluginID: member.PluginID, ProjectID: m.toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}})
	if err != nil {
		return "", "", fmt.Errorf("check plugin server twin: %w", err)
	}
	if twin {
		return OutcomeBlockedTwin, "plugin already has a server-keyed member for the wrapper", nil
	}
	moved, err := q.RekeyPluginServer(ctx, RekeyPluginServerParams{McpServerID: uuid.NullUUID{UUID: m.wrapper.ID, Valid: true}, ID: m.rowID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("re-key plugin server: %w", err)
	}
	if moved != 1 {
		return "", "", fmt.Errorf("re-key plugin server: %d rows updated", moved)
	}
	serverURN, toolsetURN := urn.NewMcpServer(m.wrapper.ID), urn.NewToolset(m.toolset.ID)
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
	if err := q.LockAssistantForMove(ctx, LockAssistantForMoveParams{ID: owner, ProjectID: m.toolset.ProjectID}); err != nil {
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
	// The assistant write path rejects these servers, so a moved attachment would make the assistant unsaveable.
	switch {
	case m.wrapper.Visibility == visibility.Disabled:
		return OutcomeBlockedUnattachable, "wrapper is disabled", nil
	case !m.wrapper.HasPlatformEndpoint:
		return OutcomeBlockedUnattachable, "wrapper has no Speakeasy-hosted endpoint", nil
	}
	twin, err := q.AssistantMcpServerExists(ctx, AssistantMcpServerExistsParams{AssistantID: attachment.AssistantID, McpServerID: m.wrapper.ID, ProjectID: m.toolset.ProjectID})
	if err != nil {
		return "", "", fmt.Errorf("check assistant twin: %w", err)
	}
	if twin {
		return OutcomeBlockedTwin, "assistant already attaches the wrapper through assistant_mcp_servers", nil
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
