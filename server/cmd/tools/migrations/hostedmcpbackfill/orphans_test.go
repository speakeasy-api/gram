package hostedmcpbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func cleanupOptions(apply bool) (Options, *memJournal) {
	journal := &memJournal{}
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, CleanupOrphans: true, Journal: journal}, journal
}

func (f *fixture) seedOrphans(t *testing.T, mcpSlug string) (uuid.UUID, dependentRows) {
	t.Helper()
	toolsetID := f.seedWrappedToolset(t, mcpSlug)
	rows := f.seedDependents(t, toolsetID)
	require.NoError(t, New(f.pool).SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: toolsetID, ProjectID: f.projectID}))
	return toolsetID, rows
}

func TestCleanupOrphansDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, rows := f.seedOrphans(t, "dry-orphan")
	before, beforeAudit := f.dependentState(t), f.state(t)
	opts, journal := cleanupOptions(false)

	report := f.run(t, opts)

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseOrphans, report.Phase)
	require.Equal(t, 3, report.Outcomes[OutcomeWouldDelete])
	require.Equal(t, 3, report.Writes)
	member := dependentRowFor(t, report, rows.memberID)
	require.Equal(t, "toolset deleted", member.Reason)
	require.Equal(t, "soft_deleted", member.Removed.Deletion)
	require.Nil(t, member.Removed.Row, "a dry-run report carries no row snapshot")
	require.Equal(t, before, f.dependentState(t))
	require.Equal(t, beforeAudit, f.state(t))
	require.Empty(t, journal.records)
}

func TestCleanupOrphansApplyRequiresJournal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seedOrphans(t, "no-journal-orphan")
	before := f.dependentState(t)
	opts, _ := cleanupOptions(true)
	opts.Journal = nil

	_, err := NewRunner(f.pool, opts).Run(t.Context())
	require.ErrorContains(t, err, "journal")
	require.Equal(t, before, f.dependentState(t))
}

