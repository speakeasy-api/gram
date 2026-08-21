package runtime

import (
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	platformchats "github.com/speakeasy-api/gram/server/internal/platformtools/chats"
	platformdeployments "github.com/speakeasy-api/gram/server/internal/platformtools/deployments"
	platformplugins "github.com/speakeasy-api/gram/server/internal/platformtools/plugins"
	platformrisk "github.com/speakeasy-api/gram/server/internal/platformtools/risk"
	platformskills "github.com/speakeasy-api/gram/server/internal/platformtools/skills"
	platformusers "github.com/speakeasy-api/gram/server/internal/platformtools/users"
	feedbackrecorder "github.com/speakeasy-api/gram/server/internal/skills/feedback"
)

// A platform toolset is composed once, here, and the composition is the only
// statement of what it contains. The managed assistant's system prompt names
// its tools inline so a turn needs no discovery search, and a drift test holds
// the prompt to these functions — which is only worth anything if they are
// what the server actually serves, rather than a second list kept in step by
// hand.

// AssistantsToolsetDeps carries the services behind the base "assistants"
// platform toolset, granted to every assistant runtime.
type AssistantsToolsetDeps struct {
	Memory           *memory.MemoryService
	Logger           *slog.Logger
	DB               *pgxpool.Pool
	FeedbackRecorder *feedbackrecorder.Recorder
	SkillLoadOptions []platformskills.LoadOption
	Triggers         *bgtriggers.App
	Audit            *audit.Logger
}

// ManagedAssistantToolsetDeps carries the services behind the
// "managed-assistant" platform toolset — the managed-only insights, chat,
// user, risk, deployment, skill, plugin, docs and changelog tools.
//
// MarketingSite must be a guardian-policy client: the changelog and docs tools
// fetch speakeasy.com, and the egress rules apply to them like any other
// outbound request.
type ManagedAssistantToolsetDeps struct {
	Telemetry     platformtools.TelemetryService
	Chats         platformchats.ChatService
	Users         platformusers.OrganizationsService
	Risk          platformrisk.RiskService
	Deployments   platformdeployments.DeploymentsService
	Skills        platformskills.SkillsService
	SkillInsights platformskills.SkillInsightsReader
	Plugins       platformplugins.PluginsService
	MarketingSite *guardian.HTTPClient
}

// AssistantsToolset composes the base "assistants" platform toolset.
func AssistantsToolset(deps AssistantsToolsetDeps) []platformtools.ExternalTool {
	return slices.Concat(
		MemoryExternalTools(deps.Memory),
		AssistantSkillTools(deps.Logger, deps.DB, deps.FeedbackRecorder, deps.SkillLoadOptions...),
		TriggerExternalTools(deps.DB, deps.Triggers, deps.Audit),
	)
}

// ManagedAssistantToolset composes the "managed-assistant" platform toolset.
func ManagedAssistantToolset(deps ManagedAssistantToolsetDeps) []platformtools.ExternalTool {
	return slices.Concat(
		ManagedAssistantLogsTools(deps.Telemetry),
		ManagedAssistantChatsTools(deps.Chats),
		ManagedAssistantUsersTools(deps.Users),
		ManagedAssistantRiskTools(deps.Risk),
		ManagedAssistantDeploymentsTools(deps.Deployments),
		ManagedAssistantSkillsTools(deps.Skills, deps.SkillInsights),
		ManagedAssistantPluginsTools(deps.Plugins),
		ManagedAssistantChangelogTools(deps.MarketingSite),
		ManagedAssistantDocsTools(deps.MarketingSite),
	)
}

// AssistantsToolsetToolNames and ManagedAssistantToolsetToolNames return the
// names each toolset serves, for the prompt drift tests.
//
// Both compose with zero dependencies: a tool's Descriptor is static and never
// touches its service, so the names are exact while the executors are inert.
// Nothing here may be used to serve traffic.

func AssistantsToolsetToolNames() []string {
	return externalToolNames(AssistantsToolset(AssistantsToolsetDeps{
		Memory:           nil,
		Logger:           nil,
		DB:               nil,
		FeedbackRecorder: nil,
		SkillLoadOptions: nil,
		Triggers:         nil,
		Audit:            nil,
	}))
}

func ManagedAssistantToolsetToolNames() []string {
	return externalToolNames(ManagedAssistantToolset(ManagedAssistantToolsetDeps{
		Telemetry:     nil,
		Chats:         nil,
		Users:         nil,
		Risk:          nil,
		Deployments:   nil,
		Skills:        nil,
		SkillInsights: nil,
		Plugins:       nil,
		MarketingSite: nil,
	}))
}

func externalToolNames(tools []platformtools.ExternalTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Executor.Descriptor().Name)
	}
	slices.Sort(names)
	return names
}
