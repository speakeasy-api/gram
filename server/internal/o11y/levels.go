package o11y

import (
	"log/slog"

	"github.com/jackc/pgx/v5/tracelog"
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

var pgxLevels = map[tracelog.LogLevel]slog.Level{
	tracelog.LogLevelNone:  slog.LevelDebug,
	tracelog.LogLevelTrace: slog.LevelDebug,
	tracelog.LogLevelDebug: slog.LevelDebug,
	tracelog.LogLevelInfo:  slog.LevelInfo,
	tracelog.LogLevelWarn:  slog.LevelWarn,
	tracelog.LogLevelError: slog.LevelError,
}
