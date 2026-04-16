package platformtools

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
)

const (
	SourceLogs               = "logs"
	SourceTriggers           = "triggers"
	ToolNameSearchLogs       = "platform_search_logs"
	ToolNameListTriggers     = "platform_list_triggers"
	ToolNameConfigureTrigger = "platform_configure_trigger"
)

type Dependencies struct {
	Logger           *slog.Logger
	DB               *pgxpool.Pool
	TelemetryService TelemetryService
	TriggerApp       *bgtriggers.App
}

type ToolDescriptor = core.ToolDescriptor
type PlatformToolExecutor = core.PlatformToolExecutor
type TelemetryService = logs.TelemetryService
