package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// recordedCheck is one authorization check a request recorded.
type recordedCheck struct {
	resourceID string
	outcome    string
}

// takeRecordedMCPConnectChecks returns the mcp:connect checks recorded since
// the last call, reading them from the outbox the engine writes them to.
func takeRecordedMCPConnectChecks(t *testing.T, ti *testInstance, seen map[int64]bool) []recordedCheck {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	checks := []recordedCheck{}
	for _, row := range rows {
		if seen[row.ID] || row.Topic != string(proto.MessageName(&authzv1.Challenge{})) {
			continue
		}
		seen[row.ID] = true
		message := &authzv1.Challenge{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		if message.GetScope() != string(authz.ScopeMCPConnect) {
			continue
		}
		checks = append(checks, recordedCheck{resourceID: message.GetResourceId(), outcome: message.GetOutcome()})
	}
	return checks
}

// recordChallenges makes the service record every authorization check.
func recordChallenges(t *testing.T, ti *testInstance) {
	t.Helper()
	ti.service.SetAuthzEngine(authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysEnabled, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
		AdmitWorkloadSession:             runtimepolicy.AdmitWorkloadSession,
		AdmitWorkloadSessionInTx:         runtimepolicy.AdmitWorkloadSessionInTx,
		DevMode:                          false,
	}))
}

// An agent gateway admits members by evaluating every private server in the
// organization. Recording each of those decisions listed every server the
// agent has no grant for as a denied check on every request. Only a call that
// names a server records its check, once per request.
func TestServeAgentGateway_RecordsChecksOnlyForNamedServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	recordChallenges(t, ti)
	fx := seedAgentGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	granted := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.granted).String()
	denied := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.denied).String()
	// A public server whose own toolset is private, which the agent holds no
	// grant on: admission keeps it among the denied members, so naming it
	// records its toolset's denial.
	decoy := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil)
	closed := seedHostedMetaMember(t, ctx, ti, decoy.ID, "closed hosted", 3, mcpservers.VisibilityPublic, "closed_tool")
	setToolsetMcpPrivate(t, ctx, ti, closed.toolsetID, *authCtx.ProjectID)
	seen := map[int64]bool{}

	call := func(name string, arguments map[string]any) string {
		t.Helper()
		w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{
			"name": name, "arguments": arguments,
		}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}

	// Listing names no server, so nothing is recorded, not even the denial
	// that kept the ungranted server off the list.
	call("list_servers", map[string]any{})
	require.Empty(t, takeRecordedMCPConnectChecks(t, ti, seen))

	// Naming the granted server records its allowed check once, though the
	// request checks it again to read its tools.
	call("describe_server", map[string]any{"server": fx.granted})
	require.Equal(t, []recordedCheck{{resourceID: granted, outcome: "allow"}}, takeRecordedMCPConnectChecks(t, ti, seen))

	// Naming the ungranted server records the real denial, once.
	require.Contains(t, call("describe_server", map[string]any{"server": fx.denied}), "unknown server")
	require.Equal(t, []recordedCheck{{resourceID: denied, outcome: "deny"}}, takeRecordedMCPConnectChecks(t, ti, seen))

	// One request naming both servers, the granted one twice, records one
	// check for each.
	call("describe_tools", map[string]any{"tools": []string{fx.granted + "--a", fx.granted + "--b", fx.denied + "--c"}})
	require.ElementsMatch(t, []recordedCheck{{resourceID: granted, outcome: "allow"}, {resourceID: denied, outcome: "deny"}}, takeRecordedMCPConnectChecks(t, ti, seen))

	// Naming the public server records the denial of its private toolset.
	require.Contains(t, call("describe_server", map[string]any{"server": closed.slug}), "unknown server")
	require.Equal(t, []recordedCheck{{resourceID: closed.toolsetID.String(), outcome: "deny"}}, takeRecordedMCPConnectChecks(t, ti, seen))
}

