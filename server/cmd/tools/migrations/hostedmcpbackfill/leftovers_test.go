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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func leftoverOptions(apply bool) (Options, *memJournal) {
	journal := &memJournal{}
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, DeleteLeftoverRemovals: true, Journal: journal}, journal
}

func (f *fixture) pluginServerImages(t *testing.T) map[uuid.UUID]string {
	t.Helper()
	rows, err := New(f.pool).ListPluginServerImagesFixture(t.Context(), f.projectID)
	require.NoError(t, err)
	out := make(map[uuid.UUID]string, len(rows))
	for _, row := range rows {
		out[row.ID] = row.RowText
	}
	return out
}

// deleteToolset soft-deletes the toolset and its canonical server, as toolset deletion does.
func (f *fixture) deleteToolset(t *testing.T, id uuid.UUID) {
	t.Helper()
	q := New(f.pool)
	require.NoError(t, q.SoftDeleteToolsetFixture(t.Context(), SoftDeleteToolsetFixtureParams{ID: id, ProjectID: f.projectID}))
	require.NoError(t, q.SoftDeleteServerFixture(t.Context(), SoftDeleteServerFixtureParams{ID: id, ProjectID: f.projectID}))
}

type leftoverSeed struct {
	f, deletedProject            *fixture
	plugin                       uuid.UUID
	want                         map[uuid.UUID]Outcome
	serverKeyed, liveServerKeyed uuid.UUID
	gone                         uuid.UUID
}

// seedLeftovers seeds one toolset-keyed row per outcome, plus server-keyed rows the mode never lists.
func seedLeftovers(t *testing.T) leftoverSeed {
	t.Helper()
	f := newFixture(t)
	q := New(f.pool)
	plugin := f.seedPlugin(t)

	deletedToolset := f.seedWrappedToolset(t, "gone-"+uuid.NewString()[:8])
	gone := f.seedRemoval(t, plugin, key(deletedToolset), uuid.NullUUID{}, "gone")
	f.deleteToolset(t, deletedToolset)

	duplicated := f.seedWrappedToolset(t, "duplicated-"+uuid.NewString()[:8])
	duplicate := f.seedRemoval(t, plugin, key(duplicated), uuid.NullUUID{}, "duplicate")
	serverKeyed := f.seedRemoval(t, plugin, uuid.NullUUID{}, key(duplicated), "existing")
	// A live server-keyed member of the same pair, as after a re-add; it must not count as the removal record.
	liveServerKeyed := uuid.New()
	readdedToolset := f.seedWrappedToolset(t, "readded-"+uuid.NewString()[:8])
	needsRekey := f.seedRemoval(t, plugin, key(readdedToolset), uuid.NullUUID{}, "needs rekey")
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: liveServerKeyed, PluginID: plugin, ToolsetID: uuid.NullUUID{}, McpServerID: key(readdedToolset), DisplayName: "readded"}))

	unwrapped := f.seedToolset(t, toolsetSpec{mcpSlug: "unwrapped-" + uuid.NewString()[:8], public: true, enabled: true, domainID: uuid.NullUUID{}})
	noCanonical := f.seedRemoval(t, plugin, key(unwrapped), uuid.NullUUID{}, "no canonical")

	tombstoned := f.seedToolset(t, toolsetSpec{mcpSlug: "tombstoned-" + uuid.NewString()[:8], public: true, enabled: true, domainID: uuid.NullUUID{}})
	require.NoError(t, q.SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: tombstoned, ProjectID: f.projectID, Name: pgtype.Text{String: "tombstoned", Valid: true}, Slug: pgtype.Text{String: "tombstoned-" + uuid.NewString()[:8], Valid: true},
		ToolsetID: key(tombstoned), Visibility: "public", DeletedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true},
	}))
	tombstonedRecord := f.seedRemoval(t, plugin, key(tombstoned), uuid.NullUUID{}, "tombstoned")
	// A server-keyed record under a tombstoned server no longer counts as history in role delivery, so it does not license the delete.
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(tombstoned), "tombstoned existing")

	liveToolset := f.seedWrappedToolset(t, "live-"+uuid.NewString()[:8])
	live := uuid.New()
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: live, PluginID: plugin, ToolsetID: key(liveToolset), McpServerID: uuid.NullUUID{}, DisplayName: "live"}))

	distributedToolset := f.seedWrappedToolset(t, "distributed-"+uuid.NewString()[:8])
	distributed := f.seedRemoval(t, plugin, key(distributedToolset), uuid.NullUUID{}, "distributed")
	f.deleteToolset(t, distributedToolset)
	require.NoError(t, q.SeedDistributionReferenceFixture(t.Context(), SeedDistributionReferenceFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, PluginID: plugin, PluginServerID: key(distributed)}))

	other := newFixtureIn(t, f.pool)
	foreign := other.seedWrappedToolset(t, "foreign-"+uuid.NewString()[:8])
	crossProject := f.seedRemoval(t, plugin, key(foreign), uuid.NullUUID{}, "cross project")

	deletedProject := newFixtureIn(t, f.pool)
	inDeletedProject := deletedProject.seedWrappedToolset(t, "in-deleted-project-"+uuid.NewString()[:8])
	projectGone := deletedProject.seedRemoval(t, deletedProject.seedPlugin(t), key(inDeletedProject), uuid.NullUUID{}, "project gone")
	require.NoError(t, q.SoftDeleteProjectFixture(t.Context(), SoftDeleteProjectFixtureParams{ID: deletedProject.projectID, OrganizationID: deletedProject.orgID}))

	return leftoverSeed{
		f: f, deletedProject: deletedProject, plugin: plugin, serverKeyed: serverKeyed, liveServerKeyed: liveServerKeyed, gone: gone,
		want: map[uuid.UUID]Outcome{
			gone: OutcomeDeleted, duplicate: OutcomeDeleted, projectGone: OutcomeDeleted,
			needsRekey: OutcomeBlockedNeedsRekey, noCanonical: OutcomeBlockedNoWrapper, tombstonedRecord: OutcomeBlockedNoWrapper,
			crossProject: OutcomeBlockedNoWrapper, live: OutcomeBlockedLive, distributed: OutcomeBlockedDistributed,
		},
	}
}

func TestDeleteLeftoverRemovalRecordsDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	s := seedLeftovers(t)
	before, deletedBefore, stateBefore := s.f.pluginServerImages(t), s.deletedProject.pluginServerImages(t), s.f.state(t)
	opts, journal := leftoverOptions(false)

	report := s.f.run(t, opts)

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseLeftovers, report.Phase)
	require.Equal(t, 3, report.Outcomes[OutcomeWouldDelete])
	require.Equal(t, 3, report.Writes)
	require.Zero(t, report.Outcomes[OutcomeDeleted])
	for id, outcome := range s.want {
		if outcome == OutcomeDeleted {
			outcome = OutcomeWouldDelete
		}
		require.Equal(t, outcome, dependentRowFor(t, report, id).Outcome, id.String())
	}
	require.Equal(t, before, s.f.pluginServerImages(t))
	require.Equal(t, deletedBefore, s.deletedProject.pluginServerImages(t))
	require.Equal(t, stateBefore, s.f.state(t))
	require.Empty(t, journal.records)
}

func TestDeleteLeftoverRemovalRecordsApplyRequiresJournal(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "no-journal-leftover")
	f.seedRemoval(t, f.seedPlugin(t), key(toolsetID), uuid.NullUUID{}, "removed")
	before := f.pluginServerImages(t)
	opts, _ := leftoverOptions(true)
	opts.Journal = nil

	_, err := NewRunner(f.pool, opts).Run(t.Context())
	require.ErrorContains(t, err, "journal")
	require.Equal(t, before, f.pluginServerImages(t))
}

