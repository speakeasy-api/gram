package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// registerRemoteToolArguments encodes tool arguments so receipts with
// arbitrary base64 content survive into the JSON payload unmangled.
func registerRemoteToolArguments(t *testing.T, projectSlug, receipt, idempotencyKey string) json.RawMessage {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{
		"project_slug":    projectSlug,
		"probe_receipt":   receipt,
		"idempotency_key": idempotencyKey,
	})
	require.NoError(t, err)
	return arguments
}

func TestRegisterRemoteMCPToolRegistersWithProbeReceipt(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Name: "Project", Slug: "project"}
	registrationID := uuid.New()
	store := remoteRegistrationStore(project, registrationID)
	registrations := liveRemoteRegistrations(store, &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: true})
	descriptor := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	principal := testPrincipal()
	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow)

	raw, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), principal), registerRemoteToolArguments(t, project.Slug, receipt, "request-key"))

	require.NoError(t, err)
	output, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	require.Equal(t, project.Slug, output.ProjectSlug)
	require.Equal(t, "https://remote.example.test/mcp", output.RemoteURL)
	require.Equal(t, registrationID.String(), output.RegistrationID)
	require.NotEmpty(t, output.ReceiptID)
	require.False(t, output.Replayed)
	require.False(t, output.BlockedPendingApproval)
	require.Empty(t, output.DashboardApprovalsURL)
	require.Equal(t, "continue_dashboard_setup", output.NextAction)
	require.Contains(t, output.Message, "dashboard")
	require.Equal(t, 1, store.completeRemoteCalls)
	require.Equal(t, "https://remote.example.test/mcp", store.remoteURL)
}

func TestRegisterRemoteMCPToolReportsBlockedPendingApproval(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	checker := &recordingRemoteApprovalChecker{state: RemoteMCPApprovalState{EnforcementActive: true, Approved: false}}
	registrations := liveRemoteRegistrations(store, checker)
	registrar := remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: true})
	descriptor := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	principal := testPrincipal()
	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow)

	raw, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), principal), registerRemoteToolArguments(t, project.Slug, receipt, "request-key"))

	require.NoError(t, err)
	output, ok := raw.(RegisterRemoteMCPToolOutput)
	require.True(t, ok)
	require.True(t, output.BlockedPendingApproval)
	require.Equal(t, "/organization/projects/project/shadow-mcp", output.DashboardApprovalsURL)
	require.Equal(t, "await_org_approval", output.NextAction)
	require.Contains(t, output.Message, "approves it")
}

func TestRegisterRemoteMCPToolRefusesExpiredReceiptWithRemedy(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	registrations := liveRemoteRegistrations(store, &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: true})
	descriptor := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	principal := testPrincipal()
	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow.Add(-probeReceiptTTL))

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), principal), registerRemoteToolArguments(t, project.Slug, receipt, "request-key"))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, "receipt_expired", payload["code"])
	require.Contains(t, payload["message"], "probe_remote_mcp again")
	require.Zero(t, store.beginCalls)
}

func TestRegisterRemoteMCPToolRefusesWhenSurfaceGateDisabled(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	store := remoteRegistrationStore(project, uuid.New())
	registrations := liveRemoteRegistrations(store, &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: false})
	descriptor := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
	principal := testPrincipal()
	receipt := mintProbeReceipt(t, remoteRegistrationTestKey, principal, "https://remote.example.test/mcp", remoteRegistrationTestNow)

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), principal), registerRemoteToolArguments(t, project.Slug, receipt, "request-key"))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, unavailableCode, payload["code"])
	require.Equal(t, featureRemoteURLRegistration, payload["feature"])
	require.Zero(t, store.beginCalls, "a gated-off organization must not reach the registration store")
}

func TestRegisterRemoteMCPToolStubRefusesWithBoundedResult(t *testing.T) {
	t.Parallel()

	registrar := remoteToolsRegistrar(nil, nil, nil)
	descriptor := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), registerRemoteToolArguments(t, "project", "receipt", "request-key"))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, unavailableCode, payload["code"])
	require.Equal(t, featureRemoteURLRegistration, payload["feature"])
}

func TestRegisterRemoteMCPToolResultMapsReceiptAndRegistrationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		code string
	}{
		{err: ErrProbeReceiptExpired, code: "receipt_expired"},
		{err: ErrProbeReceiptInvalid, code: "receipt_invalid"},
		{err: ErrProbeReceiptContextMismatch, code: "receipt_context_mismatch"},
		{err: ErrRegistrationCap, code: "conflict"},
		{err: ErrTargetIneligible, code: "ineligible_project"},
		{err: ErrOperationRateLimited, code: "rate_limited"},
		{err: ErrRegistrationUnavailable, code: unavailableCode},
	}

	for _, test := range tests {
		result, ok := registerRemoteMCPToolResult(test.err)

		require.True(t, ok, "error %v", test.err)
		require.True(t, result.IsError)
		text, isText := result.Content[0].(*mcp.TextContent)
		require.True(t, isText)
		var body operationBudgetResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &body))
		require.Equal(t, test.code, body.Code, "error %v", test.err)
		require.NotEmpty(t, body.Message)
	}
}
