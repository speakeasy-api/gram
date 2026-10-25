package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// attemptWrites hears each connection attempt batch the telemetry logger
// committed, so a test reads the rows once they exist instead of polling.
type attemptWrites struct {
	batches chan int
}

func (w attemptWrites) OnTelemetryLogsWritten(_ context.Context, params []telemetry.LogParams) {
	if len(params) > 0 && params[0].Attributes[attr.EventURNKey] == mcp.MCPConnectionAttemptEventURN {
		w.batches <- len(params)
	}
}

// watchConnectionAttempts registers before the test sends traffic, as the
// logger requires of observers.
func watchConnectionAttempts(t *testing.T, ti *testInstance) attemptWrites {
	t.Helper()
	writes := attemptWrites{batches: make(chan int, 8)}
	ti.telemLogger.AddObserver(writes)
	return writes
}

// written waits for the next listing's attempt batch and returns its size.
func (w attemptWrites) written(t *testing.T) int {
	t.Helper()
	select {
	case n := <-w.batches:
		return n
	case <-time.After(10 * time.Second):
		require.FailNow(t, "the gateway did not record its listing attempts")
		return 0
	}
}

// connectionAttemptRow is one raw mcp_connection_attempt row of a gateway.
type connectionAttemptRow struct {
	ID           string `ch:"id"`
	ListingID    string `ch:"listing_id"`
	Stage        string `ch:"stage"`
	Outcome      string `ch:"outcome"`
	FailureClass string `ch:"failure_class"`
	Reason       string `ch:"reason"`
	MemberID     string `ch:"mcp_server_id"`
	Subject      string `ch:"subject"`
	ClientKey    string `ch:"client_key"`
	Calls        uint64 `ch:"calls"`
	Retries      uint64 `ch:"retries"`
	EventSource  string `ch:"event_source"`
	GramURN      string `ch:"gram_urn"`
	ToolName     string `ch:"tool_name"`
	TraceID      string `ch:"trace_id"`
	SpanID       string `ch:"span_id"`
	ChatID       string `ch:"chat_id"`
}

// connectionAttempts reads a gateway's committed attempt rows, listing first.
func connectionAttempts(t *testing.T, projectID, gatewayID uuid.UUID) []connectionAttemptRow {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var rows []connectionAttemptRow
	require.NoError(t, chConn.Select(t.Context(), &rows,
		`SELECT toString(attributes.gram.connection_attempt.id) AS id,
			        toString(attributes.gram.connection_attempt.listing_id) AS listing_id,
			        toString(attributes.gram.connection_attempt.stage) AS stage,
			        toString(attributes.gram.connection_attempt.outcome) AS outcome,
			        toString(attributes.gram.connection_attempt.failure_class) AS failure_class,
			        toString(attributes.gram.connection_attempt.reason) AS reason,
			        mcp_server_id,
			        toString(attributes.gram.connection_attempt.subject) AS subject,
			        toString(attributes.gram.connection_attempt.client_key) AS client_key,
			        toUInt64OrZero(toString(attributes.gram.connection_attempt.calls)) AS calls,
			        toUInt64OrZero(toString(attributes.gram.connection_attempt.retries)) AS retries,
			        event_source, gram_urn, tool_name,
			        toString(ifNull(trace_id, '')) AS trace_id,
			        toString(ifNull(span_id, '')) AS span_id,
			        ifNull(gram_chat_id, '') AS chat_id
			 FROM telemetry_logs
			 WHERE gram_project_id = ? AND event_urn = ? AND meta_mcp_server_id = ?
			 ORDER BY stage = 'discovery_listing' DESC, time_unix_nano, id`,
		projectID.String(), mcp.MCPConnectionAttemptEventURN, gatewayID.String()))
	return rows
}

// requireAttemptRowsAreNotUsage checks the properties that keep attempt rows
// out of tool, trace, and chat metrics: only a left-out member carries the
// gateway discovery labels, and no row carries a trace, span, or chat.
func requireAttemptRowsAreNotUsage(t *testing.T, rows []connectionAttemptRow) {
	t.Helper()
	for _, row := range rows {
		require.Empty(t, row.TraceID, "an attempt row never joins a trace")
		require.Empty(t, row.SpanID)
		require.Empty(t, row.ChatID)
		if row.Stage == "discovery_member" && row.Outcome == "failed" {
			require.Equal(t, "meta_discovery", row.EventSource)
			require.Equal(t, "tools/list", row.ToolName)
			continue
		}
		require.Empty(t, row.EventSource)
		require.Empty(t, row.GramURN)
		require.Empty(t, row.ToolName)
	}
}

