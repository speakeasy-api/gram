package plog_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/speakeasy-api/gram/plog"
)

var update = flag.Bool("update", false, "update golden files")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

func assertSnapshot(t *testing.T, name string, got string) {
	t.Helper()

	golden := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatalf("failed to update golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(golden) //nolint:gosec // golden file path is constructed from test name, not user input
	if err != nil {
		t.Fatalf("failed to read golden file %s: %v\nRun with -update to create it", golden, err)
	}

	if got != string(want) {
		t.Errorf("output mismatch for %s\n\ngot:\n%s\n\nwant:\n%s", name, got, string(want))
	}
}

func TestPrettyPrint_BasicLevels(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"trace","msg":"trace message"}
{"time":"2025-01-30T12:00:01.000Z","level":"debug","msg":"debug message"}
{"time":"2025-01-30T12:00:02.000Z","level":"info","msg":"info message"}
{"time":"2025-01-30T12:00:03.000Z","level":"warn","msg":"warning message"}
{"time":"2025-01-30T12:00:04.000Z","level":"error","msg":"error message"}
{"time":"2025-01-30T12:00:05.000Z","level":"fatal","msg":"fatal message"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true), plog.WithLevel(plog.LevelTrace))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "basic_levels", buf.String())
}

func TestPrettyPrint_MinLevel(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"trace","msg":"trace message"}
{"time":"2025-01-30T12:00:01.000Z","level":"debug","msg":"debug message"}
{"time":"2025-01-30T12:00:02.000Z","level":"info","msg":"info message"}
{"time":"2025-01-30T12:00:03.000Z","level":"warn","msg":"warning message"}
{"time":"2025-01-30T12:00:04.000Z","level":"error","msg":"error message"}
{"time":"2025-01-30T12:00:05.000Z","level":"fatal","msg":"fatal message"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true), plog.WithLevel(plog.LevelInfo))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "min_level_info", buf.String())
}

func TestPrettyPrint_Attributes(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"request handled","method":"GET","path":"/api/users","status":200,"latency_ms":42.5}
{"time":"2025-01-30T12:00:01.000Z","level":"error","msg":"database error","error":"connection timeout","retry":true,"attempts":3}
{"time":"2025-01-30T12:00:02.000Z","level":"info","msg":"user created","user":{"id":123,"name":"John Doe"}}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "attributes", buf.String())
}

func TestPrettyPrint_SourceLocations(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"string source","source":"server.go:42"}
{"time":"2025-01-30T12:00:01.000Z","level":"info","msg":"object source","source":{"file":"handler.go","line":128}}
{"time":"2025-01-30T12:00:02.000Z","level":"info","msg":"object with function","source":{"file":"service.go","line":256,"function":"ProcessRequest"}}
{"time":"2025-01-30T12:00:03.000Z","level":"info","msg":"caller key","caller":"middleware.go:64"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "source_locations", buf.String())
}

func TestPrettyPrint_TimestampFormats(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"RFC3339 with Z"}
{"time":"2025-01-30T12:00:00.000000000Z","level":"info","msg":"RFC3339Nano"}
{"time":"2025-01-30T12:00:00+00:00","level":"info","msg":"RFC3339 with offset"}
{"ts":1738238400,"level":"info","msg":"Unix seconds"}
{"ts":1738238400000,"level":"info","msg":"Unix milliseconds"}
{"ts":1738238400000000,"level":"info","msg":"Unix microseconds"}
{"ts":1738238400000000000,"level":"info","msg":"Unix nanoseconds"}
{"timestamp":"2025-01-30T12:00:00.000Z","level":"info","msg":"timestamp key"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "timestamp_formats", buf.String())
}

func TestPrettyPrint_MessageKeys(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"using msg key"}
{"time":"2025-01-30T12:00:01.000Z","level":"info","message":"using message key"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "message_keys", buf.String())
}

func TestPrettyPrint_CustomKeys(t *testing.T) {
	t.Parallel()
	input := `{"@timestamp":"2025-01-30T12:00:00.000Z","severity":"INFO","text":"custom keys log","src":"main.go:10"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(
		strings.NewReader(input),
		plog.WithOutput(&buf),
		plog.WithNoColor(true),
		plog.WithTimestampKeys("@timestamp"),
		plog.WithLevelKeys("severity"),
		plog.WithMessageKeys("text"),
		plog.WithSourceKeys("src"),
	)
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "custom_keys", buf.String())
}

func TestPrettyPrint_NonJSON(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"valid json"}
this is not json
{"time":"2025-01-30T12:00:01.000Z","level":"info","msg":"another valid json"}
also not json: some log output
{"time":"2025-01-30T12:00:02.000Z","level":"error","msg":"final json"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "non_json", buf.String())
}

func TestPrettyPrint_SlogNumericLevels(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":-8,"msg":"trace level"}
{"time":"2025-01-30T12:00:01.000Z","level":-4,"msg":"debug level"}
{"time":"2025-01-30T12:00:02.000Z","level":0,"msg":"info level"}
{"time":"2025-01-30T12:00:03.000Z","level":4,"msg":"warn level"}
{"time":"2025-01-30T12:00:04.000Z","level":8,"msg":"error level"}
{"time":"2025-01-30T12:00:05.000Z","level":12,"msg":"fatal level"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "slog_numeric_levels", buf.String())
}

func TestPrettyPrint_SpecialValues(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"special values","null_val":null,"bool_true":true,"bool_false":false,"empty_string":"","array":[1,2,3]}
{"time":"2025-01-30T12:00:01.000Z","level":"info","msg":"string with spaces","description":"this has spaces"}
{"time":"2025-01-30T12:00:02.000Z","level":"info","msg":"string with quotes","query":"SELECT * FROM \"users\""}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "special_values", buf.String())
}

func TestPrettyPrint_EmptyAndMinimal(t *testing.T) {
	t.Parallel()
	input := `{}
{"level":"info"}
{"msg":"just a message"}
{"time":"2025-01-30T12:00:00.000Z"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(strings.NewReader(input), plog.WithOutput(&buf), plog.WithNoColor(true))
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "empty_and_minimal", buf.String())
}

