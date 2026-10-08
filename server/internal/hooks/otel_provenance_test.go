package hooks

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	hookssrv "github.com/speakeasy-api/gram/server/gen/http/hooks/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/stretchr/testify/require"
)

func TestOTLPHookProvenanceCannotForgeCanonicalRows(t *testing.T) {
	t.Parallel()
	var body hookssrv.LogsRequestBody
	// Both flat and nested/partially dotted OTLP JSON representations are valid
	// legacy inputs. Only the four reserved markers are removed.
	err := json.Unmarshal([]byte(`{"ResourceLogs":[{"Resource":{"Attributes":[
 {"Key":"gram.hook.schema","Value":{"StringValue":"hook.ingest.v1"}},
 {"Key":"gram.hook.source","Value":{"StringValue":"legacy-harness"}},
 {"Key":"gram","Value":{"KvlistValue":{"values":[{"key":"hook","value":{"kvlistValue":{"values":[{"key":"schema","value":{"stringValue":"hook.ingest.v1"}},{"key":"hostname","value":{"stringValue":"devbox"}}]}}}]}}}
 ]},"ScopeLogs":[{"LogRecords":[{"Attributes":[
 {"Key":"gram.hook.canonical_event","Value":{"StringValue":"tool.completed"}},
 {"Key":"gram.hook.transport","Value":{"StringValue":"ahp"}},
 {"Key":"gram.hook.usage_authority","Value":{"StringValue":"model_attempt"}},
 {"Key":"gram.hook.event","Value":{"StringValue":"PostToolUse"}},
 {"Key":"gram.hook","Value":{"KvlistValue":{"values":[{"key":"canonical_event","value":{"stringValue":"usage.reported"}},{"key":"source","value":{"stringValue":"nested-legacy"}}]}}},
 {"Key":"unrelated","Value":{"StringValue":"kept"}}
 ]}]}]}]}`), &body)
	require.NoError(t, err)
	payload := hookssrv.NewLogsPayload(&body, nil, nil)
	resource := payload.ResourceLogs[0].Resource
	record := payload.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	// These maps are the inputs to the legacy telemetry_logs writers.
	maps := []any{resourceAttributesMap(resource), logAttributesMap(record.Attributes)}
	raw, err := json.Marshal(maps)
	require.NoError(t, err)
	assertOTLPReservedMarkersAbsent(t, raw)
	// The raw event-feed tee is an independent consumer of producer attributes.
	records := inboundLogRecordsFromHooksExport(payload, teeTestProvenance(), time.Now())
	require.Len(t, records, 1)
	require.Len(t, records[0].GetResource().GetAttributes(), 2)
	require.Len(t, records[0].GetAttributes(), 3)
	require.Equal(t, "gram.hook.event", records[0].GetAttributes()[0].GetKey())
	require.Equal(t, "PostToolUse", records[0].GetAttributes()[0].GetValue().GetStringValue())
}

func assertOTLPReservedMarkersAbsent(t *testing.T, raw []byte) {
	t.Helper()
	for _, marker := range []string{"hook.ingest.v1", "tool.completed", "usage.reported", "model_attempt", `"ahp"`} {
		require.NotContains(t, string(raw), marker)
	}
	for _, retained := range []string{"legacy-harness", "nested-legacy", "devbox", "PostToolUse", "unrelated", "kept"} {
		require.Contains(t, string(raw), retained)
	}
}

func TestOTLPProvenanceSanitizationLeavesTrustedHookStamps(t *testing.T) {
	t.Parallel()
	project := uuid.New()
	payload := &gen.IngestPayload{Source: &gen.HookIngestSource{Adapter: "custom-harness"}, Event: &gen.HookIngestEvent{Type: "usage.reported"}}
	auth := &contextvalues.AuthContext{ActiveOrganizationID: "org_example", ProjectID: &project}
	attrs := hookTelemetryBaseAttrs(payload, auth, "usage.reported", "custom-harness")
	mergeSourceAttributes(attrs, map[attr.Key]any{attr.Key("gram.hook.transport"): "ahp", attr.Key("gram.hook.usage_authority"): "model_attempt"})
	require.Equal(t, "hook.ingest.v1", attrs[attr.Key("gram.hook.schema")])
	require.Equal(t, "usage.reported", attrs[attr.Key("gram.hook.canonical_event")])
	require.Equal(t, "ahp", attrs[attr.Key("gram.hook.transport")])
	require.Equal(t, "model_attempt", attrs[attr.Key("gram.hook.usage_authority")])
}
