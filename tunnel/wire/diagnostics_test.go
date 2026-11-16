package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTargetDisplayKeepsOnlySchemeHostAndPort(t *testing.T) {
	t.Parallel()

	require.Equal(t, "https://internal.example:8443", TargetDisplay("https://user:secret@internal.example:8443/mcp%2Fv2?token=private#secret"))
	require.Equal(t, "http://internal.example", TargetDisplay("http://internal.example/mcp/0123456789abcdef"))
	require.Equal(t, "http://[::1]:3000", TargetDisplay("http://[::1]:3000/mcp/secret-token"))
	require.Equal(t, "https://internal.example:8443", TargetDisplay("https://internal.example:8443"), "sanitizing a sanitized value is a no-op")
	require.Empty(t, TargetDisplay("file:///private/key"))
	require.Empty(t, TargetDisplay("https://"+strings.Repeat("a", MaxTargetDisplayBytes)+".example/"))
}

func TestOptionalCapabilityIgnoresMalformedHeaders(t *testing.T) {
	require.False(t, SupportsDiagnostics(DiagnosticsCapability, "secret"))
	require.False(t, SupportsDiagnostics(DiagnosticsCapability, strings.Repeat("g", 64)))
	require.False(t, SupportsDiagnostics(strings.Repeat("a", 300), strings.Repeat("a", 64)))
	require.False(t, SupportsDiagnostics("future.v2", strings.Repeat("a", 64)))
	require.True(t, SupportsDiagnostics("future.v2, diagnostics.v1", strings.Repeat("a", 64)))
	require.False(t, SupportsDiagnostics(DiagnosticsDisabledCapability, strings.Repeat("a", 64)))
}

func TestDiagnosticsDisabledNeedsOnlyTheCapability(t *testing.T) {
	t.Parallel()

	require.True(t, DiagnosticsDisabled(DiagnosticsDisabledCapability))
	require.True(t, DiagnosticsDisabled("future.v2, "+DiagnosticsDisabledCapability))
	require.False(t, DiagnosticsDisabled(""))
	require.False(t, DiagnosticsDisabled(DiagnosticsCapability))
	require.False(t, DiagnosticsDisabled(DiagnosticsDisabledCapability+","+strings.Repeat("a", 300)))
}

func TestDiagnosticsRejectArbitraryErrorLabels(t *testing.T) {
	step := DiagnosticStep{State: "not_tested"}
	r := DiagnosticsReport{Version: 1, TargetState: "pending", DNS: step, TCP: step, TLS: step}
	require.NoError(t, r.Validate())
	r.LastTransportError = "password=private"
	require.Error(t, r.Validate())
	r.LastTransportError = ""
	r.DNS.State = "private-hostname"
	require.Error(t, r.Validate())
}

func TestDecodeRequiresV1FieldsAndDiscardsFutureData(t *testing.T) {
	step := DiagnosticStep{State: "not_tested"}
	original := DiagnosticsReport{Version: 1, TargetState: "pending", DNS: step, TCP: step, TLS: step}
	raw, err := json.Marshal(original)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &fields))
	fields["future_field"] = json.RawMessage(`"PRIVATE_SENTINEL"`)
	raw, err = json.Marshal(fields)
	require.NoError(t, err)
	report, err := DecodeDiagnostics(raw)
	require.NoError(t, err)
	require.Nil(t, report.HTTPProgress, "older v1 agents omit this additive field")
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_SENTINEL")
	for _, field := range []string{"sequence", "sample_age_ms", "requests_total", "dns"} {
		saved := fields[field]
		delete(fields, field)
		raw, err = json.Marshal(fields)
		require.NoError(t, err)
		_, err = DecodeDiagnostics(raw)
		require.Error(t, err, field)
		fields[field] = saved
	}
}

func TestGatewayDisplayDoesNotLogSecrets(t *testing.T) {
	require.Equal(t, "wss://gateway.example:443", GatewayDisplay("wss://user:secret@gateway.example:443/connect?token=secret#secret"))
	require.Empty(t, GatewayDisplay("https://gateway.example"))
}
