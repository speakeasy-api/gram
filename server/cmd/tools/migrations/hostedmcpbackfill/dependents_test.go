package hostedmcpbackfill

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type dependentRows struct {
	environmentID uuid.UUID
	metadataID    uuid.UUID
	envConfigID   uuid.UUID
	pluginID      uuid.UUID
	memberID      uuid.UUID
	assistantID   uuid.UUID
	attachmentID  uuid.UUID
}

type dependentState struct {
	metadata   []ListMetadataFixtureRow
	envConfigs []ListEnvironmentConfigsFixtureRow
	members    []ListPluginServersFixtureRow
	toolsets   []ListAssistantToolsetsFixtureRow
	servers    []ListAssistantMcpServersFixtureRow
}

type memJournal struct {
	mu      sync.Mutex
	records []JournalRecord
	failOn  JournalEvent
}

func (j *memJournal) Append(record JournalRecord) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failOn != "" && record.Event == j.failOn {
		return errors.New("journal unavailable")
	}
	j.records = append(j.records, record)
	return nil
}

func (j *memJournal) committed() map[uuid.UUID]JournalRecord {
	out := map[uuid.UUID]JournalRecord{}
	for _, record := range j.records {
		if record.Event == JournalCommitted {
			out[record.RowID] = record
		}
	}
	return out
}

// seedWrappedToolset seeds a hosted toolset with its canonical wrapper.
func (f *fixture) seedWrappedToolset(t *testing.T, mcpSlug string) uuid.UUID {
	t.Helper()
	id := f.seedToolset(t, toolsetSpec{mcpSlug: mcpSlug, public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, id, id, "Hosted "+mcpSlug, mcpSlug, "public")
	f.seedEndpoint(t, id, uuid.NullUUID{}, mcpSlug)
	return id
}

func (f *fixture) seedEnvironment(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedEnvironmentFixture(t.Context(), SeedEnvironmentFixtureParams{
		ID: id, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "env", Slug: "env-" + uuid.NewString()[:8],
	}))
	return id
}

