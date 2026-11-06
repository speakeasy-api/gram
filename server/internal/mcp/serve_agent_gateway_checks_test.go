package mcp_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

// An agent gateway admits members by evaluating every private server in the
// organization. Recording each of those decisions listed every server the
// agent has no grant for as a denied check on every request. Only a call that
// names a server records its check, once per request.
func TestServeAgentGateway_RecordsChecksOnlyForNamedServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ti.service.SetAuthzEngine(authz.NewEngine(testenv.NewLogger(t), ti.conn, authztest.ChallengeLoggingAlwaysEnabled, workos.NewStubClient(), authz.EngineOpts{
		AdmitPrincipalCredential:         runtimepolicy.AdmitPrincipalCredential,
		AdmitPrincipalCredentialWithDBTX: runtimepolicy.AdmitPrincipalCredentialWithDBTX,
		AdmitWorkloadSession:             runtimepolicy.AdmitWorkloadSession,
		AdmitWorkloadSessionInTx:         runtimepolicy.AdmitWorkloadSessionInTx,
		DevMode:                          false,
	}))
	fx := seedAgentGateway(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	granted := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.granted).String()
	denied := mcpServerBySlug(t, ctx, ti, *authCtx.ProjectID, fx.denied).String()
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
}
