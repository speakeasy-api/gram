package hostedmcpbackfill

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func rekeyOptions(apply bool) (Options, *memJournal) {
	journal := &memJournal{}
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, RekeyRemovals: true, Journal: journal}, journal
}

func (f *fixture) seedPlugin(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedPluginFixture(t.Context(), SeedPluginFixtureParams{ID: id, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "plugin", Slug: "plugin-" + uuid.NewString()[:8]}))
	return id
}

// seedRemoval seeds a soft-deleted plugin member keyed by exactly one of toolsetID or serverID.
func (f *fixture) seedRemoval(t *testing.T, pluginID uuid.UUID, toolsetID, serverID uuid.NullUUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedRemovedPluginServerFixture(t.Context(), SeedRemovedPluginServerFixtureParams{
		ID: id, PluginID: pluginID, ToolsetID: toolsetID, McpServerID: serverID, DisplayName: name,
		DeletedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour).Truncate(time.Microsecond), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	return id
}

func (f *fixture) removals(t *testing.T) []ListPluginServerRemovalsFixtureRow {
	t.Helper()
	rows, err := New(f.pool).ListPluginServerRemovalsFixture(t.Context(), f.projectID)
	require.NoError(t, err)
	return rows
}

func removalFor(t *testing.T, rows []ListPluginServerRemovalsFixtureRow, id uuid.UUID) ListPluginServerRemovalsFixtureRow {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	require.FailNow(t, "plugin server not found", id.String())
	return ListPluginServerRemovalsFixtureRow{}
}

func key(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

func TestRekeyRemovalRecordsDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "dry-removal")
	recordID := f.seedRemoval(t, f.seedPlugin(t), key(toolsetID), uuid.NullUUID{}, "removed")
	before, beforeAudit := f.removals(t), f.state(t)
	opts, journal := rekeyOptions(false)

	report := f.run(t, opts)

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseRemovals, report.Phase)
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 1, report.Outcomes[OutcomeWouldRekey])
	require.Equal(t, 1, report.Writes)
	require.Equal(t, tablePluginServers, dependentRowFor(t, report, recordID).Table)
	require.Equal(t, before, f.removals(t))
	require.Equal(t, beforeAudit, f.state(t))
	require.Empty(t, journal.records)
}

func TestRekeyRemovalRecordsApplyRequiresJournal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "no-journal-removal")
	f.seedRemoval(t, f.seedPlugin(t), key(toolsetID), uuid.NullUUID{}, "removed")
	before := f.removals(t)
	opts, _ := rekeyOptions(true)
	opts.Journal = nil

	_, err := NewRunner(f.pool, opts).Run(t.Context())
	require.ErrorContains(t, err, "journal")
	require.Equal(t, before, f.removals(t))
}