// seedDependents seeds one toolset-keyed row in each dependent table, bound to an environment, plus an env config on the metadata.
func (f *fixture) seedDependents(t *testing.T, toolsetID uuid.UUID) dependentRows {
	t.Helper()
	q := New(f.pool)
	ctx := t.Context()
	rows := dependentRows{
		environmentID: f.seedEnvironment(t), metadataID: uuid.New(), envConfigID: uuid.New(), pluginID: uuid.New(), memberID: uuid.New(),
		assistantID: uuid.New(), attachmentID: uuid.New(),
	}
	key := uuid.NullUUID{UUID: toolsetID, Valid: true}
	env := uuid.NullUUID{UUID: rows.environmentID, Valid: true}
	require.NoError(t, q.SeedMetadataFixture(ctx, SeedMetadataFixtureParams{ID: rows.metadataID, ToolsetID: key, McpServerID: uuid.NullUUID{}, ProjectID: f.projectID, DefaultEnvironmentID: env, Instructions: conv.ToPGText("legacy")}))
	require.NoError(t, q.SeedEnvironmentConfigFixture(ctx, SeedEnvironmentConfigFixtureParams{ID: rows.envConfigID, ProjectID: f.projectID, McpMetadataID: rows.metadataID, VariableName: "API_KEY"}))
	require.NoError(t, q.SeedPluginFixture(ctx, SeedPluginFixtureParams{ID: rows.pluginID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "plugin", Slug: "plugin-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedPluginServerFixture(ctx, SeedPluginServerFixtureParams{ID: rows.memberID, PluginID: rows.pluginID, ToolsetID: key, McpServerID: uuid.NullUUID{}, DisplayName: "member"}))
	require.NoError(t, q.SeedAssistantFixture(ctx, SeedAssistantFixtureParams{ID: rows.assistantID, ProjectID: f.projectID, OrganizationID: f.orgID, Name: "assistant-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedAssistantToolsetFixture(ctx, SeedAssistantToolsetFixtureParams{ID: rows.attachmentID, AssistantID: rows.assistantID, ToolsetID: toolsetID, ProjectID: f.projectID, EnvironmentID: env}))
	return rows
}

func (f *fixture) dependentState(t *testing.T) dependentState {
	t.Helper()
	q := New(f.pool)
	ctx := t.Context()
	metadata, err := q.ListMetadataFixture(ctx, f.projectID)
	require.NoError(t, err)
	envConfigs, err := q.ListEnvironmentConfigsFixture(ctx, f.projectID)
	require.NoError(t, err)
	members, err := q.ListPluginServersFixture(ctx, f.projectID)
	require.NoError(t, err)
	toolsets, err := q.ListAssistantToolsetsFixture(ctx, f.projectID)
	require.NoError(t, err)
	servers, err := q.ListAssistantMcpServersFixture(ctx, f.projectID)
	require.NoError(t, err)
	return dependentState{metadata: metadata, envConfigs: envConfigs, members: members, toolsets: toolsets, servers: servers}
}

func dependentRowFor(t *testing.T, report Report, rowID uuid.UUID) RowReport {
	t.Helper()
	for _, row := range report.Rows {
		if row.RowID != nil && *row.RowID == rowID {
			return row
		}
	}
	require.FailNow(t, "dependent row not found", rowID.String())
	return RowReport{}
}

func moveOptions(apply bool) (Options, *memJournal) {
	journal := &memJournal{}
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, MoveDependents: true, Journal: journal}, journal
}

func TestMoveDependentsDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "dry-move")
	f.seedDependents(t, toolsetID)
	before, beforeAudit := f.dependentState(t), f.state(t)
	opts, journal := moveOptions(false)

	report := f.run(t, opts)

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseDependents, report.Phase)
	require.Equal(t, 3, report.Outcomes[OutcomeWouldMove])
	require.Equal(t, 3, report.Writes)
	require.Equal(t, before, f.dependentState(t))
	require.Equal(t, beforeAudit, f.state(t))
	require.Empty(t, journal.records)
}

func TestMoveDependentsApplyRequiresJournal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedDependents(t, f.seedWrappedToolset(t, "no-journal"))
	before := f.dependentState(t)
	opts, _ := moveOptions(true)
	opts.Journal = nil

	_, err := NewRunner(f.pool, opts).Run(t.Context())
	require.ErrorContains(t, err, "journal")
	require.Equal(t, before, f.dependentState(t))
}

func TestMoveDependentsRekeysInPlaceAndIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "move-me")
	rows := f.seedDependents(t, toolsetID)
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}
	env := uuid.NullUUID{UUID: rows.environmentID, Valid: true}
	opts, journal := moveOptions(true)

	report := f.run(t, opts)
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 3, report.Outcomes[OutcomeMoved])
	require.Equal(t, toolsetID, report.LastCursor)
	for _, id := range []uuid.UUID{rows.metadataID, rows.memberID, rows.attachmentID} {
		row := dependentRowFor(t, report, id)
		require.Equal(t, "move-me", row.McpSlug)
		require.Nil(t, row.CollapsedTwin)
	}

	state := f.dependentState(t)
	require.Equal(t, []ListMetadataFixtureRow{{ID: rows.metadataID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, DefaultEnvironmentID: env, Instructions: conv.ToPGText("legacy")}}, state.metadata)
	require.Equal(t, []ListEnvironmentConfigsFixtureRow{{ID: rows.envConfigID, McpMetadataID: rows.metadataID, VariableName: "API_KEY"}}, state.envConfigs)
	require.Equal(t, []ListPluginServersFixtureRow{{ID: rows.memberID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, Deleted: false}}, state.members)
	require.Empty(t, state.toolsets)
	require.Equal(t, []ListAssistantMcpServersFixtureRow{{ID: rows.attachmentID, AssistantID: rows.assistantID, McpServerID: toolsetID, EnvironmentID: env}}, state.servers)

	require.Len(t, journal.records, 6)
	committed := journal.committed()
	require.Len(t, committed, 3)
	require.Equal(t, RowKey{Table: tableAssistantToolsets, Column: "toolset_id", ID: toolsetID}, committed[rows.attachmentID].From)
	require.Equal(t, RowKey{Table: tableAssistantMcpServers, Column: "mcp_server_id", ID: toolsetID}, committed[rows.attachmentID].To)
	require.Equal(t, RowKey{Table: tableMcpMetadata, Column: "mcp_server_id", ID: toolsetID}, committed[rows.metadataID].To)
	require.Equal(t, f.projectID, committed[rows.memberID].ProjectID)
	for i := 0; i < len(journal.records); i += 2 {
		require.Equal(t, JournalIntent, journal.records[i].Event, "every commit is preceded by its intent")
		require.Equal(t, journal.records[i].RowID, journal.records[i+1].RowID)
	}

	audits := f.state(t).audits
	require.ElementsMatch(t, []ListAuditActorsFixtureRow{
		{ActorID: DependentsActorComponent, ActorType: string(urn.PrincipalTypeSystem), Action: string(audit.ActionMCPMetadataUpdate)},
		{ActorID: DependentsActorComponent, ActorType: string(urn.PrincipalTypeSystem), Action: string(audit.ActionPluginServerUpdate)},
	}, audits)
	snapshots, err := New(f.pool).ListAuditSnapshotsFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	for _, snapshot := range snapshots {
		switch snapshot.Action {
		case string(audit.ActionMCPMetadataUpdate):
			var before, after map[string]any
			require.NoError(t, json.Unmarshal(snapshot.BeforeSnapshot, &before))
			require.NoError(t, json.Unmarshal(snapshot.AfterSnapshot, &after))
			require.Equal(t, rows.metadataID.String(), before["ID"])
			require.Equal(t, toolsetID.String(), before["ToolsetID"])
			require.Nil(t, before["McpServerID"])
			require.Nil(t, after["ToolsetID"])
			require.Equal(t, toolsetID.String(), after["McpServerID"])
			require.Equal(t, rows.environmentID.String(), after["DefaultEnvironmentID"])
			require.Len(t, after["EnvironmentConfigs"], 1)
			require.Contains(t, string(snapshot.AfterSnapshot), `"API_KEY"`)
		case string(audit.ActionPluginServerUpdate):
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(snapshot.Metadata, &metadata))
			require.Equal(t, rows.memberID.String(), metadata["server_id"])
			require.Equal(t, urn.NewMcpServer(toolsetID).String(), metadata["mcp_server_urn"])
			require.Equal(t, urn.NewToolset(toolsetID).String(), metadata["previous_toolset_urn"])
		}
	}
	outbox, err := New(f.pool).CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox, "backfill audit rows must not publish webhooks")

	rerunOpts, _ := moveOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Scanned)
	require.Zero(t, rerun.Writes)
	require.Equal(t, state, f.dependentState(t))
}

