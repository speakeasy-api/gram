package platformtools

import (
	"context"
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

// BuildExecutors materializes executors for built-in plus caller-supplied
// platform tools. The second return value maps URN to the required feature
// gate; absent entries are ungated.
func BuildExecutors(deps Dependencies, extras ...ExternalTool) (map[string]PlatformToolExecutor, map[string]string) {
	executors := make(map[string]PlatformToolExecutor, len(registry)+len(extras))
	gates := map[string]string{}
	for _, factory := range registry {
		executor := factory(deps)
		executors[executor.Descriptor().ToolURN().String()] = executor
	}
	for _, extra := range extras {
		if extra.Executor == nil {
			continue
		}
		urnStr := extra.Executor.Descriptor().ToolURN().String()
		executors[urnStr] = extra.Executor
		if extra.RequiredFeature != "" {
			gates[urnStr] = extra.RequiredFeature
		}
	}
	return executors, gates
}

// ListPlatformTools returns descriptors without applying feature gates; callers
// that need per-org gating should use ListTypedTools instead.
func ListPlatformTools(extras ...ExternalTool) []ToolDescriptor {
	tools := make([]ToolDescriptor, 0, len(registry)+len(extras))
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
	for _, extra := range extras {
		if extra.Executor == nil {
			continue
		}
		tools = append(tools, extra.Executor.Descriptor())
	}
	return tools
}

// ListTypedTools enumerates platform tools available to organizationID,
// excluding any whose required feature flag is disabled. A nil checker grants
// access to every gated tool.
func ListTypedTools(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	urnPrefix string,
	checker FeatureChecker,
	extras ...ExternalTool,
) []*types.Tool {
	tools := make([]*types.Tool, 0, len(registry)+len(extras))
	deps := Dependencies{
		Logger:           nil,
		DB:               nil,
		TelemetryService: nil,
		TriggerApp:       nil,
		SlackHTTPClient:  nil,
		Audit:            nil,
	}
	for _, factory := range registry {
		descriptor := factory(deps).Descriptor()
		if urnPrefix != "" && !strings.HasPrefix(descriptor.ToolURN().String(), urnPrefix) {
			continue
		}
		tools = append(tools, descriptor.ToTool(projectID))
	}
	for _, extra := range extras {
		if extra.Executor == nil {
			continue
		}
		if extra.RequiredFeature != "" && checker != nil {
			if !checker(ctx, organizationID, extra.RequiredFeature) {
				continue
			}
		}
		descriptor := extra.Executor.Descriptor()
		if urnPrefix != "" && !strings.HasPrefix(descriptor.ToolURN().String(), urnPrefix) {
			continue
		}
		tools = append(tools, descriptor.ToTool(projectID))
	}
	return tools
}

func FindToolDescriptor(toolURN urn.Tool, extras ...ExternalTool) (ToolDescriptor, bool) {
	for _, descriptor := range ListPlatformTools(extras...) {
		if descriptor.ToolURN().String() == toolURN.String() {
			return descriptor, true
		}
	}

	var zero ToolDescriptor
	return zero, false
}

func FindToolEntries(toolURNs []string, extras ...ExternalTool) []*types.ToolEntry {
	entries := make([]*types.ToolEntry, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN, extras...)
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

func FindTypedTools(projectID uuid.UUID, toolURNs []string, extras ...ExternalTool) []*types.Tool {
	tools := make([]*types.Tool, 0, len(toolURNs))
	seen := make(map[string]struct{}, len(toolURNs))
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		descriptor, ok := FindToolDescriptor(toolURN, extras...)
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