func TestDeleteLeftoverRemovalRecordsOutcomes(t *testing.T) {
	t.Parallel()
	s := seedLeftovers(t)
	f := s.f
	before, deletedBefore := f.pluginServerImages(t), s.deletedProject.pluginServerImages(t)
	opts, journal := leftoverOptions(true)

	report := f.run(t, opts)

	require.Equal(t, phaseLeftovers, report.Phase)
	for id, outcome := range s.want {
		require.Equal(t, outcome, dependentRowFor(t, report, id).Outcome, id.String())
	}
	require.Len(t, report.Rows, len(s.want), "server-keyed rows are never listed")
	require.Equal(t, 3, report.Writes)
	require.Equal(t, 3, report.Outcomes[OutcomeDeleted])
	byReason := map[Outcome][]string{}
	for _, row := range report.Rows {
		byReason[row.Outcome] = append(byReason[row.Outcome], row.Reason)
	}
	require.ElementsMatch(t, []string{"toolset deleted", "project deleted", reasonLeftoverDuplicate}, byReason[OutcomeDeleted])
	require.ElementsMatch(t, []string{reasonNeedsRekey}, byReason[OutcomeBlockedNeedsRekey])
	require.ElementsMatch(t, []string{reasonLeftoverNoCanonical, reasonLeftoverNoCanonical, "toolset is not in the row's project"}, byReason[OutcomeBlockedNoWrapper])
	require.ElementsMatch(t, []string{reasonLeftoverLive}, byReason[OutcomeBlockedLive])
	require.ElementsMatch(t, []string{reasonDistributed}, byReason[OutcomeBlockedDistributed])

	after, deletedAfter := f.pluginServerImages(t), s.deletedProject.pluginServerImages(t)
	for id, image := range before {
		if s.want[id] == OutcomeDeleted {
			require.NotContains(t, after, id, "eligible row is deleted")
			continue
		}
		require.Equal(t, image, after[id], "ineligible row is untouched")
	}
	require.Len(t, after, len(before)-2)
	require.Contains(t, after, s.serverKeyed)
	require.Contains(t, after, s.liveServerKeyed)
	require.Len(t, deletedBefore, 1)
	require.Empty(t, deletedAfter)
	require.Empty(t, f.state(t).audits, "the delete is not audited")
	outbox, err := New(f.pool).CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox)

	committed := journal.committed()
	require.Len(t, journal.records, 6)
	require.Len(t, committed, 3)
	for id, record := range committed {
		require.Equal(t, OutcomeDeleted, s.want[id])
		require.Equal(t, OutcomeDeleted, record.Outcome)
		require.Equal(t, RowKey{Table: tablePluginServers, Column: "toolset_id", ID: record.ToolsetID}, record.From)
		require.Equal(t, tablePluginServers, record.Removed.Table)
		require.Equal(t, "deleted", record.Removed.Deletion)
		require.NotEmpty(t, record.Removed.SoftDeletedAt)
		var image map[string]any
		require.NoError(t, json.Unmarshal(record.Removed.Row, &image))
		require.Equal(t, record.ToolsetID.String(), image["toolset_id"])
		require.Equal(t, true, image["deleted"])
		require.NotNil(t, image["display_name"])
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "display_name", "the report carries no row images")

	rerunOpts, rerunJournal := leftoverOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Zero(t, rerun.Outcomes[OutcomeDeleted])
	require.Empty(t, rerunJournal.records)
	require.Equal(t, after, f.pluginServerImages(t))
}