type twinRows struct {
	environmentID uuid.UUID
	metadataID    uuid.UUID
	envConfigID   uuid.UUID
	memberID      uuid.UUID
	attachmentID  uuid.UUID
}

// seedTwins seeds a server-keyed twin of each dependent row, bound to another environment.
func (f *fixture) seedTwins(t *testing.T, toolsetID uuid.UUID, rows dependentRows) twinRows {
	t.Helper()
	q := New(f.pool)
	ctx := t.Context()
	twins := twinRows{environmentID: f.seedEnvironment(t), metadataID: uuid.New(), envConfigID: uuid.New(), memberID: uuid.New(), attachmentID: uuid.New()}
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}
	env := uuid.NullUUID{UUID: twins.environmentID, Valid: true}
	require.NoError(t, q.SeedMetadataFixture(ctx, SeedMetadataFixtureParams{ID: twins.metadataID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, ProjectID: f.projectID, DefaultEnvironmentID: env, Instructions: conv.ToPGText("twin")}))
	require.NoError(t, q.SeedEnvironmentConfigFixture(ctx, SeedEnvironmentConfigFixtureParams{ID: twins.envConfigID, ProjectID: f.projectID, McpMetadataID: twins.metadataID, VariableName: "TWIN_KEY"}))
	require.NoError(t, q.SeedPluginServerFixture(ctx, SeedPluginServerFixtureParams{ID: twins.memberID, PluginID: rows.pluginID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, DisplayName: "twin"}))
	require.NoError(t, q.SeedAssistantMcpServerFixture(ctx, SeedAssistantMcpServerFixtureParams{ID: twins.attachmentID, AssistantID: rows.assistantID, McpServerID: toolsetID, ProjectID: f.projectID}))
	return twins
}

