package platformtools

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
)

const (
	SourceLogs                     = "logs"
	SourceSlack                    = "slack"
	SourceTriggers                 = "triggers"
	ToolNameSearchLogs             = "platform_search_logs"
	ToolNameListTriggers           = "platform_list_triggers"
	ToolNameConfigureTrigger       = "platform_configure_trigger"
	ToolNameReadChannelMessages    = "platform_slack_read_channel_messages"
	ToolNameReadThreadMessages     = "platform_slack_read_thread_messages"
	ToolNameReadUserProfile        = "platform_slack_read_user_profile"
	ToolNameSearchChannels         = "platform_slack_search_channels"
	ToolNameSearchMessagesAndFiles = "platform_slack_search_messages_and_files"
	ToolNameSearchUsers            = "platform_slack_search_users"
	ToolNameScheduleMessage        = "platform_slack_schedule_message"
	ToolNameSendMessage            = "platform_slack_send_message"
)

type Dependencies struct {
	Logger           *slog.Logger
	DB               *pgxpool.Pool
	TelemetryService TelemetryService
	TriggerApp       *bgtriggers.App
	SlackHTTPClient  *guardian.HTTPClient
	Audit            *audit.Logger
}

type ToolDescriptor = core.ToolDescriptor
type PlatformToolExecutor = core.PlatformToolExecutor
type TelemetryService = logs.TelemetryService
