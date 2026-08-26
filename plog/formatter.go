package plog

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Formatter formats log records for output.
type Formatter struct {
	cfg *Config
}

// NewFormatter creates a new Formatter with the given config.
func NewFormatter(cfg *Config) *Formatter {
	maybeForceColorProfile(cfg)
	return &Formatter{cfg: cfg}
}

var forceColorProfileOnce sync.Once

// maybeForceColorProfile rescues colorized output when it is enabled but the
// destination is not a TTY. lipgloss styles render through a global renderer
// whose color profile is detected from os.Stdout; when stdout is a pipe (for
// example when a process manager like pitchfork captures the daemon's output)
// that profile is Ascii and every ANSI escape is stripped, leaving plain text.
// We only ever upgrade an Ascii profile to TrueColor, so genuine terminals and
// an explicit NO_COLOR/CLICOLOR opt-out are left untouched.
func maybeForceColorProfile(cfg *Config) {
	if cfg.NoColor {
		return
	}
	forceColorProfileOnce.Do(func() {
		if termenv.EnvNoColor() {
			return
		}
		if lipgloss.ColorProfile() == termenv.Ascii {
			lipgloss.SetColorProfile(termenv.TrueColor)
		}
	})
}

// attrLinePrefix marks source/attribute continuation lines. It groups a
// record's details visually in a terminal and, because it is a single
// non-whitespace token at the start of the line, it also lets line-oriented log
// viewers that prefix every physical line with their own timestamp (such as
// pitchfork) treat it as the leading token: they re-absorb their timestamp
// instead of stranding it on the indented line, and the bar itself is consumed.
const attrLinePrefix = "│"

// attrLineSep separates the bar from the detail. The leading space is required:
// line-oriented viewers like pitchfork split the leading token on a space, so a
// bare tab there would fail their parse and strand the viewer's timestamp on the
// line. The trailing tab indents the detail (it survives into the viewer's
// output, which renders it as leading whitespace).
const attrLineSep = " \t"

// Format writes a formatted log record to the output.
//
// The record's message occupies the first line; its source location and
// attributes follow on their own lines, each introduced by a vertical bar:
//
//	<timestamp> <LEVEL> <message>
//	│	<source>
//	│	<key>: <value>
//
// When the timestamp is hidden the message line leads with the bar instead, so
// the leading token that line-oriented viewers discard (see attrLinePrefix) is
// never the level.
func (f *Formatter) Format(w io.Writer, record *Record) error {
	prefix := f.style(f.cfg.Theme.AttrBracket, attrLinePrefix)

	var parts []string

	// Leading token. Line-oriented viewers such as pitchfork discard the first
	// whitespace-delimited token of every line; the timestamp normally fills
	// that slot. When it is hidden we lead with the bar instead so the level is
	// never mistaken for the discarded token.
	if !f.cfg.HideTimestamp && !record.Time.IsZero() {
		ts := record.Time.Format("15:04:05.000")
		parts = append(parts, f.style(f.cfg.Theme.Timestamp, ts))
	} else {
		parts = append(parts, prefix)
	}

	// Level
	parts = append(parts, f.formatLevel(record))

	// Message
	if record.Message != "" {
		parts = append(parts, f.style(f.cfg.Theme.Message, record.Message))
	}

	// Write main log line
	if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
		return fmt.Errorf("writing log line: %w", err)
	}

	// Source
	if record.Source != nil {
		srcStr := record.Source.RelativePath(f.cfg.WorkingDir)
		if record.Source.Line > 0 {
			srcStr = fmt.Sprintf("%s:%d", srcStr, record.Source.Line)
		}
		srcStr = f.style(f.cfg.Theme.Source, srcStr)
		if _, err := fmt.Fprintf(w, "%s%s%s\n", prefix, attrLineSep, srcStr); err != nil {
			return fmt.Errorf("writing source: %w", err)
		}
	}

	// Attributes
	for _, attr := range record.Attrs {
		key := f.style(f.cfg.Theme.AttrKey, attr.Key)
		value := f.formatValue(attr.Value)
		if _, err := fmt.Fprintf(w, "%s%s%s: %s\n", prefix, attrLineSep, key, value); err != nil {
			return fmt.Errorf("writing attribute: %w", err)
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
