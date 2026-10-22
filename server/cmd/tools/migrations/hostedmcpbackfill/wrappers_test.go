package hostedmcpbackfill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/roledelivery"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func wrapperOptions(apply bool) (Options, *memJournal) {
	journal := &memJournal{}
	return Options{ProjectID: uuid.NullUUID{}, Cursor: uuid.Nil, Limit: 0, PageSize: 0, Apply: apply, DeleteOrphanWrappers: true, Journal: journal}, journal
}

// wrapperState is every row the delete can change in the project, keyed by table and id.
func (f *fixture) wrapperState(t *testing.T) map[string]string {
	t.Helper()
	rows, err := New(f.pool).ListWrapperStateFixture(t.Context(), f.projectID)
	require.NoError(t, err)
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.TableName+" "+row.ID.String()] = row.RowText
	}
	return out
}

// seedExtraServer seeds a live non-canonical server of the toolset, as a gateway member wrapper.
func (f *fixture) seedExtraServer(t *testing.T, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	slug := "member-" + uuid.NewString()[:8]
	f.seedServer(t, id, toolsetID, slug, slug, "private")
	return id
}

type wrapperDependents struct {
	membership, pluginMember, attachment, link uuid.UUID
}

// seedWrapperDependents gives the server a gateway membership, a plugin member, an assistant attachment and a servable session client link.
func (f *fixture) seedWrapperDependents(t *testing.T, serverID uuid.UUID) wrapperDependents {
	t.Helper()
	ctx := t.Context()
	q := New(f.pool)
	issuer, gateway, client := uuid.New(), uuid.New(), uuid.New()
	require.NoError(t, q.SeedUserSessionIssuerFixture(ctx, SeedUserSessionIssuerFixtureParams{ID: issuer, ProjectID: key(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, Slug: "issuer-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedGatewayFixture(ctx, SeedGatewayFixtureParams{ID: gateway, OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: key(issuer), Name: "gateway"}))
	d := wrapperDependents{membership: uuid.New(), pluginMember: uuid.New(), attachment: uuid.New(), link: uuid.New()}
	require.NoError(t, q.SeedGatewayMemberFixture(ctx, SeedGatewayMemberFixtureParams{ID: d.membership, ProjectID: f.projectID, MetaMcpServerID: gateway, McpServerID: serverID}))
	require.NoError(t, q.SeedPluginServerFixture(ctx, SeedPluginServerFixtureParams{ID: d.pluginMember, PluginID: f.seedPlugin(t), ToolsetID: uuid.NullUUID{}, McpServerID: key(serverID), DisplayName: "member"}))
	assistant := uuid.New()
	require.NoError(t, q.SeedAssistantFixture(ctx, SeedAssistantFixtureParams{ID: assistant, ProjectID: f.projectID, OrganizationID: f.orgID, Name: "assistant-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedAssistantMcpServerFixture(ctx, SeedAssistantMcpServerFixtureParams{ID: d.attachment, AssistantID: assistant, McpServerID: serverID, ProjectID: f.projectID}))
	require.NoError(t, q.SeedSessionClientFixture(ctx, SeedSessionClientFixtureParams{ID: client, ProjectID: f.projectID, Slug: "rs-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedSessionClientIssuerLinkFixture(ctx, SeedSessionClientIssuerLinkFixtureParams{RemoteSessionClientID: client, UserSessionIssuerID: issuer}))
	require.NoError(t, q.SeedSessionClientLinkFixture(ctx, SeedSessionClientLinkFixtureParams{ID: d.link, ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, RemoteSessionClientID: client, ClientAttachmentScope: "project:" + f.projectID.String()}))
	return d
}

// seedUnservableLink gives a live server a link no issuer serves, which any server delete's resync prunes.
func (f *fixture) seedUnservableLink(t *testing.T, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	q := New(f.pool)
	client, link := uuid.New(), uuid.New()
	require.NoError(t, q.SeedSessionClientFixture(t.Context(), SeedSessionClientFixtureParams{ID: client, ProjectID: f.projectID, Slug: "rs-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedSessionClientLinkFixture(t.Context(), SeedSessionClientLinkFixtureParams{ID: link, ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: serverID, RemoteSessionClientID: client, ClientAttachmentScope: "project:" + f.projectID.String()}))
	return link
}

type wrapperSeed struct {
	f                         *fixture
	eligible                  uuid.UUID
	deps                      wrapperDependents
	staleLink                 uuid.UUID
	want                      map[uuid.UUID]Outcome
	liveToolsetServer         uuid.UUID
	deletedProject, elsewhere *fixture
}

// seedOrphanWrappers seeds one server per outcome, plus servers the mode never lists.
func seedOrphanWrappers(t *testing.T) wrapperSeed {
	t.Helper()
	f := newFixture(t)
	q := New(f.pool)
	ctx := t.Context()

	gone := f.seedWrappedToolset(t, "gone-"+uuid.NewString()[:8])
	eligible := f.seedExtraServer(t, gone)
	deps := f.seedWrapperDependents(t, eligible)
	addressable := f.seedExtraServer(t, gone)
	f.seedEndpoint(t, addressable, uuid.NullUUID{}, "addressable-"+uuid.NewString()[:8])
	issuerOwner := f.seedExtraServer(t, gone)
	issuer := uuid.New()
	require.NoError(t, q.SeedUserSessionIssuerFixture(ctx, SeedUserSessionIssuerFixtureParams{ID: issuer, ProjectID: key(f.projectID), OrganizationID: pgtype.Text{String: f.orgID, Valid: true}, Slug: "owned-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SetServerIssuerFixture(ctx, SetServerIssuerFixtureParams{UserSessionIssuerID: key(issuer), ID: issuerOwner, ProjectID: f.projectID}))
	policyTarget := f.seedExtraServer(t, gone)
	require.NoError(t, q.SeedServerRiskPolicyFixture(ctx, SeedServerRiskPolicyFixtureParams{ID: uuid.New(), ProjectID: f.projectID, OrganizationID: f.orgID, McpServerID: policyTarget.String()}))
	f.deleteToolset(t, gone)

	// A deleted toolset whose canonical server was never tombstoned.
	canonicalKept := f.seedWrappedToolset(t, "kept-"+uuid.NewString()[:8])
	require.NoError(t, q.SoftDeleteToolsetFixture(ctx, SoftDeleteToolsetFixtureParams{ID: canonicalKept, ProjectID: f.projectID}))

	liveToolset := f.seedWrappedToolset(t, "live-"+uuid.NewString()[:8])
	liveToolsetServer := f.seedExtraServer(t, liveToolset)
	staleLink := f.seedUnservableLink(t, liveToolset)

	deletedProject := newFixtureIn(t, f.pool)
	inDeletedProject := deletedProject.seedWrappedToolset(t, "in-deleted-project-"+uuid.NewString()[:8])
	deletedProject.seedExtraServer(t, inDeletedProject)
	deletedProject.deleteToolset(t, inDeletedProject)
	require.NoError(t, q.SoftDeleteProjectFixture(ctx, SoftDeleteProjectFixtureParams{ID: deletedProject.projectID, OrganizationID: deletedProject.orgID}))

	elsewhere := newFixtureIn(t, f.pool)
	elsewhereToolset := elsewhere.seedWrappedToolset(t, "elsewhere-"+uuid.NewString()[:8])
	elsewhere.seedExtraServer(t, elsewhereToolset)
	elsewhere.deleteToolset(t, elsewhereToolset)

	return wrapperSeed{
		f: f, eligible: eligible, deps: deps, staleLink: staleLink, liveToolsetServer: liveToolsetServer, deletedProject: deletedProject, elsewhere: elsewhere,
		want: map[uuid.UUID]Outcome{
			eligible: OutcomeDeleted, addressable: OutcomeBlockedLiveEndpoint, issuerOwner: OutcomeBlockedSessionIssuer,
			policyTarget: OutcomeBlockedRiskPolicy, canonicalKept: OutcomeSkipped,
		},
	}
}

func (s wrapperSeed) projectOptions(apply bool) (Options, *memJournal) {
	opts, journal := wrapperOptions(apply)
	opts.ProjectID = key(s.f.projectID)
	return opts, journal
}

func TestDeleteOrphanWrapperServersDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	s := seedOrphanWrappers(t)
	require.NoError(t, New(s.f.pool).SeedPluginGithubConnectionFixture(t.Context(), s.f.projectID))
	before, audits := s.f.wrapperState(t), s.f.state(t).audits
	opts, journal := s.projectOptions(false)
	opts.Publication = plugins.PublicationRequests{Enabled: true}

	report := s.f.run(t, opts)

	require.Equal(t, "dry-run", report.Mode)
	require.Equal(t, phaseWrapperDel, report.Phase)
	require.Equal(t, 1, report.Outcomes[OutcomeWouldDelete])
	require.Equal(t, 1, report.Writes)
	for id, outcome := range s.want {
		if outcome == OutcomeDeleted {
			outcome = OutcomeWouldDelete
		}
		require.Equal(t, outcome, dependentRowFor(t, report, id).Outcome, id.String())
	}
	row := dependentRowFor(t, report, s.eligible)
	require.Equal(t, &WrapperDetached{GatewayMemberships: 1, PluginMembers: 1, AssistantAttachments: 1, SessionClientLinks: 1, OtherSessionClientLinks: 1}, row.Detached)
	require.Equal(t, before, s.f.wrapperState(t))
	require.Equal(t, audits, s.f.state(t).audits)
	require.Empty(t, journal.records)
	require.False(t, row.PublicationRequested)
	outbox, err := New(s.f.pool).CountOutboxFixture(t.Context(), s.f.orgID)
	require.NoError(t, err)
	require.Zero(t, outbox, "a dry run with publication emit writes no outbox row")
}

func TestDeleteOrphanWrapperServersApplyRequiresJournal(t *testing.T) {
	t.Parallel()
	s := seedOrphanWrappers(t)
	before := s.f.wrapperState(t)
	opts, _ := s.projectOptions(true)
	opts.Journal = nil

	_, err := NewRunner(s.f.pool, opts).Run(t.Context())
	require.ErrorContains(t, err, "journal")
	require.Equal(t, before, s.f.wrapperState(t))
}

func TestDeleteOrphanWrapperServersOutcomes(t *testing.T) {
	t.Parallel()
	s := seedOrphanWrappers(t)
	f := s.f
	require.NoError(t, New(f.pool).SeedPluginGithubConnectionFixture(t.Context(), f.projectID))
	before, elsewhereBefore, deletedProjectBefore := f.wrapperState(t), s.elsewhere.wrapperState(t), s.deletedProject.wrapperState(t)
	opts, journal := s.projectOptions(true)
	opts.Publication = plugins.PublicationRequests{Enabled: true}

	report := f.run(t, opts)
	unfilteredOpts, _ := wrapperOptions(false)
	for _, row := range f.run(t, unfilteredOpts).Rows {
		require.NotEqual(t, s.liveToolsetServer, *row.RowID, "a live toolset's server is never listed")
		require.NotEqual(t, s.deletedProject.projectID, row.ProjectID, "a deleted project is never listed")
	}
	require.Equal(t, deletedProjectBefore, s.deletedProject.wrapperState(t))

	require.Equal(t, phaseWrapperDel, report.Phase)
	require.Len(t, report.Rows, len(s.want), "live toolsets and deleted projects are never listed")
	for id, outcome := range s.want {
		require.Equal(t, outcome, dependentRowFor(t, report, id).Outcome, id.String())
	}
	byReason := map[Outcome][]string{}
	for _, row := range report.Rows {
		byReason[row.Outcome] = append(byReason[row.Outcome], row.Reason)
	}
	require.Equal(t, []string{"toolset deleted"}, byReason[OutcomeDeleted])
	require.Equal(t, []string{reasonCanonicalServer}, byReason[OutcomeSkipped])
	require.Equal(t, []string{reasonLiveEndpoint}, byReason[OutcomeBlockedLiveEndpoint])
	require.Equal(t, []string{reasonSessionIssuer}, byReason[OutcomeBlockedSessionIssuer])
	require.Equal(t, []string{reasonRiskPolicy}, byReason[OutcomeBlockedRiskPolicy])
	require.Equal(t, 1, report.Writes)
	deleted := dependentRowFor(t, report, s.eligible)
	require.Equal(t, &WrapperDetached{GatewayMemberships: 1, PluginMembers: 1, AssistantAttachments: 1, SessionClientLinks: 1, OtherSessionClientLinks: 1}, deleted.Detached)
	require.True(t, deleted.PublicationRequested)

	after := f.wrapperState(t)
	changed := map[string]bool{
		"mcp_servers " + s.eligible.String():                        true,
		"meta_mcp_server_members " + s.deps.membership.String():     true,
		"plugin_servers " + s.deps.pluginMember.String():            true,
		"assistant_mcp_servers " + s.deps.attachment.String():       true,
		"mcp_server_remote_session_clients " + s.deps.link.String(): true,
		"mcp_server_remote_session_clients " + s.staleLink.String(): true,
	}
	for k, image := range before {
		if !changed[k] {
			require.Equal(t, image, after[k], "untouched: "+k)
		}
	}
	for _, k := range []string{"mcp_servers " + s.eligible.String(), "meta_mcp_server_members " + s.deps.membership.String(), "plugin_servers " + s.deps.pluginMember.String()} {
		var image map[string]any
		require.NoError(t, json.Unmarshal([]byte(after[k]), &image))
		require.NotNil(t, image["deleted_at"], k)
	}
	for _, k := range []string{"assistant_mcp_servers " + s.deps.attachment.String(), "mcp_server_remote_session_clients " + s.deps.link.String(), "mcp_server_remote_session_clients " + s.staleLink.String()} {
		require.NotContains(t, after, k)
	}
	require.Len(t, after, len(before)-3)
	require.Equal(t, elsewhereBefore, s.elsewhere.wrapperState(t), "the -project filter leaves other projects alone")

	actions := []string{}
	for _, entry := range f.state(t).audits {
		require.Equal(t, WrapperCleanupActorComponent, entry.ActorID)
		require.Equal(t, string(urn.PrincipalTypeSystem), entry.ActorType)
		actions = append(actions, entry.Action)
	}
	require.ElementsMatch(t, []string{string(audit.ActionMcpServerDelete), string(audit.ActionMetaMcpServerRemoveMember), string(audit.ActionPluginServerRemove)}, actions)
	outbox, err := New(f.pool).CountOutboxFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	require.EqualValues(t, 1, outbox)

	committed := journal.committed()
	require.Len(t, journal.records, 2)
	record := committed[s.eligible]
	require.Equal(t, OutcomeDeleted, record.Outcome)
	require.Equal(t, RowKey{Table: tableMcpServers, Column: "toolset_id", ID: record.ToolsetID}, record.From)
	require.Equal(t, tableMcpServers, record.Removed.Table)
	require.NotEmpty(t, record.Removed.SoftDeletedAt)
	require.NotEmpty(t, record.Removed.Row)
	require.Len(t, record.Dependents.GatewayMemberships, 1)
	require.Len(t, record.Dependents.PluginMembers, 1)
	require.Len(t, record.Dependents.AssistantAttachments, 1)
	require.Len(t, record.Dependents.SessionClientLinks, 1)
	require.Equal(t, s.deps.link, record.Dependents.SessionClientLinks[0].ID, "another server's pruned link is not journaled")
	require.NotEmpty(t, record.Dependents.PluginMembers[0].SoftDeletedAt)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "display_name", "the report carries no row images")

	rerunOpts, rerunJournal := s.projectOptions(true)
	rerun := f.run(t, rerunOpts)
	require.Zero(t, rerun.Writes)
	require.Zero(t, rerun.Outcomes[OutcomeDeleted])
	require.Empty(t, rerunJournal.records)
	require.Equal(t, after, f.wrapperState(t))
}

func TestDeleteOrphanWrapperServersProjectFilterAndLimit(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	first, second := f.seedWrappedToolset(t, "first-"+uuid.NewString()[:8]), f.seedWrappedToolset(t, "second-"+uuid.NewString()[:8])
	for _, id := range []uuid.UUID{first, second} {
		f.seedExtraServer(t, id)
		f.deleteToolset(t, id)
	}
	other := newFixtureIn(t, f.pool)
	elsewhere := other.seedWrappedToolset(t, "elsewhere-"+uuid.NewString()[:8])
	other.seedExtraServer(t, elsewhere)
	other.deleteToolset(t, elsewhere)
	otherBefore := other.wrapperState(t)
	opts, _ := wrapperOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Limit = 1

	report := f.run(t, opts)
	require.Equal(t, 1, report.Toolsets)
	require.Equal(t, 1, report.Outcomes[OutcomeDeleted])

	opts, _ = wrapperOptions(true)
	opts.ProjectID = key(f.projectID)
	opts.Cursor = report.LastCursor
	resumed := f.run(t, opts)
	require.Equal(t, 1, resumed.Outcomes[OutcomeDeleted])
	require.Equal(t, otherBefore, other.wrapperState(t))
}

// The mode and the app's DeleteMcpServer leave identical fixtures in the same state, apart from the audit actor.
func TestDeleteOrphanWrapperServersMatchesDeleteMcpServer(t *testing.T) {
	t.Parallel()
	pool, err := testInfra.CloneTestDatabase(t, "hostedmcpbackfill")
	require.NoError(t, err)
	// roles names each seeded row by what it is, so two fixtures with different ids compare row by row.
	seed := func() (*fixture, uuid.UUID, map[uuid.UUID]string) {
		f := newFixtureIn(t, pool)
		require.NoError(t, New(pool).SeedPluginGithubConnectionFixture(t.Context(), f.projectID))
		toolsetID := f.seedWrappedToolset(t, "same-"+uuid.NewString()[:8])
		server := f.seedExtraServer(t, toolsetID)
		deps := f.seedWrapperDependents(t, server)
		unservable := f.seedUnservableLink(t, toolsetID)
		f.deleteToolset(t, toolsetID)
		roles := map[uuid.UUID]string{
			toolsetID: "canonical_server", server: "member_server", deps.membership: "gateway_membership",
			deps.pluginMember: "plugin_member", deps.attachment: "assistant_attachment", deps.link: "session_client_link", unservable: "unservable_link",
		}
		for _, endpoint := range f.state(t).endpoints {
			roles[endpoint.ID] = "endpoint_of_" + roles[endpoint.McpServerID.UUID]
		}
		return f, server, roles
	}
	app, appServer, appRoles := seed()
	tool, _, toolRoles := seed()
	appBefore := rowStates(t, app, appRoles)
	require.Equal(t, appBefore, rowStates(t, tool, toolRoles), "both fixtures start identical")

	logger, tracerProvider := testenv.NewLogger(t), testenv.NewTracerProvider(t)
	engine := authz.NewEngine(logger, pool, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := mcpservers.NewService(logger, tracerProvider, pool, &sessions.Manager{}, engine, audit.NewLogger(audit.WithoutOutbox()), nil, nil, false, nil, nil, networkaccess.DenyAllChecker{}).WithPublicationRequests(true)
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: app.orgID, UserID: "user_fixture", ProjectID: &app.projectID})
	require.NoError(t, svc.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{ID: appServer.String()}))

	opts, _ := wrapperOptions(true)
	opts.ProjectID = key(tool.projectID)
	opts.Publication = plugins.PublicationRequests{Enabled: true}
	require.Equal(t, 1, tool.run(t, opts).Outcomes[OutcomeDeleted])

	want := map[string]string{
		"canonical_server":             "deleted_at=true deleted=true",
		"member_server":                "deleted_at=true deleted=true",
		"endpoint_of_canonical_server": "deleted_at=false deleted=false",
		"gateway_membership":           "deleted_at=true deleted=true",
		"plugin_member":                "deleted_at=true deleted=true",
		"assistant_attachment":         "absent",
		"session_client_link":          "absent",
		"unservable_link":              "absent",
	}
	require.Equal(t, "deleted_at=false deleted=false", appBefore["member_server"])
	require.Equal(t, want, rowStates(t, app, appRoles), "the app removes every dependent, both session client links and the attachment")
	require.Equal(t, want, rowStates(t, tool, toolRoles))
	actions := func(f *fixture) []string {
		out := []string{}
		for _, entry := range f.state(t).audits {
			out = append(out, entry.Action)
		}
		return out
	}
	require.Contains(t, actions(app), string(audit.ActionMcpServerDelete))
	require.ElementsMatch(t, actions(app), actions(tool))
	for _, f := range []*fixture{app, tool} {
		outbox, err := New(pool).CountOutboxFixture(t.Context(), f.orgID)
		require.NoError(t, err)
		require.EqualValues(t, 1, outbox)
	}
}

// rowStates maps each seeded row's role to its deletion state, with timestamps reduced to set or unset; any other row fails.
func rowStates(t *testing.T, f *fixture, roles map[uuid.UUID]string) map[string]string {
	t.Helper()
	out := make(map[string]string, len(roles))
	for _, role := range roles {
		out[role] = "absent"
	}
	for k, image := range f.wrapperState(t) {
		id := uuid.MustParse(strings.SplitN(k, " ", 2)[1])
		role, ok := roles[id]
		require.True(t, ok, "unexpected row %s", k)
		var row map[string]any
		require.NoError(t, json.Unmarshal([]byte(image), &row))
		state := fmt.Sprintf("deleted_at=%t", row["deleted_at"] != nil)
		if deleted, ok := row["deleted"]; ok {
			state += fmt.Sprintf(" deleted=%v", deleted)
		}
		out[role] = state
	}
	return out
}

// TestRestoreOrphanWrapperServersJQReplaysCommittedRows runs restore-orphan-wrapper-servers.jq against a real journal.
func TestRestoreOrphanWrapperServersJQReplaysCommittedRows(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "restore-"+uuid.NewString()[:8])
	committedServer, intentServer := f.seedExtraServer(t, toolsetID), f.seedExtraServer(t, toolsetID)
	f.seedWrapperDependents(t, committedServer)
	f.seedWrapperDependents(t, intentServer)
	neighbourLink := f.seedUnservableLink(t, f.seedWrappedToolset(t, "neighbour-"+uuid.NewString()[:8]))
	f.deleteToolset(t, toolsetID)
	before := f.wrapperState(t)
	opts, journal := wrapperOptions(true)
	require.Equal(t, 2, f.run(t, opts).Outcomes[OutcomeDeleted])
	deleted := f.wrapperState(t)

	// The intent server keeps only its intent, as when the journal write after its commit fails.
	kept := []JournalRecord{}
	for _, record := range journal.records {
		if record.Event != JournalCommitted || record.RowID != intentServer {
			kept = append(kept, record)
		}
	}
	journalPath := writeJournal(t, kept)
	replay := func(project uuid.UUID) string { return f.replayWrapperRestore(t, journalPath, project) }

	require.Empty(t, strings.TrimSpace(replay(uuid.New())), "another project's restore is empty")
	require.Equal(t, deleted, f.wrapperState(t))
	script := replay(f.projectID)
	require.Contains(t, script, "-- intent without outcome: mcp_servers "+intentServer.String()+"; check by hand\nUPDATE mcp_servers")
	require.Equal(t, 2, strings.Count(script, "UPDATE mcp_servers"))
	neighbourKey := "mcp_server_remote_session_clients " + neighbourLink.String()
	require.Contains(t, before, neighbourKey)
	delete(before, neighbourKey)
	require.Equal(t, before, f.wrapperState(t), "the restore brings back the deleted servers' rows, not another server's pruned link, and a second replay is a no-op")
}

// A policy that also targets a live server survives; the delete drops the wrapper from its scope, and the restore puts it back.
func TestDeleteOrphanWrapperServersJournalsSharedPolicyScope(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	q := New(f.pool)
	toolsetID := f.seedWrappedToolset(t, "scoped-"+uuid.NewString()[:8])
	wrapper := f.seedExtraServer(t, toolsetID)
	liveServer := f.seedWrappedToolset(t, "live-"+uuid.NewString()[:8])
	policy := uuid.New()
	require.NoError(t, q.SeedSharedRiskPolicyFixture(ctx, SeedSharedRiskPolicyFixtureParams{ID: policy, ProjectID: f.projectID, OrganizationID: f.orgID, FirstServerID: wrapper.String(), SecondServerID: liveServer.String()}))
	f.deleteToolset(t, toolsetID)
	before, err := q.GetRiskPolicyScopeFixture(ctx, GetRiskPolicyScopeFixtureParams{ID: policy, ProjectID: f.projectID})
	require.NoError(t, err)

	opts, journal := wrapperOptions(true)
	report := f.run(t, opts)
	row := dependentRowFor(t, report, wrapper)
	require.Equal(t, OutcomeDeleted, row.Outcome)
	require.Equal(t, 1, row.Detached.RiskPolicyScopes)
	var images []PolicyScopeImage
	for _, record := range journal.records {
		if record.Event == JournalIntent && record.Dependents != nil {
			images = append(images, record.Dependents.RiskPolicyScopes...)
		}
	}
	require.Len(t, images, 1)
	require.True(t, images[0].UpdatedAtAfter.After(images[0].UpdatedAtBefore))
	after, err := q.GetRiskPolicyScopeFixture(ctx, GetRiskPolicyScopeFixtureParams{ID: policy, ProjectID: f.projectID})
	require.NoError(t, err)
	require.JSONEq(t, `{"servers": [{"mcp_server_id": "`+liveServer.String()+`"}]}`, after.Scope)
	require.Equal(t, before.Version, after.Version)

	f.replayWrapperRestore(t, writeJournal(t, journal.records), f.projectID)
	restored, err := q.GetRiskPolicyScopeFixture(ctx, GetRiskPolicyScopeFixtureParams{ID: policy, ProjectID: f.projectID})
	require.NoError(t, err)
	require.JSONEq(t, before.Scope, restored.Scope, "the restore puts the wrapper back in the policy scope")
}

// A save after the backfill that leaves the same scope JSON still stops the restore from overwriting it.
func TestRestoreOrphanWrapperServersJQSkipsPolicyChangedAfterBackfill(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	q := New(f.pool)
	toolsetID := f.seedWrappedToolset(t, "touched-"+uuid.NewString()[:8])
	wrapper := f.seedExtraServer(t, toolsetID)
	liveServer := f.seedWrappedToolset(t, "live-"+uuid.NewString()[:8])
	policy := uuid.New()
	require.NoError(t, q.SeedSharedRiskPolicyFixture(ctx, SeedSharedRiskPolicyFixtureParams{ID: policy, ProjectID: f.projectID, OrganizationID: f.orgID, FirstServerID: wrapper.String(), SecondServerID: liveServer.String()}))
	f.deleteToolset(t, toolsetID)

	opts, journal := wrapperOptions(true)
	require.Equal(t, 1, f.run(t, opts).Outcomes[OutcomeDeleted])
	pruned, err := q.GetRiskPolicyScopeFixture(ctx, GetRiskPolicyScopeFixtureParams{ID: policy, ProjectID: f.projectID})
	require.NoError(t, err)
	require.NoError(t, q.TouchRiskPolicyFixture(ctx, TouchRiskPolicyFixtureParams{ID: policy, ProjectID: f.projectID}))

	f.replayWrapperRestore(t, writeJournal(t, journal.records), f.projectID)
	after, err := q.GetRiskPolicyScopeFixture(ctx, GetRiskPolicyScopeFixtureParams{ID: policy, ProjectID: f.projectID})
	require.NoError(t, err)
	require.JSONEq(t, pruned.Scope, after.Scope, "a policy saved after the backfill keeps its scope")
}

// Deleting the wrapper first lets the leftover delete remove the record that was blocked_live_wrapper, and role delivery re-adds nothing.
func TestDeleteOrphanWrapperServersUnblocksLeftoverRemovalRecords(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	toolsetID := f.seedWrappedToolset(t, "unblock-"+uuid.NewString()[:8])
	f.seedExtraServer(t, toolsetID)
	removedFrom, control := f.seedPlugin(t), f.seedPlugin(t)
	record := f.seedRemoval(t, removedFrom, key(toolsetID), uuid.NullUUID{}, "removed")
	f.deleteToolset(t, toolsetID)

	role := "role:organization:" + uuid.NewString()
	principal, err := urn.ParsePrincipal(role)
	require.NoError(t, err)
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, toolsetID.String()).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.pool).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(authz.ScopeMCPConnect), Selectors: selectors})
	require.NoError(t, err)

	leftoverOpts, _ := leftoverOptions(true)
	require.Equal(t, OutcomeBlockedLiveWrapper, dependentRowFor(t, f.run(t, leftoverOpts), record).Outcome)

	wrapperOpts, _ := wrapperOptions(true)
	require.Equal(t, 1, f.run(t, wrapperOpts).Outcomes[OutcomeDeleted])

	leftoverOpts, _ = leftoverOptions(true)
	leftovers := f.run(t, leftoverOpts)
	require.Equal(t, OutcomeDeleted, dependentRowFor(t, leftovers, record).Outcome)
	require.Equal(t, "toolset deleted", dependentRowFor(t, leftovers, record).Reason)
	require.Empty(t, f.removals(t))

	for _, plugin := range []uuid.UUID{removedFrom, control} {
		tx, err := f.pool.Begin(ctx) //nolint:glint // notestingrawsql: transaction boundary for the role-delivery setup under test.
		require.NoError(t, err)
		changed, err := roledelivery.Populate(ctx, tx, f.orgID, f.projectID, plugin, role, nil)
		require.NoError(t, err)
		require.False(t, changed, "no live server of the toolset is left to deliver")
		members, err := pluginsrepo.New(tx).ListPluginServers(ctx, plugin)
		require.NoError(t, err)
		require.Empty(t, members)
		require.NoError(t, tx.Rollback(context.Background()))
	}
}

// The delete rechecks under its locks what the listing saw.
func TestDeleteOrphanWrapperServersRechecksUnderLock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := t.Context()
	q := New(f.pool)
	revived := f.seedWrappedToolset(t, "revived-"+uuid.NewString()[:8])
	f.seedExtraServer(t, revived)
	f.deleteToolset(t, revived)
	moved := f.seedWrappedToolset(t, "moved-"+uuid.NewString()[:8])
	movedServer := f.seedExtraServer(t, moved)
	f.deleteToolset(t, moved)
	gone := f.seedWrappedToolset(t, "gone-"+uuid.NewString()[:8])
	goneServer := f.seedExtraServer(t, gone)
	f.deleteToolset(t, gone)

	listed := map[uuid.UUID]ListToolsetOrphanWrappersRow{}
	for _, toolsetID := range []uuid.UUID{revived, moved, gone} {
		rows, err := q.ListToolsetOrphanWrappers(ctx, ListToolsetOrphanWrappersParams{ToolsetID: toolsetID, ProjectID: key(f.projectID)})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		listed[toolsetID] = rows[0]
	}
	require.NoError(t, q.RestoreToolsetFixture(ctx, RestoreToolsetFixtureParams{ID: revived, ProjectID: f.projectID}))
	require.NoError(t, q.SetServerToolsetFixture(ctx, SetServerToolsetFixtureParams{ToolsetID: key(f.seedWrappedToolset(t, "other-"+uuid.NewString()[:8])), ID: movedServer, ProjectID: f.projectID}))
	require.NoError(t, q.SoftDeleteServerFixture(ctx, SoftDeleteServerFixtureParams{ID: goneServer, ProjectID: f.projectID}))
	before, audits := f.wrapperState(t), f.state(t).audits
	opts, journal := wrapperOptions(true)
	runner := NewRunner(f.pool, opts)

	for toolsetID, reason := range map[uuid.UUID]string{revived: reasonToolsetLive, moved: reasonRowChanged, gone: reasonRowChanged} {
		row, err := runner.deleteOrphanWrapperOne(ctx, toolsetID, listed[toolsetID])
		require.NoError(t, err)
		require.Equal(t, OutcomeSkipped, row.Outcome, toolsetID.String())
		require.Equal(t, reason, row.Reason, toolsetID.String())
		require.False(t, row.Wrote)
	}
	require.Equal(t, before, f.wrapperState(t))
	require.Equal(t, audits, f.state(t).audits)
	require.Empty(t, journal.records)
}

func TestDeleteOrphanWrapperServersIgnoresSoftDeletedEndpoints(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "retired-endpoint-"+uuid.NewString()[:8])
	server := f.seedExtraServer(t, toolsetID)
	endpoint := f.seedEndpoint(t, server, uuid.NullUUID{}, "retired-"+uuid.NewString()[:8])
	require.NoError(t, New(f.pool).SoftDeleteEndpointFixture(t.Context(), SoftDeleteEndpointFixtureParams{ID: endpoint, ProjectID: f.projectID}))
	f.deleteToolset(t, toolsetID)
	opts, _ := wrapperOptions(true)

	row := dependentRowFor(t, f.run(t, opts), server)
	require.Equal(t, OutcomeDeleted, row.Outcome)
	require.Zero(t, row.LiveEndpoints)
}

// A link another transaction deletes before the prune is not journaled, so a restore cannot bring it back.
func TestDeleteOrphanWrapperServersJournalsOnlyWhatThePruneDeleted(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "concurrent-"+uuid.NewString()[:8])
	server := f.seedExtraServer(t, toolsetID)
	deps := f.seedWrapperDependents(t, server)
	raced := f.seedUnservableLink(t, server)
	f.deleteToolset(t, toolsetID)
	listed, err := New(f.pool).ListToolsetOrphanWrappers(t.Context(), ListToolsetOrphanWrappersParams{ToolsetID: toolsetID, ProjectID: key(f.projectID)})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	opts, journal := wrapperOptions(true)
	runner := NewRunner(f.pool, opts)
	runner.beforeWrapperPrune = func(ctx context.Context) {
		require.NoError(t, New(f.pool).DeleteSessionClientLinkFixture(ctx, DeleteSessionClientLinkFixtureParams{ID: raced, ProjectID: f.projectID}))
	}

	row, err := runner.deleteOrphanWrapperOne(t.Context(), toolsetID, listed[0])
	require.NoError(t, err)
	require.Equal(t, OutcomeDeleted, row.Outcome)
	require.Equal(t, 1, row.Detached.SessionClientLinks)
	require.Zero(t, row.Detached.OtherSessionClientLinks)
	record := journal.committed()[server]
	require.Len(t, record.Dependents.SessionClientLinks, 1)
	require.Equal(t, deps.link, record.Dependents.SessionClientLinks[0].ID)
	require.NotContains(t, f.wrapperState(t), "mcp_server_remote_session_clients "+raced.String())
}

// The tool's prune must stay the app's PruneUnservableAssociations, with RETURNING added.
func TestPruneUnservableSessionClientLinksMatchesTheProductPrune(t *testing.T) {
	t.Parallel()
	body := func(path, name string) string {
		raw, err := os.ReadFile(path) // #nosec G304 -- fixed repository paths
		require.NoError(t, err)
		text := string(raw)
		start := strings.Index(text, "-- name: "+name+" ")
		require.GreaterOrEqual(t, start, 0, name)
		from := strings.Index(text[start:], "DELETE FROM")
		require.GreaterOrEqual(t, from, 0, name)
		text = text[start+from:]
		if before, _, ok := strings.Cut(text, "\nRETURNING"); ok {
			return before
		}
		before, _, ok := strings.Cut(text, ";")
		require.True(t, ok, name)
		return before
	}
	require.Equal(t,
		body("../../../../internal/mcpservers/upstreamclients/queries.sql", "PruneUnservableAssociations"),
		body("queries.sql", "PruneUnservableSessionClientLinks"))
}

// replayWrapperRestore runs restore-orphan-wrapper-servers.jq for the project and applies its SQL twice.
func (f *fixture) replayWrapperRestore(t *testing.T, journalPath string, project uuid.UUID) string {
	t.Helper()
	jq, err := exec.LookPath("jq")
	require.NoError(t, err, "jq is a mise tool; run the tests through mise")
	out, err := exec.CommandContext(t.Context(), jq, "-r", "-s", "--arg", "p", project.String(), "-f", "restore-orphan-wrapper-servers.jq", journalPath).Output() // #nosec G204 -- fixed jq program and test paths
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

func writeJournal(t *testing.T, records []JournalRecord) string {
	t.Helper()
	var lines bytes.Buffer
	for _, record := range records {
		line, err := json.Marshal(record)
		require.NoError(t, err)
		lines.Write(append(line, '\n'))
	}
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	require.NoError(t, os.WriteFile(path, lines.Bytes(), 0o600))
	return path
}

// A plugin entry whose name was taken while the server was deleted stays deleted, and the restore goes on.
func TestRestoreOrphanWrapperServersJQSkipsAPluginEntryNameClash(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	toolsetID := f.seedWrappedToolset(t, "clash-"+uuid.NewString()[:8])
	server := f.seedExtraServer(t, toolsetID)
	deps := f.seedWrapperDependents(t, server)
	f.deleteToolset(t, toolsetID)
	opts, journal := wrapperOptions(true)
	require.Equal(t, 1, f.run(t, opts).Outcomes[OutcomeDeleted])
	var member struct {
		PluginID    uuid.UUID `json:"plugin_id"`
		DisplayName string    `json:"display_name"`
	}
	require.NoError(t, json.Unmarshal(journal.committed()[server].Dependents.PluginMembers[0].Row, &member))
	taker := uuid.New()
	other := f.seedExtraServer(t, f.seedWrappedToolset(t, "taker-"+uuid.NewString()[:8]))
	require.NoError(t, New(f.pool).SeedPluginServerFixture(t.Context(), SeedPluginServerFixtureParams{ID: taker, PluginID: member.PluginID, ToolsetID: uuid.NullUUID{}, McpServerID: key(other), DisplayName: member.DisplayName}))

	script := f.replayWrapperRestore(t, writeJournal(t, journal.records), f.projectID)
	require.Contains(t, script, "-- plugin_servers "+deps.pluginMember.String()+" stays deleted if its plugin has a live entry with the same name or server; check by hand")
	after := f.wrapperState(t)
	var restored map[string]any
	require.NoError(t, json.Unmarshal([]byte(after["mcp_servers "+server.String()]), &restored))
	require.Nil(t, restored["deleted_at"], "the server comes back")
	var skipped map[string]any
	require.NoError(t, json.Unmarshal([]byte(after["plugin_servers "+deps.pluginMember.String()]), &skipped))
	require.NotNil(t, skipped["deleted_at"], "the clashing entry stays deleted")
}
