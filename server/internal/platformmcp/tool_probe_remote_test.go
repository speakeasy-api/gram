package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type fakeRemoteProber struct {
	result    RemoteProbeResult
	err       error
	calls     int
	principal Principal
	remoteURL string
}

func (p *fakeRemoteProber) Probe(_ context.Context, principal Principal, remoteURL string) (RemoteProbeResult, error) {
	p.calls++
	p.principal = principal
	p.remoteURL = remoteURL
	return p.result, p.err
}

// remoteToolsRegistrar composes the deployment the way newServer does, so tool
// tests exercise the same registration pass the endpoint serves.
func remoteToolsRegistrar(registrations *RegistrationService, prober RemoteMCPProber, surfaceGate Gate) *Registrar {
	_, registrar := newServer(nil, nil, registrations, "", nil, nil, nil, nil, nil, CatalogDescriptor{}, prober, surfaceGate)
	return registrar
}

func remoteToolDescriptor(t *testing.T, registrar *Registrar, name string) Descriptor {
	t.Helper()
	for _, descriptor := range registrar.Descriptors() {
		if descriptor.Name == name {
			return descriptor
		}
	}
	t.Fatalf("tool %q is not registered", name)
	return Descriptor{}
}

// liveRemoteRegistrations builds a registration service whose remote URL path
// is fully configured, backed by the recording store fakes.
func liveRemoteRegistrations(store *recordingRegistrationStore, approvals RemoteMCPApprovalChecker) *RegistrationService {
	return newRemoteRegistrationService(&testRegistrationGate{enabled: true}, store, approvals)
}

func decodeToolRefusal(t *testing.T, err error) map[string]any {
	t.Helper()
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload))
	return payload
}

// The rollout must change what the pair answers, not whether it exists: a tool
// that vanishes with the rollout looks to a client exactly like one that was
// never built.
func TestRemoteMCPToolsAreDeclaredWithAndWithoutTheirDependencies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		build func() *Registrar
	}{
		{name: "composed", build: func() *Registrar {
			registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
			return remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: true})
		}},
		{name: "absent", build: func() *Registrar {
			return remoteToolsRegistrar(nil, nil, nil)
		}},
		{name: "incomplete", build: func() *Registrar {
			// A registration service without its remote receipt codec and
			// approval checker must serve stubs even when a prober exists.
			registrations := newRegistrationService(testCatalog{}, &testRegistrationGate{enabled: true}, &recordingRegistrationStore{})
			return remoteToolsRegistrar(registrations, &fakeRemoteProber{}, testGate{enabled: true})
		}},
	} {
		registrar := test.build()

		probe := remoteToolDescriptor(t, registrar, "probe_remote_mcp")
		require.Equal(t, externalOnly, probe.Meta.Audiences, "%s: probe_remote_mcp audience", test.name)
		require.Equal(t, ProjectScopeNone, probe.Meta.ProjectScope, "%s: the probe takes no project selector", test.name)
		require.NotNil(t, probe.Annotations, "%s: probe_remote_mcp declares annotations", test.name)
		require.True(t, probe.Annotations.ReadOnlyHint, "%s: probe_remote_mcp is read-only", test.name)

		register := remoteToolDescriptor(t, registrar, "register_remote_mcp_for_project")
		require.Equal(t, externalOnly, register.Meta.Audiences, "%s: register_remote_mcp_for_project audience", test.name)
		require.Equal(t, ProjectScopeExplicit, register.Meta.ProjectScope, "%s: registration names its project", test.name)
	}
}

func TestProbeRemoteMCPToolStubRefusesWithBoundedResult(t *testing.T) {
	t.Parallel()

	registrar := remoteToolsRegistrar(nil, nil, nil)
	descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, unavailableCode, payload["code"])
	require.Equal(t, featureRemoteURLRegistration, payload["feature"])
}

func TestProbeRemoteMCPToolRefusesWhenSurfaceGateDisabled(t *testing.T) {
	t.Parallel()

	prober := &fakeRemoteProber{}
	registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, prober, testGate{enabled: false})
	descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, unavailableCode, payload["code"])
	require.Equal(t, featureRemoteURLRegistration, payload["feature"])
	require.Zero(t, prober.calls, "a gated-off organization must spend no probe egress")
}

