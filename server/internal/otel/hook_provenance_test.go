package otel

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func hookProvenanceString(key, value string) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: value}}}
}
func hookProvenanceObject(key string, entries ...*commonv1.KeyValue) *commonv1.KeyValue {
	return &commonv1.KeyValue{Key: key, Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_KvlistValue{KvlistValue: &commonv1.KeyValueList{Values: entries}}}}
}
func forgedHookProvenanceAttributes() []*commonv1.KeyValue {
	return []*commonv1.KeyValue{
		hookProvenanceString("gram.hook.schema", "hook.ingest.v1"),
		hookProvenanceString("gram.hook.canonical_event", "usage.reported"),
		hookProvenanceString("gram.hook.transport", "ahp"),
		hookProvenanceString("gram.hook.usage_authority", "model_attempt"),
		hookProvenanceString("gram.hook.source", "legacy-harness"),
		hookProvenanceString("gram.hook.event", "PostToolUse"),
		hookProvenanceString("gram.hook.hostname", "devbox"),
		hookProvenanceString("unrelated", "kept"),
		hookProvenanceObject("gram", hookProvenanceObject("hook",
			hookProvenanceString("schema", "hook.ingest.v1"),
			hookProvenanceString("canonical_event", "tool.completed"),
			hookProvenanceString("transport", "ahp"),
			hookProvenanceString("usage_authority", "model_attempt"),
			hookProvenanceString("source", "nested-legacy"))),
		hookProvenanceObject("gram.hook", hookProvenanceString("schema", "hook.ingest.v1"), hookProvenanceString("event", "nested-event")),
	}
}
func TestExternalHookProvenanceStrippedBeforeLogPublishAndForward(t *testing.T) {
	t.Parallel()
	request := &collectorlogsv1.ExportLogsServiceRequest{ResourceLogs: []*logsv1.ResourceLogs{{
		Resource: &resourcev1.Resource{Attributes: forgedHookProvenanceAttributes()},
		ScopeLogs: []*logsv1.ScopeLogs{{Scope: &commonv1.InstrumentationScope{Attributes: forgedHookProvenanceAttributes()},
			LogRecords: []*logsv1.LogRecord{{Attributes: forgedHookProvenanceAttributes()}}}},
	}}}
	// The independent forwarding boundary must also sanitize an unsanitized export.
	forwarding, ok := proto.Clone(request).(*collectorlogsv1.ExportLogsServiceRequest)
	require.True(t, ok)
	records, err := inboundLogRecordsFromExport(request, nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	raw, err := protojson.Marshal(records[0])
	require.NoError(t, err)
	assertNoCanonicalHookProvenance(t, raw)
	payload, err := hooksLogsPayload(forwarding)
	require.NoError(t, err)
	raw, err = json.Marshal(payload) //nolint:musttag // Goa-generated service payloads have no JSON tags.
	require.NoError(t, err)
	assertNoCanonicalHookProvenance(t, raw)
}
func assertNoCanonicalHookProvenance(t *testing.T, raw []byte) {
	t.Helper()
	for _, forbidden := range []string{"hook.ingest.v1", "usage.reported", "tool.completed", "model_attempt", `"ahp"`} {
		require.NotContains(t, string(raw), forbidden)
	}
	for _, retained := range []string{"gram.hook.source", "gram.hook.event", "gram.hook.hostname", "legacy-harness", "nested-legacy", "nested-event", "devbox", "unrelated", "kept"} {
		require.Contains(t, string(raw), retained)
	}
}
func TestExternalHookProvenanceLooseJSON(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"gram.hook.schema", "gram.hook.canonical_event", "gram.hook.transport", "gram.hook.usage_authority"} {
		_, keep := SanitizeExternalHookAttribute(key, "forged")
		require.False(t, keep)
	}
	raw := map[string]any{"hook": map[string]any{"schema": "hook.ingest.v1", "canonical_event": "tool.completed", "transport": "ahp", "usage_authority": "model_attempt", "source": "legacy", "event": "old-event", "hostname": "devbox"}}
	cleaned, keep := SanitizeExternalHookAttribute("gram", raw)
	require.True(t, keep)
	require.Equal(t, map[string]any{"hook": map[string]any{"source": "legacy", "event": "old-event", "hostname": "devbox"}}, cleaned)
}