func TestMoveDependentsCollapsesTwinsKeepingTheToolsetKeyedRow(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "twinned")
	rows := f.seedDependents(t, toolsetID)
	twins := f.seedTwins(t, toolsetID, rows)
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}
	env := uuid.NullUUID{UUID: rows.environmentID, Valid: true}
	before := f.dependentState(t)

	dryOpts, _ := moveOptions(false)
	dry := f.run(t, dryOpts)
	require.Equal(t, 3, dry.Outcomes[OutcomeWouldMoveCollapsingTwin])
	require.Equal(t, before, f.dependentState(t))

	opts, journal := moveOptions(true)
	report := f.run(t, opts)

	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 3, report.Outcomes[OutcomeMovedCollapsedTwin])
	require.Equal(t, 3, report.Writes)
	metadataRow := dependentRowFor(t, report, rows.metadataID)
	require.Equal(t, tableMcpMetadata, metadataRow.CollapsedTwin.Table)
	require.Equal(t, twins.metadataID, metadataRow.CollapsedTwin.ID)
	require.Equal(t, "deleted", metadataRow.CollapsedTwin.Deletion)
	require.Contains(t, string(metadataRow.CollapsedTwin.Row), `"instructions": "twin"`)
	require.Contains(t, string(metadataRow.CollapsedTwin.EnvironmentConfigs), twins.envConfigID.String())
	memberRow := dependentRowFor(t, report, rows.memberID)
	require.Equal(t, twins.memberID, memberRow.CollapsedTwin.ID)
	require.Equal(t, "soft_deleted", memberRow.CollapsedTwin.Deletion)
	attachmentRow := dependentRowFor(t, report, rows.attachmentID)
	require.Equal(t, tableAssistantMcpServers, attachmentRow.CollapsedTwin.Table)
	require.Equal(t, twins.attachmentID, attachmentRow.CollapsedTwin.ID)

	state := f.dependentState(t)
	require.Equal(t, []ListMetadataFixtureRow{{ID: rows.metadataID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, DefaultEnvironmentID: env, Instructions: conv.ToPGText("legacy")}}, state.metadata)
	require.Equal(t, []ListEnvironmentConfigsFixtureRow{{ID: rows.envConfigID, McpMetadataID: rows.metadataID, VariableName: "API_KEY"}}, state.envConfigs)
	require.ElementsMatch(t, []ListPluginServersFixtureRow{
		{ID: rows.memberID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, Deleted: false},
		{ID: twins.memberID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, Deleted: true},
	}, state.members)
	require.Empty(t, state.toolsets)
	require.Equal(t, []ListAssistantMcpServersFixtureRow{{ID: rows.attachmentID, AssistantID: rows.assistantID, McpServerID: toolsetID, EnvironmentID: env}}, state.servers)

	require.ElementsMatch(t, []string{string(audit.ActionMCPMetadataUpdate), string(audit.ActionPluginServerRemove), string(audit.ActionPluginServerUpdate)}, auditActions(f.state(t).audits))

	committed := journal.committed()
	require.Equal(t, metadataRow.CollapsedTwin, committed[rows.metadataID].CollapsedTwin)
	require.Equal(t, attachmentRow.CollapsedTwin, committed[rows.attachmentID].CollapsedTwin)

	// The runbook's metadata rollback, from journal values, restores both rows and the twin's env configs; replaying it is a no-op.
	q := New(f.pool)
	for range 2 {
		_, err := q.RollbackMetadataMoveFixture(t.Context(), RollbackMetadataMoveFixtureParams{ToolsetID: toolsetID, ID: rows.metadataID, ProjectID: f.projectID})
		require.NoError(t, err)
		twin := committed[rows.metadataID].CollapsedTwin
		require.NoError(t, q.RestoreMetadataTwinFixture(t.Context(), RestoreMetadataTwinFixtureParams{RowJson: twin.Row, ProjectID: f.projectID}))
		require.NoError(t, q.RestoreEnvironmentConfigsFixture(t.Context(), RestoreEnvironmentConfigsFixtureParams{RowsJson: twin.EnvironmentConfigs, ProjectID: f.projectID}))
	}
	restored := f.dependentState(t)
	require.Equal(t, before.metadata, restored.metadata)
	require.Equal(t, before.envConfigs, restored.envConfigs)
}

func auditActions(rows []ListAuditActorsFixtureRow) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Action)
	}
	return out
}

