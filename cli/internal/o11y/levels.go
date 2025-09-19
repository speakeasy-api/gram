package o11y

import (
	"log/slog"

	charmlog "github.com/charmbracelet/log"
)

type LevelMapping struct {
	Slog  slog.Level
	Charm charmlog.Level
}

var Levels = map[string]LevelMapping{
	"debug": {Slog: slog.LevelDebug, Charm: charmlog.DebugLevel},
	"info":  {Slog: slog.LevelInfo, Charm: charmlog.InfoLevel},
	"warn":  {Slog: slog.LevelWarn, Charm: charmlog.WarnLevel},
	"error": {Slog: slog.LevelError, Charm: charmlog.ErrorLevel},
}
