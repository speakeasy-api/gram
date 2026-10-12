package hostedmcpbackfill

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type dependentRows struct {
	metadataID   uuid.UUID
	envConfigID  uuid.UUID
	pluginID     uuid.UUID
	memberID     uuid.UUID
	assistantID  uuid.UUID
	attachmentID uuid.UUID
}

type dependentState struct {
	metadata   []ListMetadataFixtureRow
	envConfigs []ListEnvironmentConfigsFixtureRow
	members    []ListPluginServersFixtureRow
	toolsets   []ListAssistantToolsetsFixtureRow
	servers    []ListAssistantMcpServersFixtureRow
}

// seedWrappedToolset seeds a hosted toolset with its canonical wrapper.
func (f *fixture) seedWrappedToolset(t *testing.T, mcpSlug string) uuid.UUID {
	t.Helper()
	id := f.seedToolset(t, toolsetSpec{mcpSlug: mcpSlug, public: true, enabled: true, domainID: uuid.NullUUID{}})
	f.seedServer(t, id, id, "Hosted "+mcpSlug, mcpSlug, "public")
	f.seedEndpoint(t, id, uuid.NullUUID{}, mcpSlug)
	return id
}

// seedDependents seeds one toolset-keyed row in each dependent table, plus an env config on the metadata.
func (f *fixture) seedDependents(t *testing.T, toolsetID uuid.UUID) dependentRows {
	t.Helper()
	q := New(f.pool)
	ctx := t.Context()
	rows := dependentRows{
		metadataID: uuid.New(), envConfigID: uuid.New(), pluginID: uuid.New(), memberID: uuid.New(),
		assistantID: uuid.New(), attachmentID: uuid.New(),
	}
	key := uuid.NullUUID{UUID: toolsetID, Valid: true}
	require.NoError(t, q.SeedMetadataFixture(ctx, SeedMetadataFixtureParams{ID: rows.metadataID, ToolsetID: key, McpServerID: uuid.NullUUID{}, ProjectID: f.projectID}))
	require.NoError(t, q.SeedEnvironmentConfigFixture(ctx, SeedEnvironmentConfigFixtureParams{ID: rows.envConfigID, ProjectID: f.projectID, McpMetadataID: rows.metadataID, VariableName: "API_KEY"}))
	require.NoError(t, q.SeedPluginFixture(ctx, SeedPluginFixtureParams{ID: rows.pluginID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "plugin", Slug: "plugin-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedPluginServerFixture(ctx, SeedPluginServerFixtureParams{ID: rows.memberID, PluginID: rows.pluginID, ToolsetID: key, McpServerID: uuid.NullUUID{}, DisplayName: "member"}))
	require.NoError(t, q.SeedAssistantFixture(ctx, SeedAssistantFixtureParams{ID: rows.assistantID, ProjectID: f.projectID, OrganizationID: f.orgID, Name: "assistant-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedAssistantToolsetFixture(ctx, SeedAssistantToolsetFixtureParams{ID: rows.attachmentID, AssistantID: rows.assistantID, ToolsetID: toolsetID, ProjectID: f.projectID}))
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

func moveOptions(apply bool) Options {
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, MoveDependents: true}
}

func TestMoveDependentsDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "dry-move")
	f.seedDependents(t, toolsetID)
	before, beforeAudit := f.dependentState(t), f.state(t)

	report := f.run(t, moveOptions(false))

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseDependents, report.Phase)
	require.Equal(t, 3, report.Outcomes[OutcomeWouldMove])
	require.Equal(t, 3, report.Writes)
	require.Equal(t, before, f.dependentState(t))
	require.Equal(t, beforeAudit, f.state(t))
}

func TestMoveDependentsRekeysInPlaceAndIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "move-me")
	rows := f.seedDependents(t, toolsetID)
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}

	report := f.run(t, moveOptions(true))
	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 3, report.Outcomes[OutcomeMoved])
	require.Equal(t, toolsetID, report.LastCursor)
	for _, id := range []uuid.UUID{rows.metadataID, rows.memberID, rows.attachmentID} {
		require.Equal(t, "move-me", dependentRowFor(t, report, id).McpSlug)
	}

	state := f.dependentState(t)
	require.Equal(t, []ListMetadataFixtureRow{{ID: rows.metadataID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey}}, state.metadata)
	require.Equal(t, []ListEnvironmentConfigsFixtureRow{{ID: rows.envConfigID, McpMetadataID: rows.metadataID, VariableName: "API_KEY"}}, state.envConfigs)
	require.Equal(t, []ListPluginServersFixtureRow{{ID: rows.memberID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey}}, state.members)
	require.Empty(t, state.toolsets)
	require.Equal(t, []ListAssistantMcpServersFixtureRow{{ID: rows.attachmentID, AssistantID: rows.assistantID, McpServerID: toolsetID}}, state.servers)

	audits := f.state(t).audits
	require.ElementsMatch(t, []ListAuditActorsFixtureRow{
		{ActorID: DependentsActorComponent, ActorType: string(urn.PrincipalTypeSystem), Action: string(audit.ActionMCPMetadataUpdate)},
		{ActorID: DependentsActorComponent, ActorType: string(urn.PrincipalTypeSystem), Action: string(audit.ActionPluginServerUpdate)},
	}, audits)
	outbox, err := New(f.pool).CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox, "backfill audit rows must not publish webhooks")

	rerun := f.run(t, moveOptions(true))
	require.Zero(t, rerun.Scanned)
	require.Zero(t, rerun.Writes)
	require.Equal(t, state, f.dependentState(t))
}

