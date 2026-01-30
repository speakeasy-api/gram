// Command plog pretty-prints JSON lines logs with colorized output.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/speakeasy-api/gram/plog"
)

func main() {
	var (
		levelKeys     string
		messageKeys   string
		timestampKeys string
		sourceKeys    string
		omitKeys      string
		level         string
		noColor       bool
	)

	flag.StringVar(&levelKeys, "level-keys", "level", "comma-separated keys to look for log level")
	flag.StringVar(&messageKeys, "message-keys", "msg,message", "comma-separated keys to look for log message")
	flag.StringVar(&timestampKeys, "timestamp-keys", "time,ts,timestamp", "comma-separated keys to look for timestamp")
	flag.StringVar(&sourceKeys, "source-keys", "source,caller", "comma-separated keys to look for source location")
	flag.StringVar(&omitKeys, "omit", "", "comma-separated field patterns to omit (supports globs, e.g., \"*_id,secret*\")")
	flag.StringVar(&level, "level", "info", "minimum level to display (trace, debug, info, warn, error, fatal)")
	flag.BoolVar(&noColor, "no-color", false, "disable colorized output")
	flag.Parse()

	opts := []plog.Option{
		plog.WithNoColor(noColor),
		plog.WithLevel(parseLevel(level)),
	}

	if levelKeys != "" {
		opts = append(opts, plog.WithLevelKeys(splitKeys(levelKeys)...))
	}
	if messageKeys != "" {
		opts = append(opts, plog.WithMessageKeys(splitKeys(messageKeys)...))
	}
	if timestampKeys != "" {
		opts = append(opts, plog.WithTimestampKeys(splitKeys(timestampKeys)...))
	}
	if sourceKeys != "" {
		opts = append(opts, plog.WithSourceKeys(splitKeys(sourceKeys)...))
	}
	if omitKeys != "" {
		opts = append(opts, plog.WithOmitKeys(splitKeys(omitKeys)...))
	}

	if err := plog.PrettyPrint(os.Stdin, opts...); err != nil {
		fmt.Fprintf(os.Stderr, "plog: %v\n", err)
		os.Exit(1)
	}
}

func splitKeys(s string) []string {
	parts := strings.Split(s, ",")
	keys := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			keys = append(keys, p)
		}
	}
	return keys
}

func parseLevel(s string) int {
	switch strings.ToLower(s) {
	case "trace":
		return plog.LevelTrace
	case "debug":
		return plog.LevelDebug
	case "info":
		return plog.LevelInfo
	case "warn", "warning":
		return plog.LevelWarn
	case "error":
		return plog.LevelError
	case "fatal":
		return plog.LevelFatal
	default:
		return plog.LevelInfo
	}
}