func TestRekeyRemovalRecordsOutcomes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	plugin := f.seedPlugin(t)

	wrapped := f.seedWrappedToolset(t, "rekey")
	rekeyed := f.seedRemoval(t, plugin, key(wrapped), uuid.NullUUID{}, "same name")
	// A live server-keyed member with the same plugin, server and display name: the live-only partial unique indexes must not apply.
	readded := uuid.New()
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: readded, PluginID: plugin, ToolsetID: uuid.NullUUID{}, McpServerID: key(wrapped), DisplayName: "same name"}))

	deletedToolset := f.seedWrappedToolset(t, "gone")
	gone := f.seedRemoval(t, plugin, key(deletedToolset), uuid.NullUUID{}, "gone")
	require.NoError(t, q.SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: deletedToolset, ProjectID: f.projectID}))

	unwrapped := f.seedToolset(t, toolsetSpec{mcpSlug: "unwrapped", public: true, enabled: true, domainID: uuid.NullUUID{}})
	noCanonical := f.seedRemoval(t, plugin, key(unwrapped), uuid.NullUUID{}, "no canonical")

	tombstoned := f.seedToolset(t, toolsetSpec{mcpSlug: "tombstoned", public: true, enabled: true, domainID: uuid.NullUUID{}})
	require.NoError(t, q.SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: tombstoned, ProjectID: f.projectID, Name: pgtype.Text{String: "tombstoned", Valid: true}, Slug: pgtype.Text{String: "tombstoned", Valid: true},
		ToolsetID: key(tombstoned), Visibility: "public", DeletedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	tombstonedRecord := f.seedRemoval(t, plugin, key(tombstoned), uuid.NullUUID{}, "tombstoned")

	duplicated := f.seedWrappedToolset(t, "duplicated")
	duplicate := f.seedRemoval(t, plugin, key(duplicated), uuid.NullUUID{}, "duplicate")
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(duplicated), "existing")

	liveToolset := f.seedWrappedToolset(t, "live")
	live := uuid.New()
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: live, PluginID: plugin, ToolsetID: key(liveToolset), McpServerID: uuid.NullUUID{}, DisplayName: "live"}))

	deletedProject := newFixtureIn(t, f.pool)
	inDeletedProject := deletedProject.seedWrappedToolset(t, "in-deleted-project-"+uuid.NewString()[:8])
	projectGone := deletedProject.seedRemoval(t, deletedProject.seedPlugin(t), key(inDeletedProject), uuid.NullUUID{}, "project gone")
	require.NoError(t, q.SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: deletedProject.projectID, OrganizationID: deletedProject.orgID}))

	other := newFixtureIn(t, f.pool)
	foreign := other.seedWrappedToolset(t, "foreign-"+uuid.NewString()[:8])
	crossProject := f.seedRemoval(t, plugin, key(foreign), uuid.NullUUID{}, "cross project")

	before, deletedProjectBefore := f.removals(t), deletedProject.removals(t)
	opts, journal := rekeyOptions(true)
	report := f.run(t, opts)

	want := map[uuid.UUID]Outcome{
		rekeyed: OutcomeRekeyed, gone: OutcomeOrphaned, noCanonical: OutcomeBlockedNoWrapper, tombstonedRecord: OutcomeBlockedNoWrapper,
		duplicate: OutcomeBlockedDuplicateRemoval, live: OutcomeBlockedLive, projectGone: OutcomeOrphaned, crossProject: OutcomeBlockedNoWrapper,
	}
	for id, outcome := range want {
		require.Equal(t, outcome, dependentRowFor(t, report, id).Outcome, id.String())
	}
	require.Len(t, report.Rows, len(want), "server-keyed rows are never listed")
	require.Equal(t, 1, report.Writes)
	require.Equal(t, "toolset deleted", dependentRowFor(t, report, gone).Reason)
	require.Equal(t, "project deleted", dependentRowFor(t, report, projectGone).Reason)
	require.Equal(t, reasonNoCanonicalServer, dependentRowFor(t, report, noCanonical).Reason)
	require.Equal(t, reasonNoCanonicalServer, dependentRowFor(t, report, tombstonedRecord).Reason)
	require.Equal(t, reasonDuplicateRemoval, dependentRowFor(t, report, duplicate).Reason)
	require.Equal(t, reasonLiveMember, dependentRowFor(t, report, live).Reason)
	require.Equal(t, "toolset is not in the row's project", dependentRowFor(t, report, crossProject).Reason)

	after := f.removals(t)
	moved := removalFor(t, after, rekeyed)
	original := removalFor(t, before, rekeyed)
	require.NotEqual(t, original.UpdatedAt, moved.UpdatedAt)
	require.Equal(t, ListPluginServerRemovalsFixtureRow{ID: rekeyed, PluginID: plugin, ToolsetID: uuid.NullUUID{}, McpServerID: key(wrapped), Deleted: true, DeletedAt: original.DeletedAt, UpdatedAt: moved.UpdatedAt}, moved)
	for _, row := range before {
		if row.ID != rekeyed {
			require.Equal(t, row, removalFor(t, after, row.ID), "only the re-keyed record changes")
		}
	}
	require.Equal(t, deletedProjectBefore, deletedProject.removals(t))
	require.Empty(t, f.state(t).audits, "the re-key is not audited")
	outbox, err := q.CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox)

	require.Len(t, journal.records, 2)
	intent, committed := journal.records[0], journal.records[1]
	require.Equal(t, JournalIntent, intent.Event)
	require.Equal(t, JournalCommitted, committed.Event)
	require.Equal(t, intent.RowID, committed.RowID)
	require.Equal(t, rekeyed, committed.RowID)
	require.Equal(t, f.projectID, committed.ProjectID)
	require.Equal(t, OutcomeRekeyed, committed.Outcome)
	require.Equal(t, RowKey{Table: tablePluginServers, Column: "toolset_id", ID: wrapped}, committed.From)
	require.Equal(t, RowKey{Table: tablePluginServers, Column: "mcp_server_id", ID: wrapped}, committed.To)
	require.Equal(t, "soft_deleted", committed.Rekeyed.Deletion)
	require.Equal(t, original.DeletedAt, committed.Rekeyed.SoftDeletedAt)
	var image map[string]any
	require.NoError(t, json.Unmarshal(committed.Rekeyed.Row, &image))
	require.Equal(t, wrapped.String(), image["toolset_id"])
	require.Nil(t, image["mcp_server_id"])
	require.Equal(t, "same name", image["display_name"])
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "same name", "the report carries no row images")

	rerunOpts, rerunJournal := rekeyOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Zero(t, rerun.Outcomes[OutcomeRekeyed])
	require.Empty(t, rerunJournal.records)
	require.Equal(t, after, f.removals(t))
}

