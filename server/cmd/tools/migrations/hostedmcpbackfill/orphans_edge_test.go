package hostedmcpbackfill

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/plugins"
)

func TestCleanupOrphansSkipsRowsReKeyedAfterListing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID, rows := f.seedOrphans(t, "rekeyed-after-list")
	q := New(f.pool)
	deps, err := q.ListToolsetDependents(t.Context(), ListToolsetDependentsParams{ToolsetID: toolsetID, ProjectID: uuid.NullUUID{}})
	require.NoError(t, err)
	require.Len(t, deps, 3)
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}
	_, err = q.RekeyMetadata(t.Context(), RekeyMetadataParams{McpServerID: serverKey, ID: rows.metadataID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = q.RekeyPluginServer(t.Context(), RekeyPluginServerParams{McpServerID: serverKey, ID: rows.memberID, ProjectID: f.projectID})
	require.NoError(t, err)
	_, err = q.MoveAssistantToolset(t.Context(), MoveAssistantToolsetParams{McpServerID: toolsetID, ID: rows.attachmentID, ProjectID: f.projectID})
	require.NoError(t, err)
	before := f.dependentState(t)
	opts, journal := cleanupOptions(true)
	runner := NewRunner(f.pool, opts)

	for _, dep := range deps {
		row, err := runner.cleanupOne(t.Context(), toolsetID, dep)
		require.NoError(t, err)
		require.Equal(t, OutcomeSkipped, row.Outcome, dep.TableName)
		require.Equal(t, reasonRowChanged, row.Reason)
	}
	require.Equal(t, before, f.dependentState(t))
	require.Empty(t, journal.records)
}

func TestCleanupOrphansLeavesRowsPointingAtAnotherProjectsToolset(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	foreign := f.seedWrappedToolset(t, "foreign-orphan-"+uuid.NewString()[:8])
	crossRows := other.seedDependents(t, foreign)
	require.NoError(t, New(f.pool).SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: foreign, ProjectID: f.projectID}))
	otherBefore := other.dependentState(t)
	opts, _ := cleanupOptions(true)

	report := f.run(t, opts)

	for _, id := range []uuid.UUID{crossRows.metadataID, crossRows.memberID, crossRows.attachmentID} {
		row := dependentRowFor(t, report, id)
		require.Equal(t, OutcomeBlockedNoWrapper, row.Outcome)
		require.Equal(t, "toolset is not in the row's project", row.Reason)
	}
	require.Zero(t, report.Writes)
	require.Equal(t, otherBefore, other.dependentState(t))
}

func TestCleanupOrphansLeavesAttachmentsOfAnAssistantOutsideTheProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	q := New(f.pool)
	toolsetID, _ := f.seedOrphans(t, "foreign-assistant")
	foreignAssistant, attachmentID := uuid.New(), uuid.New()
	require.NoError(t, q.SeedAssistantFixture(t.Context(), SeedAssistantFixtureParams{ID: foreignAssistant, ProjectID: other.projectID, OrganizationID: other.orgID, Name: "assistant-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedAssistantToolsetFixture(t.Context(), SeedAssistantToolsetFixtureParams{ID: attachmentID, AssistantID: foreignAssistant, ToolsetID: toolsetID, ProjectID: f.projectID, EnvironmentID: uuid.NullUUID{}}))
	opts, _ := cleanupOptions(true)

	report := f.run(t, opts)

	row := dependentRowFor(t, report, attachmentID)
	require.Equal(t, OutcomeOrphaned, row.Outcome)
	require.Equal(t, "assistant is not in the row's project", row.Reason)
	require.Equal(t, 3, report.Outcomes[OutcomeDeleted])
	var ids []uuid.UUID
	for _, a := range f.dependentState(t).toolsets {
		ids = append(ids, a.ID)
	}
	require.Equal(t, []uuid.UUID{attachmentID}, ids)
}

func TestCleanupOrphansBlocksDistributedMembers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, rows := f.seedOrphans(t, "distributed-orphan")
	require.NoError(t, New(f.pool).SeedDistributionReferenceFixture(t.Context(), SeedDistributionReferenceFixtureParams{
		OrganizationID: f.orgID, ProjectID: f.projectID, PluginID: rows.pluginID, PluginServerID: uuid.NullUUID{UUID: rows.memberID, Valid: true},
	}))
	before := f.dependentState(t)
	opts, _ := cleanupOptions(true)

	report := f.run(t, opts)

	member := dependentRowFor(t, report, rows.memberID)
	require.Equal(t, OutcomeBlockedDistributed, member.Outcome)
	require.Equal(t, reasonDistributed, member.Reason)
	require.Equal(t, before.members, f.dependentState(t).members)
}

func TestCleanupOrphansBlocksMetadataWithAForeignProjectConfig(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	_, rows := f.seedOrphans(t, "foreign-config")
	foreignConfig := uuid.New()
	require.NoError(t, New(f.pool).SeedEnvironmentConfigFixture(t.Context(), SeedEnvironmentConfigFixtureParams{ID: foreignConfig, ProjectID: other.projectID, McpMetadataID: rows.metadataID, VariableName: "FOREIGN"}))
	before, otherBefore := f.dependentState(t), other.dependentState(t)
	opts, _ := cleanupOptions(true)

	report := f.run(t, opts)

	metadata := dependentRowFor(t, report, rows.metadataID)
	require.Equal(t, OutcomeBlockedForeignConfig, metadata.Outcome)
	require.Equal(t, before.metadata, f.dependentState(t).metadata)
	require.Equal(t, before.envConfigs, f.dependentState(t).envConfigs)
	require.Equal(t, otherBefore.envConfigs, other.dependentState(t).envConfigs)
	require.Len(t, otherBefore.envConfigs, 1)
}

func TestCleanupOrphansWaitsForAConcurrentProjectRestore(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	f.seedOrphans(t, "project-restore")
	require.NoError(t, q.SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: f.projectID, OrganizationID: f.orgID}))
	before := f.dependentState(t)

	holder, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: transaction boundary only; the restore uses a generated SQLc method.
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	require.NoError(t, New(holder).RestoreProjectFixture(t.Context(), RestoreProjectFixtureParams{ID: f.projectID, OrganizationID: f.orgID}))

	opts, journal := cleanupOptions(true)
	opts.LockTimeout = 200 * time.Millisecond
	_, err = NewRunner(f.pool, opts).Run(t.Context())
	require.Error(t, err)
	require.True(t, isTransient(err), "an in-flight restore must stop the run: %v", err)
	require.Empty(t, journal.records)
	require.NoError(t, holder.Rollback(t.Context()))
	require.Equal(t, before, f.dependentState(t))
}