// requireNoTraceOrToolUsage checks that a project with only gateway listings
// recorded no trace, tool call, or chat request.
func requireNoTraceOrToolUsage(t *testing.T, projectID uuid.UUID) {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	var traces uint64
	require.NoError(t, chConn.QueryRow(t.Context(), `SELECT count() FROM trace_summaries WHERE gram_project_id = ?`, projectID.String()).Scan(&traces))
	require.Zero(t, traces)
	var toolCalls, chatRequests uint64
	require.NoError(t, chConn.QueryRow(t.Context(),
		`SELECT countIfMerge(total_tool_calls), countIfMerge(total_chat_requests) FROM metrics_summaries WHERE gram_project_id = ?`,
		projectID.String()).Scan(&toolCalls, &chatRequests))
	require.Zero(t, toolCalls)
	require.Zero(t, chatRequests)
}

// attemptRollup returns a gateway's rolled-up member attempts, retries, and
// recoveries, keyed by outcome and reason.
func attemptRollup(t *testing.T, projectID, gatewayID uuid.UUID) map[string][3]uint64 {
	t.Helper()
	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	rows, err := chConn.Query(t.Context(),
		`SELECT concat(stage, '/', outcome, '/', reason), sum(attempts), sum(retries), sum(recovered)
		 FROM mcp_connection_attempt_summaries
		 WHERE gram_project_id = toUUID(?) AND server_kind = 'meta' AND server_id = ?
		 GROUP BY stage, outcome, reason`,
		projectID.String(), gatewayID.String())
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	totals := map[string][3]uint64{}
	for rows.Next() {
		var key string
		var attempts, retries, recovered uint64
		require.NoError(t, rows.Scan(&key, &attempts, &retries, &recovered))
		totals[key] = [3]uint64{attempts, retries, recovered}
	}
	require.NoError(t, rows.Err())
	return totals
}

func requireUUIDv7(t *testing.T, value string) {
	t.Helper()
	id, err := uuid.Parse(value)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), id.Version())
}

// memberAttemptsByOutcome groups a listing's member rows by outcome.
func memberAttemptsByOutcome(rows []connectionAttemptRow) map[string][]connectionAttemptRow {
	byOutcome := map[string][]connectionAttemptRow{}
	for _, row := range rows {
		if row.Stage == "discovery_member" {
			byOutcome[row.Outcome] = append(byOutcome[row.Outcome], row)
		}
	}
	return byOutcome
}

// attemptsAfter waits for the next listing's attempt batch, checks its size,
// and reads the gateway's rows.
func attemptsAfter(t *testing.T, writes attemptWrites, projectID, gatewayID uuid.UUID, want int) []connectionAttemptRow {
	t.Helper()
	require.Equal(t, want, writes.written(t))
	rows := connectionAttempts(t, projectID, gatewayID)
	require.Len(t, rows, want)
	return rows
}

// Members added after consent are recorded as waiting on consent only when
// the caller could use them once approved. A deleted or denied one is left
// out, as an approved one would be.
func TestGatewayDirectRecordsOnlyUsableUnapprovedMembers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	writes := watchConnectionAttempts(t, ti)
	auth, _ := contextvalues.GetAuthContext(ctx)
	_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
	slug := "unapproved-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
	source := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Source", 1, mcpservers.VisibilityPublic, "alpha")
	approvedGone := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Approved then deleted", 2, mcpservers.VisibilityPublic, "beta")
	connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)

	available := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Added", 3, mcpservers.VisibilityPublic, "gamma")
	deleted := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Added then deleted", 4, mcpservers.VisibilityPublic, "delta")
	denied := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Added private", 5, mcpservers.VisibilityPublic, "epsilon")
	setToolsetMcpPrivate(t, ctx, ti, denied.toolsetID, *auth.ProjectID)
	toolsets := toolsets_repo.New(ti.conn)
	for _, gone := range []uuid.UUID{approvedGone.toolsetID, deleted.toolsetID} {
		toolset, err := toolsets.GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{ID: gone, ProjectID: *auth.ProjectID})
		require.NoError(t, err)
		_, err = toolsets.DeleteToolset(ctx, toolsets_repo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: *auth.ProjectID})
		require.NoError(t, err)
	}

	caller := authztest.WithExactGrants(t, t.Context(), authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
	w, err := servePublicHTTP(t, caller, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	rows := attemptsAfter(t, writes, *auth.ProjectID, gateway.ID, 3)
	require.Equal(t, []string{"partial", "", ""}, []string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason})
	byMember := map[string]connectionAttemptRow{}
	for _, row := range rows[1:] {
		byMember[row.MemberID] = row
	}
	require.Equal(t, "listed", byMember[source.serverID.String()].Outcome)
	waiting := byMember[available.serverID.String()]
	require.Equal(t, []string{"consent_blocked", "configuration", "consent_not_approved"}, []string{waiting.Outcome, waiting.FailureClass, waiting.Reason})
	require.Len(t, byMember, 2, "deleted and denied members, approved or not, are not recorded")
}