// Two removal records of one plugin and toolset: the dry run reports the second as the duplicate the apply finds.
func TestRekeyRemovalRecordsDuplicatePairMatchesAcrossModes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	plugin := f.seedPlugin(t)
	toolsetID := f.seedWrappedToolset(t, "twice-"+uuid.NewString()[:8])
	f.seedRemoval(t, plugin, key(toolsetID), uuid.NullUUID{}, "first")
	f.seedRemoval(t, plugin, key(toolsetID), uuid.NullUUID{}, "second")
	before := f.removals(t)
	first, second := before[0].ID, before[1].ID

	dryOpts, _ := rekeyOptions(false)
	dry := f.run(t, dryOpts)
	require.Len(t, dry.Outcomes, 2)
	require.Equal(t, 1, dry.Outcomes[OutcomeWouldRekey])
	require.Equal(t, 1, dry.Outcomes[OutcomeBlockedDuplicateRemoval])
	require.Equal(t, 1, dry.Writes)
	require.Equal(t, OutcomeWouldRekey, dependentRowFor(t, dry, first).Outcome)
	require.Equal(t, reasonDuplicateInThisRun, dependentRowFor(t, dry, second).Reason)
	require.Equal(t, before, f.removals(t))

	applyOpts, journal := rekeyOptions(true)
	applied := f.run(t, applyOpts)
	require.Len(t, applied.Outcomes, 2)
	require.Equal(t, 1, applied.Outcomes[OutcomeRekeyed])
	require.Equal(t, 1, applied.Outcomes[OutcomeBlockedDuplicateRemoval])
	require.Equal(t, dry.Writes, applied.Writes)
	require.Equal(t, OutcomeRekeyed, dependentRowFor(t, applied, first).Outcome)
	require.Equal(t, dependentRowFor(t, dry, second), dependentRowFor(t, applied, second))
	require.Len(t, journal.records, 2)

	after := f.removals(t)
	require.Equal(t, key(toolsetID), removalFor(t, after, first).McpServerID)
	require.Equal(t, removalFor(t, before, second), removalFor(t, after, second))

	rerunOpts, _ := rekeyOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Equal(t, reasonDuplicateRemoval, dependentRowFor(t, rerun, second).Reason)
	require.Equal(t, after, f.removals(t))
}

func TestRekeyRemovalRecordsProjectFilterAndLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	plugin := f.seedPlugin(t)
	first, second := f.seedWrappedToolset(t, "first-"+uuid.NewString()[:8]), f.seedWrappedToolset(t, "second-"+uuid.NewString()[:8])
	f.seedRemoval(t, plugin, key(first), uuid.NullUUID{}, "first")
	f.seedRemoval(t, plugin, key(second), uuid.NullUUID{}, "second")
	elsewhere := other.seedWrappedToolset(t, "elsewhere-"+uuid.NewString()[:8])
	other.seedRemoval(t, other.seedPlugin(t), key(elsewhere), uuid.NullUUID{}, "elsewhere")
	otherBefore := other.removals(t)
	opts, _ := rekeyOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Limit = 1

	report := f.run(t, opts)
	require.Equal(t, 1, report.Toolsets)
	require.Equal(t, 1, report.Outcomes[OutcomeRekeyed])

	opts, _ = rekeyOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Cursor = report.LastCursor
	resumed := f.run(t, opts)
	require.Equal(t, 1, resumed.Outcomes[OutcomeRekeyed])
	for _, row := range f.removals(t) {
		require.False(t, row.ToolsetID.Valid, row.ID.String())
	}
	require.Equal(t, otherBefore, other.removals(t))
}