// A removal record may be all that keeps a live non-canonical wrapper of its toolset out, so it stays.
func TestDeleteLeftoverRemovalRecordsLiveWrappers(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	plugin := f.seedPlugin(t)
	wrapper := func(toolsetID uuid.UUID, deleted bool) uuid.UUID {
		id := uuid.New()
		slug := "extra-" + uuid.NewString()[:8]
		require.NoError(t, q.SeedServerFixture(t.Context(), SeedServerFixtureParams{
			ID: id, ProjectID: f.projectID, Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
			ToolsetID: key(toolsetID), Visibility: "public", DeletedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: deleted},
		}))
		return id
	}

	liveWrapperToolset := f.seedWrappedToolset(t, "live-wrapper-"+uuid.NewString()[:8])
	wrapper(liveWrapperToolset, false)
	deletedLiveWrapper := f.seedRemoval(t, plugin, key(liveWrapperToolset), uuid.NullUUID{}, "deleted toolset, live wrapper")
	f.deleteToolset(t, liveWrapperToolset)

	goneWrapperToolset := f.seedWrappedToolset(t, "gone-wrapper-"+uuid.NewString()[:8])
	wrapper(goneWrapperToolset, true)
	deletedGoneWrapper := f.seedRemoval(t, plugin, key(goneWrapperToolset), uuid.NullUUID{}, "deleted toolset, deleted wrapper")
	f.deleteToolset(t, goneWrapperToolset)

	uncovered := f.seedWrappedToolset(t, "uncovered-"+uuid.NewString()[:8])
	wrapper(uncovered, false)
	duplicateUncovered := f.seedRemoval(t, plugin, key(uncovered), uuid.NullUUID{}, "duplicate, uncovered wrapper")
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(uncovered), "canonical removal")

	covered := f.seedWrappedToolset(t, "covered-"+uuid.NewString()[:8])
	coveredWrapper := wrapper(covered, false)
	duplicateCovered := f.seedRemoval(t, plugin, key(covered), uuid.NullUUID{}, "duplicate, covered wrapper")
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(covered), "canonical removal")
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(coveredWrapper), "wrapper removal")

	want := map[uuid.UUID]struct {
		outcome Outcome
		reason  string
	}{
		deletedLiveWrapper: {OutcomeBlockedLiveWrapper, reasonDeletedToolsetLiveWrapper},
		deletedGoneWrapper: {OutcomeDeleted, "toolset deleted"},
		duplicateUncovered: {OutcomeBlockedLiveWrapper, reasonDuplicateLiveWrapper},
		duplicateCovered:   {OutcomeDeleted, reasonLeftoverDuplicate},
	}
	before := f.pluginServerImages(t)

	dryOpts, _ := leftoverOptions(false)
	dry := f.run(t, dryOpts)
	require.Equal(t, 2, dry.Outcomes[OutcomeWouldDelete])
	require.Equal(t, 2, dry.Outcomes[OutcomeBlockedLiveWrapper])
	require.Equal(t, before, f.pluginServerImages(t))

	opts, _ := leftoverOptions(true)
	report := f.run(t, opts)
	for id, w := range want {
		got := dependentRowFor(t, report, id)
		require.Equal(t, w.outcome, got.Outcome, id.String())
		require.Equal(t, w.reason, got.Reason, id.String())
	}
	require.Equal(t, 2, report.Writes)
	after := f.pluginServerImages(t)
	require.Len(t, after, len(before)-2)
	require.NotContains(t, after, deletedGoneWrapper)
	require.NotContains(t, after, duplicateCovered)
	require.Equal(t, before[deletedLiveWrapper], after[deletedLiveWrapper])
	require.Equal(t, before[duplicateUncovered], after[duplicateUncovered])

	rerunOpts, _ := leftoverOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Equal(t, 2, rerun.Outcomes[OutcomeBlockedLiveWrapper])
}

// After -rekey-removal-records, the blocked_needs_rekey row is re-keyed and its duplicates become deletable.
func TestDeleteLeftoverRemovalRecordsAfterRekey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	plugin := f.seedPlugin(t)
	toolsetID := f.seedWrappedToolset(t, "twice-"+uuid.NewString()[:8])
	first := f.seedRemoval(t, plugin, key(toolsetID), uuid.NullUUID{}, "first")
	second := f.seedRemoval(t, plugin, key(toolsetID), uuid.NullUUID{}, "second")

	opts, _ := leftoverOptions(true)
	report := f.run(t, opts)
	require.Equal(t, 2, report.Outcomes[OutcomeBlockedNeedsRekey])
	require.Zero(t, report.Writes)

	rekeyOpts, _ := rekeyOptions(true)
	require.Equal(t, 1, f.run(t, rekeyOpts).Outcomes[OutcomeRekeyed])

	opts, _ = leftoverOptions(true)
	report = f.run(t, opts)
	require.Equal(t, 1, report.Outcomes[OutcomeDeleted])
	rows := f.removals(t)
	require.Len(t, rows, 1)
	require.Contains(t, []uuid.UUID{first, second}, rows[0].ID)
	require.Equal(t, key(toolsetID), rows[0].McpServerID)
	require.False(t, rows[0].ToolsetID.Valid)
}