func TestProbeRemoteMCPToolFailsClosedWhenSurfaceGateUnavailable(t *testing.T) {
	t.Parallel()

	prober := &fakeRemoteProber{}
	registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, prober, testGate{enabled: true, err: errors.New("flag service unavailable")})
	descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))

	payload := decodeToolRefusal(t, err)
	require.Equal(t, unavailableCode, payload["code"])
	require.NotContains(t, payload["message"], "flag service", "gate detail must not be echoed")
	require.Zero(t, prober.calls)
}

func TestProbeRemoteMCPToolReturnsEvidenceAndReceipt(t *testing.T) {
	t.Parallel()

	expiry := time.Date(2026, 8, 21, 12, 10, 0, 0, time.UTC)
	prober := &fakeRemoteProber{result: RemoteProbeResult{
		Evidence: ProbeEvidence{
			NormalizedURL: "https://remote.example.test/mcp",
			ServerName:    "vendor-mcp",
			ServerVersion: "1.2.3",
			ToolCount:     2,
			ToolNames:     []string{"list_things", "get_thing"},
			AuthPosture:   ProbeAuthPostureOpen,
			Gaps:          []string{"server publishes no OAuth metadata at the RFC 9728/8414 well-known endpoints"},
		},
		Receipt:          "signed-receipt",
		ReceiptExpiresAt: expiry,
	}}
	registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, prober, testGate{enabled: true})
	descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")
	principal := testPrincipal()

	raw, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), principal), json.RawMessage(`{"remote_url":"https://Remote.example.test/mcp"}`))

	require.NoError(t, err)
	output, ok := raw.(ProbeRemoteMCPToolOutput)
	require.True(t, ok)
	require.Equal(t, prober.result.Evidence, output.Evidence)
	require.Equal(t, "signed-receipt", output.ProbeReceipt)
	require.Equal(t, "2026-08-21T12:10:00Z", output.ReceiptExpiresAt)
	require.Equal(t, "confirm_evidence_with_user", output.NextAction)
	require.Contains(t, output.Message, "explicit confirmation")

	require.Equal(t, 1, prober.calls)
	require.Equal(t, principal, prober.principal)
	require.Equal(t, "https://Remote.example.test/mcp", prober.remoteURL, "normalization belongs to the probe service, not the tool")
}

func TestProbeRemoteMCPToolMapsTypedRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		code string
	}{
		{err: ErrRemoteURLInvalid, code: "invalid_url"},
		{err: ErrProbeEgressDenied, code: "egress_denied"},
		{err: ErrProbeUnreachable, code: "unreachable"},
		{err: ErrProbeNotMCPServer, code: "not_an_mcp_server"},
		{err: ErrProbeReceiptInvalid, code: "receipt_invalid"},
		{err: ErrOperationRateLimited, code: "rate_limited"},
		{err: ErrOperationBudgetUnavailable, code: unavailableCode},
	}

	registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
	for _, test := range tests {
		prober := &fakeRemoteProber{err: test.err}
		registrar := remoteToolsRegistrar(registrations, prober, testGate{enabled: true})
		descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")

		_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))

		payload := decodeToolRefusal(t, err)
		require.Equal(t, test.code, payload["code"], "error %v", test.err)
		require.NotEmpty(t, payload["message"], "error %v", test.err)
	}
}

// An unexpected internal failure must surface as an error, never as a bounded
// refusal the model would treat as a fact about the probed server.
func TestProbeRemoteMCPToolLeavesUnexpectedFailuresAsErrors(t *testing.T) {
	t.Parallel()

	internal := errors.New("receipt signing failed")
	prober := &fakeRemoteProber{err: internal}
	registrations := liveRemoteRegistrations(remoteRegistrationStore(ResolvedProject{ID: uuid.New(), Slug: "project"}, uuid.New()), &recordingRemoteApprovalChecker{})
	registrar := remoteToolsRegistrar(registrations, prober, testGate{enabled: true})
	descriptor := remoteToolDescriptor(t, registrar, "probe_remote_mcp")

	_, err := descriptor.Invoke(ContextWithPrincipal(t.Context(), testPrincipal()), json.RawMessage(`{"remote_url":"https://remote.example.test/mcp"}`))

	require.ErrorIs(t, err, internal)
	var refusal *ToolRefusalError
	require.NotErrorAs(t, err, &refusal)
}