func TestCleanupOrphansRemovesEachKindAndLeavesLiveRows(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	orphanToolset, orphanRows := f.seedOrphans(t, "orphan")
	liveToolset := f.seedWrappedToolset(t, "live")
	liveRows := f.seedDependents(t, liveToolset)
	noSlug := uuid.New()
	require.NoError(t, q.SeedToolsetFixture(t.Context(), SeedToolsetFixtureParams{
		ID: noSlug, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "no slug", Slug: "ts-" + uuid.NewString()[:8],
		McpSlug: pgtype.Text{}, McpIsPublic: false, McpEnabled: false, CustomDomainID: uuid.NullUUID{},
	}))
	noSlugRows := f.seedDependents(t, noSlug)

	deletedProject := newFixtureIn(t, f.pool)
	inDeletedProject := deletedProject.seedWrappedToolset(t, "in-deleted-project-"+uuid.NewString()[:8])
	deletedProjectRows := deletedProject.seedDependents(t, inDeletedProject)
	require.NoError(t, q.SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: deletedProject.projectID, OrganizationID: deletedProject.orgID}))

	opts, journal := cleanupOptions(true)
	report := f.run(t, opts)

	require.Equal(t, 6, report.Outcomes[OutcomeDeleted])
	require.Equal(t, 6, report.Writes)
	require.Equal(t, 3, report.Outcomes[OutcomeSkipped])
	require.Equal(t, 3, report.Outcomes[OutcomeBlockedNoWrapper])
	require.Equal(t, "project deleted", dependentRowFor(t, report, deletedProjectRows.attachmentID).Reason)
	require.Equal(t, "toolset deleted", dependentRowFor(t, report, orphanRows.metadataID).Reason)
	blockedMember := dependentRowFor(t, report, noSlugRows.memberID)
	require.Equal(t, OutcomeBlockedNoWrapper, blockedMember.Outcome)
	require.Contains(t, blockedMember.Reason, "no mcp slug")
	require.Nil(t, blockedMember.Removed)
	for _, id := range []uuid.UUID{liveRows.metadataID, liveRows.memberID, liveRows.attachmentID} {
		require.Equal(t, OutcomeSkipped, dependentRowFor(t, report, id).Outcome)
	}

	key := uuid.NullUUID{UUID: orphanToolset, Valid: true}
	state := f.dependentState(t)
	for _, m := range state.metadata {
		require.NotEqual(t, orphanRows.metadataID, m.ID)
	}
	for _, c := range state.envConfigs {
		require.NotEqual(t, orphanRows.envConfigID, c.ID, "env configs cascade with their metadata")
	}
	require.Len(t, state.metadata, 2)
	require.Len(t, state.envConfigs, 2)
	require.ElementsMatch(t, []ListPluginServersFixtureRow{
		{ID: orphanRows.memberID, ToolsetID: key, McpServerID: uuid.NullUUID{}, Deleted: true},
		{ID: liveRows.memberID, ToolsetID: uuid.NullUUID{UUID: liveToolset, Valid: true}, McpServerID: uuid.NullUUID{}, Deleted: false},
		{ID: noSlugRows.memberID, ToolsetID: uuid.NullUUID{UUID: noSlug, Valid: true}, McpServerID: uuid.NullUUID{}, Deleted: false},
	}, state.members)
	require.Len(t, state.toolsets, 2)
	for _, a := range state.toolsets {
		require.NotEqual(t, orphanRows.attachmentID, a.ID)
	}
	gone := deletedProject.dependentState(t)
	require.Empty(t, gone.metadata)
	require.Empty(t, gone.toolsets)
	require.Equal(t, []ListPluginServersFixtureRow{{ID: deletedProjectRows.memberID, ToolsetID: uuid.NullUUID{UUID: inDeletedProject, Valid: true}, McpServerID: uuid.NullUUID{}, Deleted: true}}, gone.members)

	require.Len(t, journal.records, 12)
	for i := 0; i < len(journal.records); i += 2 {
		require.Equal(t, JournalIntent, journal.records[i].Event)
		require.Equal(t, JournalCommitted, journal.records[i+1].Event)
		require.Equal(t, journal.records[i].RowID, journal.records[i+1].RowID)
	}
	committed := journal.committed()
	metadataRecord := committed[orphanRows.metadataID]
	require.Equal(t, RowKey{Table: tableMcpMetadata, Column: "toolset_id", ID: orphanToolset}, metadataRecord.From)
	require.Equal(t, RowKey{Table: "", Column: "", ID: uuid.Nil}, metadataRecord.To)
	require.Equal(t, "deleted", metadataRecord.Removed.Deletion)
	require.Contains(t, string(metadataRecord.Removed.Row), `"instructions": "legacy"`)
	require.Contains(t, string(metadataRecord.Removed.EnvironmentConfigs), orphanRows.envConfigID.String())
	require.Equal(t, "soft_deleted", committed[orphanRows.memberID].Removed.Deletion)
	require.Contains(t, string(committed[orphanRows.attachmentID].Removed.Row), orphanRows.assistantID.String())
	for _, row := range report.Rows {
		if row.Removed != nil {
			require.Nil(t, row.Removed.Row, "row images go to the journal only")
			require.Nil(t, row.Removed.EnvironmentConfigs)
		}
	}
	require.Equal(t, metadataRecord.Removed.withoutImages(), dependentRowFor(t, report, orphanRows.metadataID).Removed)
	require.NotEmpty(t, committed[orphanRows.memberID].Removed.SoftDeletedAt)

	actor := func(action audit.Action) ListAuditActorsFixtureRow {
		return ListAuditActorsFixtureRow{ActorID: OrphansActorComponent, ActorType: string(urn.PrincipalTypeSystem), Action: string(action)}
	}
	want := []ListAuditActorsFixtureRow{actor(audit.ActionMCPMetadataUpdate), actor(audit.ActionPluginServerRemove)}
	require.ElementsMatch(t, want, f.state(t).audits)
	require.ElementsMatch(t, want, deletedProject.state(t).audits)
	snapshots, err := q.ListAuditSnapshotsFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	for _, snapshot := range snapshots {
		switch snapshot.Action {
		case string(audit.ActionMCPMetadataUpdate):
			require.Contains(t, string(snapshot.BeforeSnapshot), orphanRows.metadataID.String())
			require.Nil(t, snapshot.AfterSnapshot)
		case string(audit.ActionPluginServerRemove):
			require.Contains(t, string(snapshot.Metadata), orphanRows.memberID.String())
			require.Contains(t, string(snapshot.Metadata), urn.NewToolset(orphanToolset).String())
		}
	}
	outbox, err := q.CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox, "cleanup audit rows must not publish webhooks")

	rerunOpts, rerunJournal := cleanupOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Zero(t, rerun.Outcomes[OutcomeDeleted])
	require.Empty(t, rerunJournal.records)
	require.Equal(t, state, f.dependentState(t))
}

func TestCleanupOrphansLeavesRowsWithAServerKeyedTwin(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "orphan-twinned")
	rows := f.seedDependents(t, toolsetID)
	f.seedTwins(t, toolsetID, rows)
	require.NoError(t, New(f.pool).SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: toolsetID, ProjectID: f.projectID}))
	before := f.dependentState(t)
	opts, journal := cleanupOptions(true)

	report := f.run(t, opts)

	require.Equal(t, 3, report.Outcomes[OutcomeBlockedTwin])
	require.Zero(t, report.Writes)
	require.Equal(t, reasonTwinExists, dependentRowFor(t, report, rows.memberID).Reason)
	require.Equal(t, before, f.dependentState(t))
	require.Empty(t, journal.records)
}

func TestCleanupOrphansProjectFilter(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	f.seedOrphans(t, "filtered-"+uuid.NewString()[:8])
	other.seedOrphans(t, "elsewhere-"+uuid.NewString()[:8])
	otherBefore := other.dependentState(t)
	opts, _ := cleanupOptions(true)
	opts.ProjectID = uuid.NullUUID{UUID: f.projectID, Valid: true}

	report := f.run(t, opts)

	require.Equal(t, 3, report.Outcomes[OutcomeDeleted])
	require.Equal(t, otherBefore, other.dependentState(t))
}
