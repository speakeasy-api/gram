package o11y

import (
	"log/slog"

	"github.com/charmbracelet/log"
)

type LevelMapping struct {
	Slog  slog.Level
	Charm log.Level
}

var LogLevels = map[string]LevelMapping{
	"debug": {Slog: slog.LevelDebug, Charm: log.DebugLevel},
	"info":  {Slog: slog.LevelInfo, Charm: log.InfoLevel},
	"warn":  {Slog: slog.LevelWarn, Charm: log.WarnLevel},
	"error": {Slog: slog.LevelError, Charm: log.ErrorLevel},
}