func TestCleanupOrphansReportsCommitUnknown(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	_, rows := f.seedOrphans(t, "commit-unknown")
	opts, journal := cleanupOptions(true)
	terminated := false
	journal.onAppend = func(record JournalRecord) {
		if record.Event != JournalIntent || terminated {
			return
		}
		terminated = true
		require.NoError(t, New(f.pool).TerminateOtherBackendsFixture(t.Context()))
	}

	report, err := NewRunner(f.pool, opts).Run(t.Context())

	require.ErrorContains(t, err, "commit outcome unknown")
	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeCommitUnknown, report.Rows[0].Outcome)
	require.Equal(t, rows.attachmentID, *report.Rows[0].RowID)
	require.Len(t, journal.records, 2)
	require.Equal(t, JournalIntent, journal.records[0].Event)
	require.Equal(t, JournalCommitUnknown, journal.records[1].Event)
	f.pool.Reset()
	require.Len(t, f.dependentState(t).toolsets, 1, "the killed transaction never committed")
}

func TestCleanupOrphansEnqueuesPluginPublicationOnApplyOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	_, rows := f.seedOrphans(t, "publication")
	require.NoError(t, q.SeedPluginGithubConnectionFixture(t.Context(), f.projectID))

	dryOpts, _ := cleanupOptions(false)
	dryOpts.Publication = plugins.PublicationRequests{Enabled: true}
	dry := f.run(t, dryOpts)
	require.False(t, dependentRowFor(t, dry, rows.memberID).PublicationRequested)
	outbox, err := q.CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox)

	opts, _ := cleanupOptions(true)
	opts.Publication = plugins.PublicationRequests{Enabled: true}
	report := f.run(t, opts)
	require.True(t, dependentRowFor(t, report, rows.memberID).PublicationRequested)
	outbox, err = q.CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Equal(t, int64(1), outbox)
}

// TestRestoreOrphansJQReplaysCommittedRows runs the runbook's restore-orphans.jq against a real journal.
func TestRestoreOrphansJQReplaysCommittedRows(t *testing.T) {
	t.Parallel()
	jq, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is a mise tool; run the tests through mise")
	f := newFixture(t)
	q := New(f.pool)
	_, rows := f.seedOrphans(t, "restore-"+uuid.NewString()[:8])
	_, redeleted := f.seedOrphans(t, "redeleted-"+uuid.NewString()[:8])
	before := f.dependentState(t)
	opts, journal := cleanupOptions(true)
	report := f.run(t, opts)
	require.Equal(t, 6, report.Outcomes[OutcomeDeleted])
	// Someone else restored and deleted this member again after the cleanup; the restore must leave it.
	require.NoError(t, q.BumpPluginServerDeletedAtFixture(t.Context(), BumpPluginServerDeletedAtFixtureParams{ID: redeleted.memberID, ProjectID: f.projectID}))

	var lines bytes.Buffer
	for _, record := range append(journal.records, JournalRecord{
		Event: JournalCommitUnknown, ProjectID: f.projectID, ToolsetID: uuid.New(), RowID: uuid.New(), Outcome: OutcomeCommitUnknown,
		From: RowKey{Table: tablePluginServers, Column: "toolset_id", ID: uuid.New()}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
		CollapsedTwin: nil, Removed: &RowImage{Table: tablePluginServers, ID: uuid.New(), Deletion: "soft_deleted", Row: nil, EnvironmentConfigs: nil, SoftDeletedAt: ""},
	}) {
		line, err := json.Marshal(record)
		require.NoError(t, err)
		lines.Write(append(line, '\n'))
	}
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, os.WriteFile(journalPath, lines.Bytes(), 0o600))
	out, err := exec.CommandContext(t.Context(), jq, "-r", "-s", "--arg", "p", f.projectID.String(), "-f", "restore-orphans.jq", journalPath).Output() // #nosec G204 -- fixed jq program and test paths
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

	restored := f.dependentState(t)
	require.Equal(t, before.metadata, restored.metadata)
	require.Equal(t, before.envConfigs, restored.envConfigs)
	require.Equal(t, before.toolsets, restored.toolsets)
	for _, member := range restored.members {
		require.Equal(t, member.ID == redeleted.memberID, member.Deleted, member.ID.String())
	}
	require.Contains(t, []uuid.UUID{restored.members[0].ID, restored.members[1].ID}, rows.memberID)
}