func TestDeleteLeftoverRemovalRecordsProjectFilterAndLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := newFixtureIn(t, f.pool)
	plugin := f.seedPlugin(t)
	first, second := f.seedWrappedToolset(t, "first-"+uuid.NewString()[:8]), f.seedWrappedToolset(t, "second-"+uuid.NewString()[:8])
	f.seedRemoval(t, plugin, key(first), uuid.NullUUID{}, "first")
	f.seedRemoval(t, plugin, key(second), uuid.NullUUID{}, "second")
	for _, id := range []uuid.UUID{first, second} {
		f.deleteToolset(t, id)
	}
	elsewhere := other.seedWrappedToolset(t, "elsewhere-"+uuid.NewString()[:8])
	other.seedRemoval(t, other.seedPlugin(t), key(elsewhere), uuid.NullUUID{}, "elsewhere")
	other.deleteToolset(t, elsewhere)
	otherBefore := other.pluginServerImages(t)
	opts, _ := leftoverOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Limit = 1

	report := f.run(t, opts)
	require.Equal(t, 1, report.Toolsets)
	require.Equal(t, 1, report.Outcomes[OutcomeDeleted])
	require.Len(t, f.pluginServerImages(t), 1)

	opts, _ = leftoverOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Cursor = report.LastCursor
	resumed := f.run(t, opts)
	require.Equal(t, 1, resumed.Outcomes[OutcomeDeleted])
	require.Empty(t, f.pluginServerImages(t))
	require.Equal(t, otherBefore, other.pluginServerImages(t))
}

// TestRestoreLeftoverRemovalRecordsJQReplaysCommittedRows runs restore-leftover-removal-records.jq against a real journal.
func TestRestoreLeftoverRemovalRecordsJQReplaysCommittedRows(t *testing.T) {
	t.Parallel()
	jq, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is a mise tool; run the tests through mise")
	s := seedLeftovers(t)
	f := s.f
	before, deletedBefore := f.pluginServerImages(t), s.deletedProject.pluginServerImages(t)
	opts, journal := leftoverOptions(true)
	require.Equal(t, 3, f.run(t, opts).Outcomes[OutcomeDeleted])

	// The gone row keeps only its intent, as when the journal write after its commit fails.
	records := make([]JournalRecord, 0, len(journal.records)+2)
	for _, record := range journal.records {
		if record.Event == JournalCommitted && record.RowID == s.gone {
			continue
		}
		records = append(records, record)
	}
	// An intent-only record for a row whose delete never committed; its stale image must not overwrite the row.
	var stale map[string]any
	require.NoError(t, json.Unmarshal([]byte(before[s.serverKeyed]), &stale))
	stale["display_name"] = "stale"
	staleRow, err := json.Marshal(stale)
	require.NoError(t, err)
	records = append(records, JournalRecord{
		Event: JournalIntent, ProjectID: f.projectID, ToolsetID: uuid.New(), RowID: s.serverKeyed, Outcome: OutcomeDeleted,
		From: RowKey{Table: tablePluginServers, Column: "toolset_id", ID: uuid.New()}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
		CollapsedTwin: nil, Removed: &RowImage{Table: tablePluginServers, ID: s.serverKeyed, Deletion: "deleted", Row: staleRow, EnvironmentConfigs: nil, SoftDeletedAt: "", RekeyedUpdatedAt: ""}, Rekeyed: nil,
	}, JournalRecord{
		Event: JournalCommitUnknown, ProjectID: f.projectID, ToolsetID: uuid.New(), RowID: uuid.New(), Outcome: OutcomeCommitUnknown,
		From: RowKey{Table: tablePluginServers, Column: "toolset_id", ID: uuid.New()}, To: RowKey{Table: "", Column: "", ID: uuid.Nil},
	})
	var lines bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		require.NoError(t, err)
		lines.Write(append(line, '\n'))
	}
	journalPath := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, os.WriteFile(journalPath, lines.Bytes(), 0o600))

	replay := func(project uuid.UUID) string {
		out, err := exec.CommandContext(t.Context(), jq, "-r", "-s", "--arg", "p", project.String(), "-f", "restore-leftover-removal-records.jq", journalPath).Output() // #nosec G204 -- fixed jq program and test paths
		require.NoError(t, err)
		script := string(out)
		for range 2 {
			tx, err := f.pool.Begin(t.Context()) //nolint:glint // notestingrawsql: executes the runbook's generated restore SQL, which is the code under test.
			require.NoError(t, err)
			for statement := range strings.SplitSeq(strings.TrimSpace(script), "\n") {
				if statement == "" || strings.HasPrefix(statement, "--") {
					continue
				}
				_, err := tx.Exec(t.Context(), statement) //nolint:glint // notestingrawsql: the runbook's generated restore SQL is the code under test.
				require.NoError(t, err, statement)
			}
			require.NoError(t, tx.Commit(t.Context()))
		}
		return script
	}

	script := replay(f.projectID)
	require.Contains(t, script, "-- commit_unknown: plugin_servers")
	require.Contains(t, script, "-- no row image: plugin_servers")
	require.Contains(t, script, "-- intent without outcome: plugin_servers "+s.gone.String()+"; check by hand\nINSERT INTO plugin_servers")
	require.Contains(t, script, "-- intent without outcome: plugin_servers "+s.serverKeyed.String()+"; check by hand\nINSERT INTO plugin_servers")
	require.Equal(t, 3, strings.Count(script, "INSERT INTO plugin_servers"))
	require.Equal(t, before, f.pluginServerImages(t), "the restore re-inserts exactly the deleted rows, intent-only ones included, and leaves present rows alone")
	require.Empty(t, s.deletedProject.pluginServerImages(t), "a project's restore leaves other projects alone")

	require.Equal(t, 1, strings.Count(replay(s.deletedProject.projectID), "INSERT INTO plugin_servers"))
	require.Equal(t, deletedBefore, s.deletedProject.pluginServerImages(t))
	require.Equal(t, before, f.pluginServerImages(t))
}