// TestRestoreRemovalRecordsJQReplaysCommittedRows runs restore-removal-records.jq against a real journal.
func TestRestoreRemovalRecordsJQReplaysCommittedRows(t *testing.T) {
	t.Parallel()
	jq, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is a mise tool; run the tests through mise")
	f := newFixture(t)
	q := New(f.pool)
	plugin := f.seedPlugin(t)
	restored := f.seedWrappedToolset(t, "restore-"+uuid.NewString()[:8])
	restoredRecord := f.seedRemoval(t, plugin, key(restored), uuid.NullUUID{}, "restored")
	retouched := f.seedWrappedToolset(t, "retouched-"+uuid.NewString()[:8])
	retouchedRecord := f.seedRemoval(t, plugin, key(retouched), uuid.NullUUID{}, "retouched")
	updated := f.seedWrappedToolset(t, "updated-"+uuid.NewString()[:8])
	updatedRecord := f.seedRemoval(t, plugin, key(updated), uuid.NullUUID{}, "updated")
	before := f.removals(t)
	opts, journal := rekeyOptions(true)
	report := f.run(t, opts)
	require.Equal(t, 3, report.Outcomes[OutcomeRekeyed])
	// Someone changed these records after the re-key; the restore must leave them.
	require.NoError(t, q.BumpPluginServerDeletedAtFixture(t.Context(), BumpPluginServerDeletedAtFixtureParams{ID: retouchedRecord, ProjectID: f.projectID}))
	require.NoError(t, q.BumpPluginServerUpdatedAtFixture(t.Context(), BumpPluginServerUpdatedAtFixtureParams{ID: updatedRecord, ProjectID: f.projectID}))
	rekeyedState := f.removals(t)

	var lines bytes.Buffer
	for _, record := range append(journal.records, JournalRecord{
		Event: JournalCommitUnknown, ProjectID: f.projectID, ToolsetID: uuid.New(), RowID: uuid.New(), Outcome: OutcomeCommitUnknown,
		From: RowKey{Table: tablePluginServers, Column: "toolset_id", ID: uuid.New()}, To: RowKey{Table: tablePluginServers, Column: "mcp_server_id", ID: uuid.New()},
	}) {
		line, err := json.Marshal(record)
		require.NoError(t, err)
		lines.Write(append(line, '\n'))
	}
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, os.WriteFile(journalPath, lines.Bytes(), 0o600))
	out, err := exec.CommandContext(t.Context(), jq, "-r", "-s", "--arg", "p", f.projectID.String(), "-f", "restore-removal-records.jq", journalPath).Output() // #nosec G204 -- fixed jq program and test paths
	require.NoError(t, err)
	script := string(out)
	require.Contains(t, script, "-- commit_unknown: plugin_servers")

	for range 2 {
		tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: executes the runbook's generated restore SQL, which is the code under test.
		require.NoError(t, err)
		for statement := range strings.SplitSeq(strings.TrimSpace(script), "\n") {
			if strings.HasPrefix(statement, "--") {
				continue
			}
			_, err := tx.Exec(t.Context(), statement) //nolint:glint // notestingrawsql: the runbook's generated restore SQL is the code under test.
			require.NoError(t, err, statement)
		}
		require.NoError(t, tx.Commit(t.Context()))
	}

	after := f.removals(t)
	require.Equal(t, removalFor(t, before, restoredRecord), removalFor(t, after, restoredRecord))
	require.Equal(t, removalFor(t, rekeyedState, retouchedRecord), removalFor(t, after, retouchedRecord))
	require.Equal(t, removalFor(t, rekeyedState, updatedRecord), removalFor(t, after, updatedRecord))
}