func TestMoveDependentsBlocksPluginTwinReferencedByDistribution(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "distributed-twin")
	rows := f.seedDependents(t, toolsetID)
	twins := f.seedTwins(t, toolsetID, rows)
	require.NoError(t, New(f.pool).SeedDistributionReferenceFixture(t.Context(), SeedDistributionReferenceFixtureParams{
		OrganizationID: f.orgID, ProjectID: f.projectID, PluginID: rows.pluginID, PluginServerID: uuid.NullUUID{UUID: twins.memberID, Valid: true},
	}))
	before := f.dependentState(t)
	opts, _ := moveOptions(true)

	report := f.run(t, opts)

	member := dependentRowFor(t, report, rows.memberID)
	require.Equal(t, OutcomeBlockedTwin, member.Outcome)
	require.Contains(t, member.Reason, "platform distribution")
	require.Equal(t, before.members, f.dependentState(t).members)
}

func TestMoveDependentsBlocksAssistantAttachmentsTheReaderDoesNotDial(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	mcpOff := f.seedToolset(t, toolsetSpec{mcpSlug: "mcp-off", public: true, enabled: false, domainID: uuid.NullUUID{}})
	f.seedServer(t, mcpOff, mcpOff, "mcp off", "mcp-off", "disabled")
	f.seedEndpoint(t, mcpOff, uuid.NullUUID{}, "mcp-off")
	offRows := f.seedDependents(t, mcpOff)
	domain := f.seedDomain(t, false)
	domainOnly := f.seedToolset(t, toolsetSpec{mcpSlug: "domain-only", public: true, enabled: true, domainID: domain})
	f.seedServer(t, domainOnly, domainOnly, "domain only", "domain-only", "public")
	f.seedEndpoint(t, domainOnly, domain, "domain-only")
	domainRows := f.seedDependents(t, domainOnly)
	noEndpoint := f.seedToolset(t, toolsetSpec{mcpSlug: "endpoint-gone", public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, noEndpoint, noEndpoint, "endpoint gone", "endpoint-gone", "public")
	endpointID := f.seedEndpoint(t, noEndpoint, uuid.NullUUID{}, "endpoint-gone")
	require.NoError(t, New(f.pool).SoftDeleteEndpointFixture(t.Context(), SoftDeleteEndpointFixtureParams{ID: endpointID, ProjectID: f.projectID}))
	noEndpointRows := f.seedDependents(t, noEndpoint)
	opts, _ := moveOptions(true)

	report := f.run(t, opts)

	require.Equal(t, OutcomeBlockedUnattachable, dependentRowFor(t, report, offRows.attachmentID).Outcome)
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, offRows.metadataID).Outcome)
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, domainRows.attachmentID).Outcome, "the reader dials custom-domain-only toolsets by mcp slug")
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, noEndpointRows.attachmentID).Outcome, "the reader ignores wrapper endpoint drift")
	state := f.dependentState(t)
	require.Len(t, state.toolsets, 1)
	require.Equal(t, offRows.attachmentID, state.toolsets[0].ID)
	require.Len(t, state.servers, 2)
}

