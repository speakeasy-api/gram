package cmd

import (
	"log/slog"

	"github.com/charmbracelet/log"
)

type levelMapping struct {
	slog  slog.Level
	charm log.Level
}

var logLevels = map[string]levelMapping{
	"debug": {slog: slog.LevelDebug, charm: log.DebugLevel},
	"info":  {slog: slog.LevelInfo, charm: log.InfoLevel},
	"warn":  {slog: slog.LevelWarn, charm: log.WarnLevel},
	"error": {slog: slog.LevelError, charm: log.ErrorLevel},
}
