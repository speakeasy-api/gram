package events_test

import (
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

	require.NoError(t, cataloggen.Check("."))
	require.NoError(t, cataloggen.CheckYAML(".", events.All))
}

func TestAccessEventsUseGenericNames(t *testing.T) {
	t.Parallel()

	eventDescriptions := make(map[outbox.EventType]string, len(events.All))
	for _, event := range events.All {
		eventDescriptions[event.EventType()] = event.Description()
	}

	require.Contains(t, eventDescriptions, outbox.EventType("audit_log.access_rule_event_v1"))
	require.Contains(t, eventDescriptions, outbox.EventType("audit_log.access_request_event_v1"))

	require.Contains(t, eventDescriptions, outbox.EventType("audit_log.shadow_mcp_access_rule_event_v1"))
	require.Contains(t, eventDescriptions, outbox.EventType("audit_log.shadow_mcp_approval_event_v1"))
	require.Contains(t, eventDescriptions[outbox.EventType("audit_log.shadow_mcp_access_rule_event_v1")], "Deprecated: use audit_log.access_rule_event_v1.")
	require.Contains(t, eventDescriptions[outbox.EventType("audit_log.shadow_mcp_approval_event_v1")], "Deprecated: use audit_log.access_request_event_v1.")
}