func TestHandler_BasicOutput(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true))

	// Use a fixed time for deterministic output
	logger.Info("server started", "port", 8080)
	logger.Debug("processing request", "request_id", "abc123")
	logger.Warn("high memory usage", "percent", 85.5)
	logger.Error("connection failed", "error", "timeout")

	// The output will have variable timestamps, so we need to normalize them
	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_basic", output)
}

func TestHandler_WithAttrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true))

	childLogger := logger.With("service", "api", "version", "1.0.0")
	childLogger.Info("request handled", "path", "/users")
	childLogger.Error("request failed", "path", "/orders", "error", "not found")

	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_with_attrs", output)
}

func TestHandler_WithGroup(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true))

	dbLogger := logger.WithGroup("db")
	dbLogger.Info("connected", "host", "localhost", "port", 5432)

	cacheLogger := logger.WithGroup("cache")
	cacheLogger.Info("hit", "key", "user:123")

	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_with_group", output)
}

func TestHandler_WithSource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true), plog.WithAddSource(true))

	logger.Info("message with source")

	// Normalize both timestamp and source path for deterministic output
	output := normalizeTimestamps(buf.String())
	output = normalizeSourcePaths(output)
	assertSnapshot(t, "handler_with_source", output)
}

func TestHandler_NestedGroups(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true))

	nested := logger.WithGroup("outer").WithGroup("inner")
	nested.Info("deeply nested", "key", "value")

	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_nested_groups", output)
}

func TestHandler_ComplexValues(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true))

	logger.Info("complex values",
		"duration", time.Second*5,
		"timestamp", time.Date(2025, 1, 30, 12, 0, 0, 0, time.UTC),
		"count", int64(42),
		"ratio", 0.75,
		"enabled", true,
	)

	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_complex_values", output)
}

func TestHandler_MinLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := plog.NewLogger(&buf, plog.WithNoColor(true), plog.WithLevel(plog.LevelWarn))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warning message")
	logger.Error("error message")

	output := normalizeTimestamps(buf.String())
	assertSnapshot(t, "handler_min_level", output)
}

func TestPrettyPrint_PartialTheme(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"info message"}
{"time":"2025-01-30T12:00:01.000Z","level":"error","msg":"error message"}
`
	// Only override LevelError, other styles should come from default theme
	customTheme := plog.Theme{
		LevelError: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5500")).Bold(true),
	}

	var buf bytes.Buffer
	err := plog.PrettyPrint(
		strings.NewReader(input),
		plog.WithOutput(&buf),
		plog.WithNoColor(true),
		plog.WithLevel(plog.LevelTrace),
		plog.WithTheme(customTheme),
	)
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	// With NoColor, output should still work (theme merge doesn't affect no-color mode)
	assertSnapshot(t, "partial_theme", buf.String())
}

func TestPrettyPrint_OmitKeys(t *testing.T) {
	t.Parallel()
	input := `{"time":"2025-01-30T12:00:00.000Z","level":"info","msg":"user login","user_id":"123","request_id":"abc","session_token":"secret123","path":"/login"}
{"time":"2025-01-30T12:00:01.000Z","level":"info","msg":"api call","trace_id":"xyz","span_id":"456","api_key":"hidden","endpoint":"/api/users"}
`

	var buf bytes.Buffer
	err := plog.PrettyPrint(
		strings.NewReader(input),
		plog.WithOutput(&buf),
		plog.WithNoColor(true),
		plog.WithLevel(plog.LevelTrace),
		plog.WithOmitKeys("*_id", "*_token", "*_key"),
	)
	if err != nil {
		t.Fatalf("PrettyPrint failed: %v", err)
	}

	assertSnapshot(t, "omit_keys", buf.String())
}

// normalizeTimestamps replaces timestamps with a fixed value for deterministic tests
func normalizeTimestamps(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if len(line) >= 12 && line[2] == ':' && line[5] == ':' {
			lines[i] = "00:00:00.000" + line[12:]
		}
	}
	return strings.Join(lines, "\n")
}

// normalizeSourcePaths replaces source file paths with a fixed value
func normalizeSourcePaths(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Look for pattern like "filename.go:123"
		parts := strings.Fields(line)
		for j, part := range parts {
			if strings.Contains(part, ".go:") && !strings.HasPrefix(part, "error=") {
				parts[j] = "test_file.go:1"
			}
		}
		lines[i] = strings.Join(parts, " ")
	}
	return strings.Join(lines, "\n")
}
