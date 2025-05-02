package o11y

import (
	"log/slog"

	"github.com/charmbracelet/log"
	"github.com/jackc/pgx/v5/tracelog"
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

var pgxLevels = map[tracelog.LogLevel]slog.Level{
	tracelog.LogLevelNone:  slog.LevelDebug,
	tracelog.LogLevelTrace: slog.LevelDebug,
	tracelog.LogLevelDebug: slog.LevelDebug,
	tracelog.LogLevelInfo:  slog.LevelInfo,
	tracelog.LogLevelWarn:  slog.LevelWarn,
	tracelog.LogLevelError: slog.LevelError,
}
