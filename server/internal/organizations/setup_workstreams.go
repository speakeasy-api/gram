package organizations

import (
	"slices"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/gen/types"
)

type setupWorkstreamDefinition struct {
	ID          string
	Title       string
	Description string
	TaskKeys    []string
}

// setupWorkstreams is the canonical display order and membership for the
// onboarding wizard and assignment. Membership includes hidden tasks.
var setupWorkstreams = []setupWorkstreamDefinition{
	{ID: "connect", Title: "Connect identity", Description: "Authenticate people and agents. Sync IDP roles.", TaskKeys: []string{"domain-verification", "connect-idp", "directory-sync", "identity-provider"}},
	{ID: "observe", Title: "Observe agents", Description: "Instrument agents, add integrations, and verify traffic.", TaskKeys: []string{"enable-logging", "anthropic-observability", "instrument-agents", "litellm", "additional-agent-config", "confirm-traffic"}},
	{ID: "distribute", Title: "MCP Gateway", Description: "Publish and distribute approved MCP servers.", TaskKeys: []string{"create-marketplace", "distribute-servers", "platform-mcp"}},
	{ID: "secure", Title: "Secure agent traffic", Description: "Apply the initial policy controls.", TaskKeys: []string{"anthropic-admin-controls", "configure-policies"}},
}

func setupWorkstreamForID(id string) *setupWorkstreamDefinition {
	for i := range setupWorkstreams {
		if setupWorkstreams[i].ID == id {
			return &setupWorkstreams[i]
		}
	}
	return nil
}

// setupWorkstreamViewsForTasks preserves catalog order but exposes only membership
// present in the already-authorized task response. Assignment uses the full catalog.
func setupWorkstreamViewsForTasks(tasks []*gen.SetupTask) []*types.SetupWorkstream {
	allowed := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		allowed[task.Key] = true
	}
	views := setupWorkstreamViews()
	for _, view := range views {
		view.TaskKeys = slices.DeleteFunc(view.TaskKeys, func(key string) bool { return !allowed[key] })
	}
	return views
}

func setupWorkstreamViews() []*types.SetupWorkstream {
	result := make([]*types.SetupWorkstream, 0, len(setupWorkstreams))
	for _, workstream := range setupWorkstreams {
		result = append(result, &types.SetupWorkstream{
			ID: workstream.ID, Title: workstream.Title, TaskKeys: slices.Clone(workstream.TaskKeys),
		})
	}
	return result
}