func TestMoveDependentsRequiresTheLiveCanonicalWrapper(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	freshOnly := f.seedToolset(t, toolsetSpec{mcpSlug: "fresh-only", public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, uuid.New(), freshOnly, "fresh", "fresh-only", "public")
	freshRows := f.seedDependents(t, freshOnly)
	tombstoned := f.seedToolset(t, toolsetSpec{mcpSlug: "tombstoned", public: true, enabled: true, domainID: uuid.NullUUID{}})
	require.NoError(t, q.SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: tombstoned, ProjectID: f.projectID, Name: conv.ToPGText("tombstoned"), Slug: conv.ToPGText("tombstoned"),
		ToolsetID: uuid.NullUUID{UUID: tombstoned, Valid: true}, Visibility: "public", NetworkAccessMode: pgtype.Text{},
		DeletedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	tombstonedRows := f.seedDependents(t, tombstoned)
	before := f.dependentState(t)
	opts, journal := moveOptions(true)

	report := f.run(t, opts)

	require.Equal(t, 6, report.Outcomes[OutcomeBlockedNoWrapper])
	require.Zero(t, report.Writes)
	require.Equal(t, OutcomeBlockedNoWrapper, dependentRowFor(t, report, freshRows.metadataID).Outcome)
	require.Equal(t, OutcomeBlockedNoWrapper, dependentRowFor(t, report, tombstonedRows.attachmentID).Outcome)
	require.Equal(t, before, f.dependentState(t))
	require.Empty(t, journal.records)
}

func TestMoveDependentsReportsRowsWithoutLiveWrapper(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	deletedToolset := f.seedWrappedToolset(t, "deleted-toolset")
	deletedRows := f.seedDependents(t, deletedToolset)
	require.NoError(t, q.SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: deletedToolset, ProjectID: f.projectID}))
	unwrapped := f.seedToolset(t, toolsetSpec{mcpSlug: "no-wrapper", public: true, enabled: true, domainID: uuid.NullUUID{}})
	unwrappedRows := f.seedDependents(t, unwrapped)

	deletedProject := newFixtureIn(t, f.pool)
	inDeletedProject := deletedProject.seedWrappedToolset(t, "in-deleted-project")
	deletedProjectRows := deletedProject.seedDependents(t, inDeletedProject)
	require.NoError(t, q.SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: deletedProject.projectID, OrganizationID: deletedProject.orgID}))

	before, beforeDeletedProject := f.dependentState(t), deletedProject.dependentState(t)
	opts, _ := moveOptions(true)
	report := f.run(t, opts)

	require.Zero(t, report.Writes)
	require.Equal(t, 6, report.Outcomes[OutcomeOrphaned])
	require.Equal(t, 3, report.Outcomes[OutcomeBlockedNoWrapper])
	require.Equal(t, "toolset deleted", dependentRowFor(t, report, deletedRows.memberID).Reason)
	require.Equal(t, "project deleted", dependentRowFor(t, report, deletedProjectRows.attachmentID).Reason)
	require.Equal(t, OutcomeBlockedNoWrapper, dependentRowFor(t, report, unwrappedRows.metadataID).Outcome)
	require.Equal(t, before, f.dependentState(t))
	require.Equal(t, beforeDeletedProject, deletedProject.dependentState(t))
}

func TestMoveDependentsLeavesRowsPointingAtAnotherProjectsToolset(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	foreign := f.seedWrappedToolset(t, "foreign-"+uuid.NewString()[:8])
	crossRows := other.seedDependents(t, foreign)
	otherBefore := other.dependentState(t)

	for _, project := range []uuid.NullUUID{{}, {UUID: other.projectID, Valid: true}} {
		opts, _ := moveOptions(true)
		opts.ProjectID = project
		report := f.run(t, opts)
		for _, id := range []uuid.UUID{crossRows.metadataID, crossRows.memberID, crossRows.attachmentID} {
			row := dependentRowFor(t, report, id)
			require.Equal(t, OutcomeBlockedNoWrapper, row.Outcome)
			require.Equal(t, "toolset is not in the row's project", row.Reason)
		}
		require.Zero(t, report.Writes)
	}
	require.Equal(t, otherBefore, other.dependentState(t))
}

func TestMoveDependentsProjectFilterCursorAndLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	a := f.seedWrappedToolset(t, "cursor-a-"+uuid.NewString()[:8])
	b := f.seedWrappedToolset(t, "cursor-b-"+uuid.NewString()[:8])
	f.seedDependents(t, a)
	f.seedDependents(t, b)
	elsewhere := other.seedWrappedToolset(t, "cursor-elsewhere-"+uuid.NewString()[:8])
	other.seedDependents(t, elsewhere)
	otherBefore := other.dependentState(t)
	opts, _ := moveOptions(true)
	opts.ProjectID = uuid.NullUUID{UUID: f.projectID, Valid: true}
	opts.Limit = 1

	first := f.run(t, opts)
	require.Equal(t, 3, first.Scanned)
	require.Equal(t, 1, first.Toolsets)
	opts.Cursor = first.LastCursor
	opts.Limit = 0
	second := f.run(t, opts)
	require.Equal(t, 3, second.Scanned)
	require.ElementsMatch(t, []uuid.UUID{a, b}, []uuid.UUID{first.LastCursor, second.LastCursor})
	require.Equal(t, otherBefore, other.dependentState(t))
}

