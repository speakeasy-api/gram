package platformmcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every tool's description and input schema are sent to the model on each
// turn it is listed, so they cost context whether or not the tool is called.
const (
	maxToolDescriptionBytes = 1536
	maxToolInputSchemaBytes = 4096
)

// oversizedToolDescriptions and oversizedToolInputSchemas hold tools that were
// already over budget, with the size each may not grow past. Shrink a tool and
// lower or remove its entry; the test fails when an entry is no longer needed.
var (
	oversizedToolDescriptions = map[string]int{
		"create_mcp_from_functions":           2059,
		"create_plugin":                       1809,
		"create_sensor":                       2007,
		"delete_widget":                       1607,
		"distribute_mcp_to_plugin":            1630,
		"duplicate_widget":                    1698,
		"edit_dashboard_layout":               1612,
		"get_agent_mcp_access":                2047,
		"get_mcp_connection_settings":         2111,
		"get_network_ingress":                 2808,
		"get_plugin":                          4332,
		"get_xaa_readiness":                   4240,
		"redeploy_deployment":                 1568,
		"resume_data_export":                  2098,
		"run_analytics_query":                 1554,
		"save_widget":                         1871,
		"search_tool_calls":                   1562,
		"send_data_export_test_event":         2221,
		"set_plugin_assignments":              2138,
		"trust_inbound_ema_identity_provider": 1644,
		"update_sensor":                       1838,
	}
	oversizedToolInputSchemas = map[string]int{
		"create_risk_exclusion": 17658,
		"create_risk_policy":    22344,
		"run_analytics_query":   6064,
		"update_risk_policy":    13299,
	}
)

// TestPlatformToolsStayWithinSizeBudget stops a tool's description or input
// schema from growing past its budget, and stops a grandfathered one from
// growing at all.
func TestPlatformToolsStayWithinSizeBudget(t *testing.T) {
	t.Parallel()

	catalogue := toolEvalCatalogue(t, toolEvalRegistrar(t))
	seen := make(map[string]bool, len(catalogue))
	for _, tool := range catalogue {
		seen[tool.Name] = true
		checkToolSizeBudget(t, tool.Name, "description", len(tool.Description), maxToolDescriptionBytes, oversizedToolDescriptions)
		checkToolSizeBudget(t, tool.Name, "input schema", len(tool.InputSchema), maxToolInputSchemaBytes, oversizedToolInputSchemas)
	}
	for _, exempt := range []map[string]int{oversizedToolDescriptions, oversizedToolInputSchemas} {
		for name := range exempt {
			require.True(t, seen[name], "%s has a size exemption but is not in the catalogue; remove it", name)
		}
	}
}

func checkToolSizeBudget(t *testing.T, name, part string, size, budget int, exempt map[string]int) {
	t.Helper()

	ceiling, grandfathered := exempt[name]
	switch {
	case !grandfathered:
		require.LessOrEqual(t, size, budget, "%s's %s is %d bytes, over the %d-byte budget; shorten it", name, part, size, budget)
	case size <= budget:
		require.Fail(t, "exemption no longer needed", "%s's %s is %d bytes, within budget; remove its exemption", name, part, size)
	default:
		require.LessOrEqual(t, size, ceiling, "%s's %s grew to %d bytes, past its %d-byte ceiling; shorten it, do not raise the ceiling", name, part, size, ceiling)
	}
}
