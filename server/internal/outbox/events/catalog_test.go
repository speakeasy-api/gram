package events_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/outbox/cataloggen"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// TestCatalogIsUpToDate fails if either catalog_gen.go or catalog_gen.yaml is out of
// sync with the event definition files. Run 'mise gen:webhooks-server' to fix.
func TestCatalogIsUpToDate(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	dir := filepath.Dir(thisFile)

	require.NoError(t, cataloggen.Check(dir))
	require.NoError(t, cataloggen.CheckYAML(dir, events.All))
}

func TestAccessEventsUseGenericNames(t *testing.T) {
	t.Parallel()

	eventTypes := make([]outbox.EventType, 0, len(events.All))
	for _, event := range events.All {
		eventTypes = append(eventTypes, event.EventType())
	}

	require.Contains(t, eventTypes, outbox.EventType("audit_log.access_rule_event_v1"))
	require.Contains(t, eventTypes, outbox.EventType("audit_log.access_request_event_v1"))
	require.NotContains(t, eventTypes, outbox.EventType("audit_log.shadow_mcp_access_rule_event_v1"))
	require.NotContains(t, eventTypes, outbox.EventType("audit_log.shadow_mcp_approval_event_v1"))
}
