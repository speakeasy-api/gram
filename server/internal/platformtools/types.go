package platformtools

import (
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/platformtools/logs"
)

const (
	SourceLogs         = "logs"
	ToolNameSearchLogs = "platform_search_logs"
)

type ToolDescriptor = core.ToolDescriptor
type PlatformToolExecutor = core.PlatformToolExecutor
type TelemetryService = logs.TelemetryService
