# plog

A pretty-printer for JSON logs with colorized output. Use it as a standalone CLI to pipe logs through, or as a `slog.Handler` for direct integration in Go applications.

## Installation

### CLI

```bash
go install github.com/speakeasy-api/gram/plog/cmd/plog@latest
```

### Library

```bash
go get github.com/speakeasy-api/gram/plog
```

## CLI Usage

Pipe JSON logs through `plog` to format them:

```bash
# From a file
cat app.log | plog

# From a running process
./myapp 2>&1 | plog

# Filter to warnings and above
./myapp 2>&1 | plog --level=warn
```

### Example

Input:

```json
{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"Server started","port":8080}
{"time":"2025-01-30T12:00:01.000Z","level":"error","msg":"Connection failed","error":"timeout","host":"db.example.com"}
```

Output:

```
12:00:00.000 INFO  Server started
    port: 8080

12:00:01.000 ERROR Connection failed
    error: timeout
    host: db.example.com
```

### CLI Flags

| Flag               | Default             | Description                                                                   |
| ------------------ | ------------------- | ----------------------------------------------------------------------------- |
| `--level`          | `info`              | Minimum level to display (trace, debug, info, warn, error, fatal)             |
| `--omit`           |                     | Comma-separated field patterns to omit (supports globs, e.g., `*_id,secret*`) |
| `--level-keys`     | `level`             | Comma-separated keys to look for log level                                    |
| `--message-keys`   | `msg,message`       | Comma-separated keys to look for log message                                  |
| `--timestamp-keys` | `time,ts,timestamp` | Comma-separated keys to look for timestamp                                    |
| `--source-keys`    | `source,caller`     | Comma-separated keys to look for source location                              |
| `--no-color`       | `false`             | Disable colorized output                                                      |

## slog.Handler Integration

Use `plog` as a handler for Go's `log/slog` package:

```go
package main

import (
    "log/slog"
    "github.com/speakeasy-api/gram/plog"
)

func main() {
    // Create a logger with pretty output
    logger := plog.DefaultLogger()

    logger.Info("Server started", "port", 8080)
    logger.Error("Connection failed", "error", "timeout")
}
```

### Set as Default Logger

```go
func main() {
    plog.SetDefault()

    slog.Info("Using the default logger", "key", "value")
}
```

### With Options

```go
logger := plog.DefaultLogger(
    plog.WithLevel(plog.LevelInfo),  // Suppress debug/trace
    plog.WithAddSource(true),            // Include source file:line
    plog.WithNoColor(true),              // Disable colors
)
```

### Write to Custom Output

```go
var buf bytes.Buffer
logger := plog.NewLogger(&buf, plog.WithNoColor(true))
```

## Configuration Options

| Option                       | Description                                                                                                       |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `WithLevel(level)`           | Set minimum log level (use `plog.LevelTrace`, `LevelDebug`, `LevelInfo`, `LevelWarn`, `LevelError`, `LevelFatal`) |
| `WithOmitKeys(patterns...)`  | Field patterns to omit from output (supports globs, e.g., `*_id`, `secret*`)                                      |
| `WithAddSource(bool)`        | Include source file and line number                                                                               |
| `WithNoColor(bool)`          | Disable colorized output                                                                                          |
| `WithOutput(io.Writer)`      | Set output destination                                                                                            |
| `WithLevelKeys(keys...)`     | Keys to search for log level                                                                                      |
| `WithMessageKeys(keys...)`   | Keys to search for message                                                                                        |
| `WithTimestampKeys(keys...)` | Keys to search for timestamp                                                                                      |
| `WithSourceKeys(keys...)`    | Keys to search for source location                                                                                |
| `WithWorkingDir(dir)`        | Base directory for relative source paths                                                                          |
| `WithTheme(theme)`           | Custom color theme (partial themes are merged with defaults)                                                      |

## Processing Existing Logs

Use `PrettyPrint` to process JSON logs from any `io.Reader`:

```go
file, _ := os.Open("app.log")
defer file.Close()

plog.PrettyPrint(file,
    plog.WithLevel(plog.LevelWarn),
    plog.WithNoColor(true),
)
```

## Features

- **Automatic timestamp parsing**: RFC3339, Unix seconds/milliseconds/microseconds/nanoseconds
- **Flexible source locations**: Parses both `"file:line"` strings and `{"file":"...", "line":42}` objects
- **Non-JSON passthrough**: Lines that aren't valid JSON are passed through unchanged
- **Sorted attributes**: Extra fields are displayed alphabetically for consistent output
- **Color themes**: Customizable colors with hex code support

## License

MIT