func TestGatewayDirectRecordsOnlyLiveUnapprovedTunnels(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		deleted     bool
		wantRows    int
		wantOutcome string
	}{
		{name: "live", deleted: false, wantRows: 3, wantOutcome: "partial"},
		{name: "deleted", deleted: true, wantRows: 2, wantOutcome: "complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
			writes := watchConnectionAttempts(t, ti)
			auth, _ := contextvalues.GetAuthContext(ctx)
			_, issuer, client := seedPrivateToolsetWithIssuer(t, ctx, ti)
			slug := "unapproved-tunnel-" + uuid.NewString()
			gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *auth.ProjectID, auth.ActiveOrganizationID, slug, issuer.ID)
			source := seedHostedMetaMember(t, ctx, ti, gateway.ID, "Source", 1, mcpservers.VisibilityPublic, "alpha")
			connection := approveGatewayCatalog(t, ctx, ti, issuer.ID, client, gateway.ID, slug, toolfilter.CatalogModeCached, metamcp.DiscoveryModeDirect, nil)
			tunnelID, _ := seedTunneledMetaMember(t, ctx, ti, *auth.ProjectID, gateway.ID, "Added tunnel", "added-tunnel", 2, "")
			if tc.deleted {
				_, err := tunneledmcprepo.New(ti.conn).DeleteServer(ctx, tunneledmcprepo.DeleteServerParams{ID: tunnelID, ProjectID: *auth.ProjectID})
				require.NoError(t, err)
			}

			caller := authztest.WithExactGrants(t, t.Context(), authz.NewGrant(authz.ScopeMCPConnect, gateway.ID.String()))
			w, err := servePublicHTTP(t, caller, ti, slug, makeMetaRPCBody(t, "tools/list", map[string]any{}), connection.token, nil)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			rows := attemptsAfter(t, writes, *auth.ProjectID, gateway.ID, tc.wantRows)
			require.Equal(t, tc.wantOutcome, rows[0].Outcome)
			for _, row := range rows[1:] {
				if row.MemberID == source.serverID.String() {
					require.Equal(t, "listed", row.Outcome)
				} else {
					require.False(t, tc.deleted)
					require.Equal(t, []string{"consent_blocked", "configuration", "consent_not_approved"}, []string{row.Outcome, row.FailureClass, row.Reason})
				}
			}
		})
	}
}

// A listing that fails after describing still records what each member
// returned.
func TestGatewayDirectRecordsMembersOfAFailedInventory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	writes := watchConnectionAttempts(t, ti)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	slug := "gateway-" + uuid.NewString()
	gateway := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	// Both qualify to x--y--z, so the inventory is ambiguous.
	tool := func(name string) []any {
		return []any{map[string]any{"name": name, "inputSchema": map[string]any{"type": "object"}}}
	}
	first := newScriptedUpstream(t, tool("y--z"), 100)
	second := newScriptedUpstream(t, tool("z"), 100)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "First", "x", 1, first.url)
	seedMetaMemberWithUpstream(t, ctx, ti.conn, *authCtx.ProjectID, gateway.ID, "Second", "x--y", 2, second.url)
	setGatewayMode(t, ctx, ti, gateway.ID, "direct")
	listing := listDirectGateway(t, ti, slug, map[string]any{})
	require.NotNil(t, listing.Error)
	require.Contains(t, listing.Error.Message, "ambiguous tool name")

	rows := attemptsAfter(t, writes, *authCtx.ProjectID, gateway.ID, 3)
	require.Equal(t, []string{"failed", "configuration", "ambiguous_tool_name"}, []string{rows[0].Outcome, rows[0].FailureClass, rows[0].Reason})
	require.Equal(t, []string{"listed", "listed"}, []string{rows[1].Outcome, rows[2].Outcome})
}
