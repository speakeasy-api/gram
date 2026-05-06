package platformtools

import (
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
	platformtriggers "github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolFactory func(deps Dependencies) core.PlatformToolExecutor

var registry = []toolFactory{
	func(deps Dependencies) PlatformToolExecutor {
		return logs.NewSearchLogsTool(deps.TelemetryService)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformtriggers.NewListTriggersTool(deps.DB, deps.TriggerApp)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformtriggers.NewConfigureTriggerTool(deps.DB, deps.TriggerApp, deps.Audit)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadChannelMessagesTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadThreadMessagesTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadUserProfileTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchChannelsTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchMessagesAndFilesTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchUsersTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewScheduleMessageTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSendMessageTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewAddReactionTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRemoveReactionTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetReactionsTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListReactionsTool(deps.SlackHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListEmojiTool(deps.SlackHTTPClient)
	},
}

func BuildExecutors(deps Dependencies) map[string]PlatformToolExecutor {
	executors := make(map[string]PlatformToolExecutor, len(registry))
	for _, factory := range registry {
		executor := factory(deps)
		executors[executor.Descriptor().ToolURN().String()] = executor
	}
	return executors
}

func ListPlatformTools() []ToolDescriptor {
	tools := make([]ToolDescriptor, 0, len(registry))
	deps := Dependencies{
		Logger:           nil,
		DB:               nil,
		TelemetryService: nil,
		TriggerApp:       nil,
		SlackHTTPClient:  nil,
		Audit:            nil,
	}
	for _, factory := range registry {
		tools = append(tools, factory(deps).Descriptor())
	}
	return tools
}

func ListTypedTools(projectID uuid.UUID, urnPrefix string) []*types.Tool {
	tools := make([]*types.Tool, 0, len(registry))
	for _, descriptor := range ListPlatformTools() {
		if urnPrefix != "" && !strings.HasPrefix(descriptor.ToolURN().String(), urnPrefix) {
			continue
		}
		tools = append(tools, descriptor.ToTool(projectID))
	}
	return tools
}

func FindToolDescriptor(toolURN urn.Tool) (ToolDescriptor, bool) {
	for _, descriptor := range ListPlatformTools() {
		if descriptor.ToolURN().String() == toolURN.String() {
			return descriptor, true
		}
	}

	var zero ToolDescriptor
	return zero, false
}

func FindToolEntries(toolURNs []string) []*types.ToolEntry {
	entries := make([]*types.ToolEntry, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN)
		if !ok {
			continue
		}
		if _, ok := seen[descriptor.ToolURN().String()]; ok {
			continue
		}
		seen[descriptor.ToolURN().String()] = struct{}{}
		entries = append(entries, descriptor.ToToolEntry())
	}

	return entries
}

func FindTypedTools(projectID uuid.UUID, toolURNs []string) []*types.Tool {
	tools := make([]*types.Tool, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN)
		if !ok {
			continue
		}
		if _, ok := seen[descriptor.ToolURN().String()]; ok {
			continue
		}
		seen[descriptor.ToolURN().String()] = struct{}{}
		tools = append(tools, descriptor.ToTool(projectID))
	}

	return tools
}
