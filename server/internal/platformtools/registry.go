package platformtools

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	platformgooglechat "github.com/speakeasy-api/gram/server/internal/platformtools/googlechat"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
	platformslack "github.com/speakeasy-api/gram/server/internal/platformtools/slack"
	platformtriggers "github.com/speakeasy-api/gram/server/internal/platformtools/triggers"
	platformusersessions "github.com/speakeasy-api/gram/server/internal/platformtools/usersessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type toolFactory func(deps Dependencies) core.PlatformToolExecutor

var registry = []toolFactory{
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.WithReplyRecorder(platformgooglechat.NewSendMessageTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver), deps.GoogleChatReplyRecorder)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.WithReplyRecorder(platformgooglechat.NewReplyToThreadTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver), deps.GoogleChatReplyRecorder)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.NewUpdateMessageTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.NewDeleteMessageTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.NewListSpacesTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.NewGetMessageTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformgooglechat.NewListMessagesTool(deps.IntegrationHTTPClient, deps.GoogleChatTokenResolver)
	},

	func(deps Dependencies) PlatformToolExecutor {
		return logs.NewSearchLogsTool(deps.TelemetryService)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformusersessions.NewListUserSessionsTool(deps.DB)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformusersessions.NewGetUserSessionTool(deps.DB)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformtriggers.NewListTriggersTool(deps.DB, deps.TriggerApp)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformtriggers.NewConfigureTriggerTool(deps.DB, deps.TriggerApp, deps.Audit)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadChannelMessagesTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadThreadMessagesTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewReadUserProfileTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchChannelsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchMessagesAndFilesTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSearchUsersTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewScheduleMessageTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		var router platformslack.ThreadRouter
		if deps.TriggerApp != nil {
			router = deps.TriggerApp
		}
		return platformslack.NewSendMessageTool(deps.IntegrationHTTPClient, router)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewAddReactionTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRemoveReactionTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetReactionsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListReactionsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListEmojiTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatUpdateTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSetThreadStatusTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatDeleteTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatPostEphemeralTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatGetPermalinkTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatDeleteScheduledMessageTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatListScheduledMessagesTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewChatMeMessageTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetChannelInfoTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListChannelMembersTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewOpenConversationTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewCreateChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewJoinChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewLeaveChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewInviteToChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSetChannelTopicTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSetChannelPurposeTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewMarkConversationTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewArchiveChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewUnarchiveChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRenameChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRemoveFromChannelTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewLookupUserByEmailTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListUserConversationsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetUserPresenceTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetUserProfileFieldsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetUserDndTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetTeamDndTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewAddReminderTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListRemindersTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewCompleteReminderTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewDeleteReminderTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetReminderTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewUploadFileTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetFileInfoTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetFileURLTool(deps.IntegrationHTTPClient, deps.Encryption, deps.ServerURL)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListFilesTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewDeleteFileTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewPinMessageTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewUnpinMessageTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListPinsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewAddBookmarkTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewEditBookmarkTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRemoveBookmarkTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListBookmarksTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListUsergroupsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewListUsergroupMembersTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewGetTeamInfoTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewCreateCanvasTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewEditCanvasTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewDeleteCanvasTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewLookupCanvasSectionsTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewSetCanvasAccessTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewRemoveCanvasAccessTool(deps.IntegrationHTTPClient)
	},
	func(deps Dependencies) PlatformToolExecutor {
		return platformslack.NewCreateChannelCanvasTool(deps.IntegrationHTTPClient)
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
		Logger:                  nil,
		DB:                      nil,
		TelemetryService:        nil,
		TriggerApp:              nil,
		IntegrationHTTPClient:   nil,
		GoogleChatTokenResolver: nil,
		GoogleChatReplyRecorder: nil,
		Audit:                   nil,
		Encryption:              nil,
		ServerURL:               nil,
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
		Logger:                  nil,
		DB:                      nil,
		TelemetryService:        nil,
		TriggerApp:              nil,
		IntegrationHTTPClient:   nil,
		GoogleChatTokenResolver: nil,
		GoogleChatReplyRecorder: nil,
		Audit:                   nil,
		Encryption:              nil,
		ServerURL:               nil,
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

func indexToolDescriptors(extras ...ExternalTool) map[string]ToolDescriptor {
	all := ListPlatformTools(extras...)
	descriptorsByURN := make(map[string]ToolDescriptor, len(all))
	for _, descriptor := range all {
		toolURN := descriptor.ToolURN().String()
		if _, exists := descriptorsByURN[toolURN]; exists {
			continue
		}
		descriptorsByURN[toolURN] = descriptor
	}
	return descriptorsByURN
}

func findToolDescriptors(toolURNs []string, extras ...ExternalTool) []ToolDescriptor {
	var found []ToolDescriptor
	var seen map[string]struct{}
	var descriptorsByURN map[string]ToolDescriptor
	for _, rawURN := range toolURNs {
		var toolURN urn.Tool
		if err := toolURN.UnmarshalText([]byte(rawURN)); err != nil {
			continue
		}
		if toolURN.Kind != urn.ToolKindPlatform {
			continue
		}
		if descriptorsByURN == nil {
			descriptorsByURN = indexToolDescriptors(extras...)
		}
		descriptor, ok := descriptorsByURN[toolURN.String()]
		if !ok {
			continue
		}
		descriptorURN := descriptor.ToolURN().String()
		if _, ok := seen[descriptorURN]; ok {
			continue
		}
		if seen == nil {
			seen = make(map[string]struct{}, len(descriptorsByURN))
		}
		seen[descriptorURN] = struct{}{}
		found = append(found, descriptor)
	}
	return found
}

func FindToolEntries(toolURNs []string, extras ...ExternalTool) []*types.ToolEntry {
	descriptors := findToolDescriptors(toolURNs, extras...)
	entries := make([]*types.ToolEntry, 0, len(descriptors))
	for _, descriptor := range descriptors {
		entries = append(entries, descriptor.ToToolEntry())
	}
	return entries
}

func FindTypedTools(projectID uuid.UUID, toolURNs []string, extras ...ExternalTool) []*types.Tool {
	descriptors := findToolDescriptors(toolURNs, extras...)
	tools := make([]*types.Tool, 0, len(descriptors))
	for _, descriptor := range descriptors {
		tools = append(tools, descriptor.ToTool(projectID))
	}
	return tools
}
