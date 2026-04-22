package assistants

import "log/slog"

const (
	logKeyThreadID  = "assistant.thread_id"
	logKeyEventID   = "assistant.event_id"
	logKeyRuntimeID = "assistant.runtime_id"
	logKeyAttempt   = "assistant.attempt"
	logKeyServerIP  = "assistant.server_ip"
)

func slogThreadID(v string) slog.Attr  { return slog.String(logKeyThreadID, v) }
func slogEventID(v string) slog.Attr   { return slog.String(logKeyEventID, v) }
func slogRuntimeID(v string) slog.Attr { return slog.String(logKeyRuntimeID, v) }
func slogAttempt(v int) slog.Attr      { return slog.Int(logKeyAttempt, v) }
func slogServerIP(v string) slog.Attr  { return slog.String(logKeyServerIP, v) }
