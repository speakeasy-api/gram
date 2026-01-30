package plog

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Formatter formats log records for output.
type Formatter struct {
	cfg *Config
}

// NewFormatter creates a new Formatter with the given config.
func NewFormatter(cfg *Config) *Formatter {
	return &Formatter{cfg: cfg}
}

// Format writes a formatted log record to the output.
func (f *Formatter) Format(w io.Writer, record *Record) error {
	var parts []string

	// Timestamp
	if !f.cfg.HideTimestamp && !record.Time.IsZero() {
		ts := record.Time.Format("15:04:05.000")
		ts = f.style(f.cfg.Theme.Timestamp, ts)
		parts = append(parts, ts)
	}

	// Level
	levelStr := f.formatLevel(record)
	parts = append(parts, levelStr)

	// Message
	if record.Message != "" {
		msg := f.style(f.cfg.Theme.Message, record.Message)
		parts = append(parts, msg)
	}

	// Write main log line
	if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
		return fmt.Errorf("writing log line: %w", err)
	}

	// Write source and attributes on separate indented lines
	hasDetails := record.Source != nil || len(record.Attrs) > 0
	if hasDetails {
		// Source
		if record.Source != nil {
			srcStr := record.Source.RelativePath(f.cfg.WorkingDir)
			if record.Source.Line > 0 {
				srcStr = fmt.Sprintf("%s:%d", srcStr, record.Source.Line)
			}
			srcStr = f.style(f.cfg.Theme.Source, srcStr)
			if _, err := fmt.Fprintf(w, "    %s\n", srcStr); err != nil {
				return fmt.Errorf("writing source: %w", err)
			}
		}

		// Attributes
		for _, attr := range record.Attrs {
			key := f.style(f.cfg.Theme.AttrKey, attr.Key)
			value := f.formatValue(attr.Value)
			if _, err := fmt.Fprintf(w, "    %s: %s\n", key, value); err != nil {
				return fmt.Errorf("writing attribute: %w", err)
			}
		}

		// Blank line after details
		if _, err := fmt.Fprintln(w); err != nil {
			return fmt.Errorf("writing blank line: %w", err)
		}
	}

	return nil
}

// formatLevel formats the level with appropriate coloring.
func (f *Formatter) formatLevel(record *Record) string {
	name := strings.ToUpper(record.LevelRaw)
	if name == "" {
		name = "INFO"
	}

	// Pad to 5 characters for alignment
	name = fmt.Sprintf("%-5s", name)
	if len(name) > 5 {
		name = name[:5]
	}

	var style lipgloss.Style
	switch record.Level {
	case LevelTrace:
		style = f.cfg.Theme.LevelTrace
	case LevelDebug:
		style = f.cfg.Theme.LevelDebug
	case LevelInfo:
		style = f.cfg.Theme.LevelInfo
	case LevelWarn:
		style = f.cfg.Theme.LevelWarn
	case LevelError:
		style = f.cfg.Theme.LevelError
	case LevelFatal:
		style = f.cfg.Theme.LevelFatal
	default:
		style = f.cfg.Theme.LevelInfo
	}

	return f.style(style, name)
}

// formatValue formats a value for display.
func (f *Formatter) formatValue(v any) string {
	var s string
	switch val := v.(type) {
	case string:
		if strings.ContainsAny(val, " \t\n\"") {
			s = fmt.Sprintf("%q", val)
		} else {
			s = val
		}
	case float64:
		if val == float64(int64(val)) {
			s = fmt.Sprintf("%d", int64(val))
		} else {
			s = fmt.Sprintf("%g", val)
		}
	case bool:
		s = fmt.Sprintf("%t", val)
	case nil:
		s = "null"
	case map[string]any, []any:
		b, _ := json.Marshal(val)
		s = string(b)
	default:
		s = fmt.Sprintf("%v", val)
	}
	return f.style(f.cfg.Theme.AttrValue, s)
}

// style applies a lipgloss style if colors are enabled.
func (f *Formatter) style(s lipgloss.Style, str string) string {
	if f.cfg.NoColor {
		return str
	}
	return s.Render(str)
}