// A public server whose own toolset is private needs a grant on that toolset,
// which the caller here lacks. On every gateway mode a listing records nothing
// for it, and a call that names it records the toolset's denial once.
func TestGateways_RecordPrivateToolsetDenialOnlyWhenNamed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// serve seeds the gateway with member as a member and returns a
		// function that calls one of its meta tools and returns the body.
		serve func(t *testing.T, ctx context.Context, ti *testInstance, member func(metaID uuid.UUID) hostedMemberFixture) func(tool string, arguments map[string]any) string
	}{
		{name: "stored project gateway", serve: serveStoredGatewayWithoutGrants},
		{name: "organization gateway", serve: serveOrganizationGatewayAsMember},
		{name: "agent gateway", serve: serveAgentGatewayAsAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			recordChallenges(t, ti)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			var closed hostedMemberFixture
			call := tc.serve(t, ctx, ti, func(metaID uuid.UUID) hostedMemberFixture {
				closed = seedHostedMetaMember(t, ctx, ti, metaID, "closed hosted", 9, mcpservers.VisibilityPublic, "closed_tool")
				setToolsetMcpPrivate(t, ctx, ti, closed.toolsetID, *authCtx.ProjectID)
				return closed
			})
			denial := []recordedCheck{{resourceID: closed.toolsetID.String(), outcome: "deny"}}
			seen := map[int64]bool{}

			call("list_servers", map[string]any{})
			require.Empty(t, takeRecordedMCPConnectChecks(t, ti, seen), "a listing names no server")

			require.Contains(t, call("describe_server", map[string]any{"server": closed.slug}), "unknown server")
			require.Equal(t, denial, takeRecordedMCPConnectChecks(t, ti, seen))

			require.Contains(t, call("execute_tool", map[string]any{"name": closed.slug + "--closed_tool", "arguments": map[string]any{}}), "unknown server")
			require.Equal(t, denial, takeRecordedMCPConnectChecks(t, ti, seen))
		})
	}
}

func serveStoredGatewayWithoutGrants(t *testing.T, ctx context.Context, ti *testInstance, member func(metaID uuid.UUID) hostedMemberFixture) func(string, map[string]any) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	slug := "meta-" + uuid.NewString()
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, slug, uuid.Nil)
	member(meta.ID)
	callerCtx := authztest.WithExactGrants(t, ctx)
	return func(tool string, arguments map[string]any) string {
		w, err := servePublicHTTP(t, callerCtx, ti, slug, makeMetaRPCBody(t, "tools/call", map[string]any{"name": tool, "arguments": arguments}), "", nil)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
}

func serveOrganizationGatewayAsMember(t *testing.T, ctx context.Context, ti *testInstance, member func(metaID uuid.UUID) hostedMemberFixture) func(string, map[string]any) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fx := seedOrganizationGateway(t, ctx, ti)
	member(createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil).ID)
	bearer := mintOrganizationGatewayBearer(t, ti, fx)
	return func(tool string, arguments map[string]any) string {
		w, err := serveOrganizationGatewayHTTP(t, t.Context(), ti, fx.organizationSlug, bearer, makeMetaRPCBody(t, "tools/call", map[string]any{"name": tool, "arguments": arguments}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
}

func serveAgentGatewayAsAgent(t *testing.T, ctx context.Context, ti *testInstance, member func(metaID uuid.UUID) hostedMemberFixture) func(string, map[string]any) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	fx := seedAgentGateway(t, ctx, ti)
	member(createMetaMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, authCtx.ActiveOrganizationID, "meta-"+uuid.NewString(), uuid.Nil).ID)
	return func(tool string, arguments map[string]any) string {
		w, err := serveAgentGatewayHTTP(t, ti, fx.agent.ID.String(), fx.token, makeMetaRPCBody(t, "tools/call", map[string]any{"name": tool, "arguments": arguments}))
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
}
