package plog

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Attr represents a key-value attribute from a log record.
type Attr struct {
	Key   string
	Value any
}

// Record represents a parsed log record.
type Record struct {
	Level    int
	LevelRaw string
	Message  string
	Time     time.Time
	Source   *Source
	Attrs    []Attr
}

// Parse parses a JSON log line into a Record using the given config.
func Parse(line []byte, cfg *Config) (*Record, error) {
	var data map[string]any
	if err := json.Unmarshal(line, &data); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}

	record := &Record{
		Level:    0,
		LevelRaw: "",
		Message:  "",
		Time:     time.Time{},
		Source:   nil,
		Attrs:    nil,
	}
	consumed := make(map[string]bool)

	// Parse level
	for _, key := range cfg.LevelKeys {
		if v, ok := data[key]; ok {
			record.LevelRaw = levelToString(v)
			record.Level = parseLevelValue(record.LevelRaw, cfg.LevelMap)
			consumed[key] = true
			break
		}
	}

	// Parse message
	for _, key := range cfg.MessageKeys {
		if v, ok := data[key]; ok {
			if s, ok := v.(string); ok {
				record.Message = s
				consumed[key] = true
				break
			}
		}
	}

	// Parse timestamp
	for _, key := range cfg.TimestampKeys {
		if v, ok := data[key]; ok {
			record.Time = parseTimestamp(v)
			consumed[key] = true
			break
		}
	}

	// Parse source
	for _, key := range cfg.SourceKeys {
		if v, ok := data[key]; ok {
			record.Source = parseSource(v)
			consumed[key] = true
			break
		}
	}

	// Collect remaining attributes, sorted alphabetically
	var attrs []Attr
	for k, v := range data {
		if !consumed[k] && !cfg.ShouldOmit(k) {
			attrs = append(attrs, Attr{Key: k, Value: v})
		}
	}
	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].Key < attrs[j].Key
	})
	record.Attrs = attrs

	return record, nil
}

// levelToString converts a level value to a string.
func levelToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return levelNumberToString(int(val))
	case int:
		return levelNumberToString(val)
	default:
		return ""
	}
}

// levelNumberToString converts a numeric slog-style level to a string.
func levelNumberToString(n int) string {
	switch {
	case n <= -8:
		return "trace"
	case n <= -4:
		return "debug"
	case n <= 0:
		return "info"
	case n <= 4:
		return "warn"
	case n <= 8:
		return "error"
	default:
		return "fatal"
	}
}

// parseLevelValue looks up a level string in the level map (case-insensitive).
func parseLevelValue(level string, levelMap map[string]int) int {
	if level == "" {
		return LevelInfo
	}
	lower := strings.ToLower(level)
	if v, ok := levelMap[lower]; ok {
		return v
	}
	// Try common prefixes (e.g., "warning" -> "warn")
	for k, v := range levelMap {
		if strings.HasPrefix(lower, k) || strings.HasPrefix(k, lower) {
			return v
		}
	}
	return LevelInfo
}