func TestMoveDependentsReportsTwinsAndLeavesThemUntouched(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "twinned")
	rows := f.seedDependents(t, toolsetID)
	q := New(f.pool)
	serverKey := uuid.NullUUID{UUID: toolsetID, Valid: true}
	require.NoError(t, q.SeedMetadataFixture(t.Context(), SeedMetadataFixtureParams{ID: uuid.New(), ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, ProjectID: f.projectID}))
	require.NoError(t, q.SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: uuid.New(), PluginID: rows.pluginID, ToolsetID: uuid.NullUUID{}, McpServerID: serverKey, DisplayName: "twin"}))
	require.NoError(t, q.SeedAssistantMcpServerFixture(t.Context(), SeedAssistantMcpServerFixtureParams{ID: uuid.New(), AssistantID: rows.assistantID, McpServerID: toolsetID, ProjectID: f.projectID}))
	before := f.dependentState(t)

	report := f.run(t, moveOptions(true))

	require.Len(t, report.Outcomes, 1)
	require.Equal(t, 3, report.Outcomes[OutcomeBlockedTwin])
	require.Zero(t, report.Writes)
	require.Equal(t, tableMcpMetadata, dependentRowFor(t, report, rows.metadataID).Table)
	require.Equal(t, before, f.dependentState(t))
	require.Empty(t, f.state(t).audits)
}

func TestMoveDependentsBlocksAssistantAttachmentsTheWritePathRejects(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	disabled := f.seedToolset(t, toolsetSpec{mcpSlug: "disabled-wrapper", public: true, enabled: false, domainID: uuid.NullUUID{}})
	f.seedServer(t, disabled, disabled, "disabled", "disabled-wrapper", "disabled")
	f.seedEndpoint(t, disabled, uuid.NullUUID{}, "disabled-wrapper")
	disabledRows := f.seedDependents(t, disabled)
	domain := f.seedDomain(t, false)
	domainOnly := f.seedToolset(t, toolsetSpec{mcpSlug: "domain-only", public: true, enabled: true, domainID: domain})
	f.seedServer(t, domainOnly, domainOnly, "domain only", "domain-only", "public")
	f.seedEndpoint(t, domainOnly, domain, "domain-only")
	domainRows := f.seedDependents(t, domainOnly)

	report := f.run(t, moveOptions(true))

	require.Equal(t, OutcomeBlockedUnattachable, dependentRowFor(t, report, disabledRows.attachmentID).Outcome)
	require.Equal(t, OutcomeBlockedUnattachable, dependentRowFor(t, report, domainRows.attachmentID).Outcome)
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, disabledRows.metadataID).Outcome)
	require.Equal(t, OutcomeMoved, dependentRowFor(t, report, domainRows.memberID).Outcome)
	state := f.dependentState(t)
	require.Len(t, state.toolsets, 2)
	require.Empty(t, state.servers)
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
	report := f.run(t, moveOptions(true))

	require.Zero(t, report.Writes)
	require.Equal(t, 6, report.Outcomes[OutcomeOrphaned])
	require.Equal(t, 3, report.Outcomes[OutcomeBlockedNoWrapper])
	require.Equal(t, "toolset deleted", dependentRowFor(t, report, deletedRows.memberID).Reason)
	require.Equal(t, "project deleted", dependentRowFor(t, report, deletedProjectRows.attachmentID).Reason)
	require.Equal(t, OutcomeBlockedNoWrapper, dependentRowFor(t, report, unwrappedRows.metadataID).Outcome)
	require.Equal(t, before, f.dependentState(t))
	require.Equal(t, beforeDeletedProject, deletedProject.dependentState(t))
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
	opts := moveOptions(true)
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

func TestMoveDependentsStopsOnDBErrorWithoutAdvancingCursor(t *testing.T) {
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

	config := f.pool.Config().Copy()
	config.ConnConfig.RuntimeParams["lock_timeout"] = "200ms"
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	report, err := NewRunner(pool, moveOptions(true)).Run(t.Context())
	require.Error(t, err)
	require.True(t, isTransient(err), "lock timeout must stop the run: %v", err)
	require.Equal(t, uuid.Nil, report.LastCursor, "the toolset is not done, so the cursor must not pass it")
	require.NoError(t, holder.Rollback(t.Context()))
	after := f.dependentState(t)
	require.Equal(t, before.metadata, after.metadata)
}