// Deleting a toolset-keyed duplicate keeps the canonical server out of the plugin: the server-keyed record still blocks the re-add.
func TestDeletedDuplicateRemovalRecordStillBlocksRoleDeliveryReAdd(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	toolsetID := f.seedWrappedToolset(t, "role-delivered")
	removedFrom, control := f.seedPlugin(t), f.seedPlugin(t)

	role := "role:organization:" + uuid.NewString()
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolsetID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.pool).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)
	for _, plugin := range []uuid.UUID{removedFrom, control} {
		_, err := pluginsrepo.New(f.pool).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{OrganizationID: f.orgID, PrincipalUrn: role, InstallMode: "", PluginID: plugin})
		require.NoError(t, err)
	}
	duplicate := f.seedRemoval(t, removedFrom, key(toolsetID), uuid.NullUUID{}, "removed")
	serverKeyed := f.seedRemoval(t, removedFrom, uuid.NullUUID{}, key(toolsetID), "removed again")

	// eligible runs the MCP-enable hook in a transaction it rolls back and returns each plugin's live members.
	eligible := func() (removed, delivered []pluginsrepo.PluginServer) {
		tx, err := f.pool.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for the role-delivery hook under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }()
		changed, err := roledelivery.Eligible(ctx, tx, f.orgID, f.projectID, toolsetID, nil)
		require.NoError(t, err)
		require.True(t, changed, "the control plugin receives the server")
		removed, err = pluginsrepo.New(tx).ListPluginServers(ctx, removedFrom)
		require.NoError(t, err)
		delivered, err = pluginsrepo.New(tx).ListPluginServers(ctx, control)
		require.NoError(t, err)
		return removed, delivered
	}

	removed, delivered := eligible()
	require.Empty(t, removed)
	require.Len(t, delivered, 1)

	opts, _ := leftoverOptions(true)
	report := f.run(t, opts)
	require.Equal(t, OutcomeDeleted, dependentRowFor(t, report, duplicate).Outcome)
	rows := f.removals(t)
	require.Len(t, rows, 1)
	require.Equal(t, serverKeyed, rows[0].ID)

	removed, delivered = eligible()
	require.Empty(t, removed, "the server-keyed removal keeps the server out")
	require.Len(t, delivered, 1)
	require.Equal(t, key(toolsetID), delivered[0].McpServerID)

	populate := func(plugin uuid.UUID) (bool, []pluginsrepo.PluginServer) {
		tx, err := f.pool.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for the role-delivery setup under test.
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(context.Background()) }()
		changed, err := roledelivery.Populate(ctx, tx, f.orgID, f.projectID, plugin, role, nil)
		require.NoError(t, err)
		members, err := pluginsrepo.New(tx).ListPluginServers(ctx, plugin)
		require.NoError(t, err)
		return changed, members
	}
	changed, members := populate(removedFrom)
	require.False(t, changed, "setup delivery honours the server-keyed removal")
	require.Empty(t, members)
	changed, members = populate(control)
	require.True(t, changed)
	require.Len(t, members, 1)
}

