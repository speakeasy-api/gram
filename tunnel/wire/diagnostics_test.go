package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTargetDisplayRemovesCredentialsQueryAndFragment(t *testing.T) {
	require.Equal(t, "https://internal.example:8443/mcp%2Fv2", TargetDisplay("https://user:secret@internal.example:8443/mcp%2Fv2?token=private#secret"))
	require.Empty(t, TargetDisplay("file:///private/key"))
	require.Empty(t, TargetDisplay("https://example.com/"+strings.Repeat("a", MaxTargetDisplayBytes)))
}

func TestOptionalCapabilityIgnoresMalformedHeaders(t *testing.T) {
	require.False(t, SupportsDiagnostics(DiagnosticsCapability, "secret"))
	require.False(t, SupportsDiagnostics(DiagnosticsCapability, strings.Repeat("g", 64)))
	require.False(t, SupportsDiagnostics(strings.Repeat("a", 300), strings.Repeat("a", 64)))
	require.False(t, SupportsDiagnostics("future.v2", strings.Repeat("a", 64)))
	require.True(t, SupportsDiagnostics("future.v2, diagnostics.v1", strings.Repeat("a", 64)))
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
	require.Equal(t, "wss://gateway.example:443/connect", GatewayDisplay("wss://user:secret@gateway.example:443/connect?token=secret#secret"))
	require.Empty(t, GatewayDisplay("https://gateway.example"))
}