func TestMoveDependentsPagesWithASmallPageSize(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for i := range 3 {
		f.seedDependents(t, f.seedWrappedToolset(t, "paged-"+string(rune('a'+i))+"-"+uuid.NewString()[:8]))
	}
	opts, journal := moveOptions(true)
	opts.PageSize = 1

	report := f.run(t, opts)

	require.Equal(t, 3, report.Toolsets)
	require.Equal(t, 9, report.Outcomes[OutcomeMoved])
	require.Len(t, journal.committed(), 9)
	state := f.dependentState(t)
	require.Empty(t, state.toolsets)
	require.Len(t, state.servers, 3)
}

func TestWrapperPhaseLeavesDependentsToolsetKeyed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedToolset(t, toolsetSpec{mcpSlug: "wrapper-only", public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedDependents(t, toolsetID)
	before := f.dependentState(t)

	report := f.run(t, Options{Apply: true})

	require.Equal(t, phaseWrappers, report.Phase)
	require.Equal(t, OutcomeCreated, rowFor(t, report, toolsetID).Outcome)
	require.Equal(t, before, f.dependentState(t))
	outbox, err := New(f.pool).CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox)
}

func TestMoveDependentsStopsOnLockTimeoutKeepingEarlierCommitsJournaled(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "locked")
	rows := f.seedDependents(t, toolsetID)
	before := f.dependentState(t)

	holder, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the row lock uses a generated SQLc method.
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	_, err = New(holder).LockToolsetKeyedMetadata(t.Context(), LockToolsetKeyedMetadataParams{ID: rows.metadataID, ProjectID: f.projectID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}})
	require.NoError(t, err)

	opts, journal := moveOptions(true)
	opts.LockTimeout = 200 * time.Millisecond
	report, err := NewRunner(f.pool, opts).Run(t.Context())
	require.Error(t, err)
	require.True(t, isTransient(err), "lock timeout must stop the run: %v", err)
	require.Equal(t, uuid.Nil, report.LastCursor, "the toolset is not done, so the cursor must not pass it")

	// Dependents run in table order, so the assistant attachment committed before the metadata lock timed out.
	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, rows.attachmentID).Outcome)
	committed := journal.committed()
	require.Len(t, committed, 1)
	require.Contains(t, committed, rows.attachmentID)
	for _, record := range journal.records {
		require.NotEqual(t, rows.metadataID, record.RowID, "a row that never reached commit is not journaled")
	}

	require.NoError(t, holder.Rollback(t.Context()))
	after := f.dependentState(t)
	require.Equal(t, before.metadata, after.metadata)
	require.Equal(t, before.members, after.members)
	require.Empty(t, after.toolsets)
}

func TestMoveDependentsJournalFailureBeforeCommitWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedDependents(t, f.seedWrappedToolset(t, "journal-down"))
	before := f.dependentState(t)
	opts, journal := moveOptions(true)
	journal.failOn = JournalIntent

	report, err := NewRunner(f.pool, opts).Run(t.Context())

	require.ErrorContains(t, err, "journal intent")
	require.Zero(t, report.Writes)
	require.Equal(t, before, f.dependentState(t))
}

func TestTwinViolationMapsOnlyTheTablesTwinConstraint(t *testing.T) {
	t.Parallel()
	violation := func(constraint string) error {
		return &pgconn.PgError{Code: pgerrcode.UniqueViolation, ConstraintName: constraint}
	}

	for table, constraint := range twinConstraints {
		got, ok := twinViolation(table, errors.Join(errors.New("wrapped"), violation(constraint)))
		require.True(t, ok, table)
		require.Equal(t, constraint, got)
	}
	_, ok := twinViolation(tableMcpMetadata, violation("plugin_servers_plugin_id_mcp_server_id_key"))
	require.False(t, ok, "another table's twin constraint stops the run")
	_, ok = twinViolation(tablePluginServers, violation("plugin_servers_plugin_id_display_name_key"))
	require.False(t, ok, "a display-name collision stops the run")
	_, ok = twinViolation(tableAssistantToolsets, &pgconn.PgError{Code: pgerrcode.ForeignKeyViolation, ConstraintName: "assistant_mcp_servers_assistant_id_mcp_server_id_key"})
	require.False(t, ok)
	_, ok = twinViolation(tableAssistantToolsets, nil)
	require.False(t, ok)
}
