package o11y

import (
	"log/slog"

	"github.com/speakeasy-api/gram/plog"
)

type LevelMapping struct {
	Slog slog.Level
	Plog int
}

var LogLevels = map[string]LevelMapping{
	"debug": {Slog: slog.LevelDebug, Plog: plog.LevelDebug},
	"info":  {Slog: slog.LevelInfo, Plog: plog.LevelInfo},
	"warn":  {Slog: slog.LevelWarn, Plog: plog.LevelWarn},
	"error": {Slog: slog.LevelError, Plog: plog.LevelError},
}