// The DELETE rechecks eligibility itself, so a state that changed after the caller's checks deletes nothing.
func TestDeleteLeftoverRemovalRecordRechecksEligibility(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	q := New(f.pool)
	plugin, otherPlugin := f.seedPlugin(t), f.seedPlugin(t)
	del := func(id, toolsetID uuid.UUID, eligibility string) int64 {
		n, err := q.DeleteLeftoverRemovalRecord(t.Context(), DeleteLeftoverRemovalRecordParams{ID: id, ProjectID: f.projectID, ToolsetID: toolsetID, Eligibility: eligibility})
		require.NoError(t, err)
		return n
	}
	liveServer := func(toolsetID uuid.UUID) uuid.UUID {
		id := uuid.New()
		slug := "late-" + uuid.NewString()[:8]
		require.NoError(t, q.SeedServerFixture(t.Context(), SeedServerFixtureParams{
			ID: id, ProjectID: f.projectID, Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
			ToolsetID: key(toolsetID), Visibility: "public", DeletedAt: pgtype.Timestamptz{},
		}))
		return id
	}

	gone := f.seedWrappedToolset(t, "gone-"+uuid.NewString()[:8])
	goneRecord := f.seedRemoval(t, plugin, key(gone), uuid.NullUUID{}, "gone")
	f.deleteToolset(t, gone)
	late := liveServer(gone)
	require.Zero(t, del(goneRecord, gone, eligibilityToolsetDeleted), "a live wrapper appeared")
	require.Zero(t, del(goneRecord, gone, eligibilityProjectDeleted), "the project is live")
	require.Zero(t, del(goneRecord, gone, "unknown"))

	distributedToolset := f.seedWrappedToolset(t, "distributed-"+uuid.NewString()[:8])
	distributed := f.seedRemoval(t, plugin, key(distributedToolset), uuid.NullUUID{}, "distributed")
	f.deleteToolset(t, distributedToolset)
	require.NoError(t, q.SeedDistributionReferenceFixture(t.Context(), SeedDistributionReferenceFixtureParams{OrganizationID: f.orgID, ProjectID: f.projectID, PluginID: plugin, PluginServerID: key(distributed)}))
	require.Zero(t, del(distributed, distributedToolset, eligibilityToolsetDeleted), "a distribution references the row")

	dup := f.seedWrappedToolset(t, "duplicate-"+uuid.NewString()[:8])
	dupRecord := f.seedRemoval(t, plugin, key(dup), uuid.NullUUID{}, "duplicate")
	f.seedRemoval(t, otherPlugin, uuid.NullUUID{}, key(dup), "other plugin's canonical removal")
	require.Zero(t, del(dupRecord, dup, eligibilityDuplicate), "only another plugin has a server-keyed removal")
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(dup), "canonical removal")
	lateDup := liveServer(dup)
	require.Zero(t, del(dupRecord, dup, eligibilityDuplicate), "a live wrapper without a removal appeared")
	require.Zero(t, del(dupRecord, dup, eligibilityToolsetDeleted), "the toolset's servers are live")

	images := f.pluginServerImages(t)
	require.Contains(t, images, goneRecord)
	require.Contains(t, images, distributed)
	require.Contains(t, images, dupRecord)

	require.NoError(t, q.SoftDeleteServerFixture(t.Context(), SoftDeleteServerFixtureParams{ID: late, ProjectID: f.projectID}))
	require.EqualValues(t, 1, del(goneRecord, gone, eligibilityToolsetDeleted))
	f.seedRemoval(t, plugin, uuid.NullUUID{}, key(lateDup), "late wrapper removal")
	require.EqualValues(t, 1, del(dupRecord, dup, eligibilityDuplicate))
}
