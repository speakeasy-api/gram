package runtime

import (
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
)

// SpeakeasySlackTools returns the Slack tools a project's managed assistant
// replies through when the Speakeasy Slack app routes a workspace to its
// project. router may be nil, in which case replies cannot be routed back to
// the assistant's conversation.
func SpeakeasySlackTools(httpClient *guardian.HTTPClient, router platformslack.ThreadRouter, tokens platformslack.ProjectTokenSource) []platformtools.ExternalTool {
	executors := platformslack.NewSpeakeasyAppTools(httpClient, router, tokens)
	tools := make([]platformtools.ExternalTool, 0, len(executors))
	for _, executor := range executors {
		tools = append(tools, platformtools.ExternalTool{Executor: executor, RequiredFeature: "", RequiredScope: ""})
	}
	return tools
}
