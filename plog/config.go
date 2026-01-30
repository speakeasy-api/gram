package plog

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Level constants for log severity.
const (
	LevelTrace = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// Theme defines the color styles for log output.
type Theme struct {
	Timestamp   lipgloss.Style
	LevelTrace  lipgloss.Style
	LevelDebug  lipgloss.Style
	LevelInfo   lipgloss.Style
	LevelWarn   lipgloss.Style
	LevelError  lipgloss.Style
	LevelFatal  lipgloss.Style
	Message     lipgloss.Style
	Source      lipgloss.Style
	AttrKey     lipgloss.Style
	AttrValue   lipgloss.Style
	AttrBracket lipgloss.Style
}

func defaultStyle(value, def lipgloss.Style) lipgloss.Style {
	zero := lipgloss.Style{}

	if value.Value() == zero.Value() {
		return def
	}
	return value
}

// mergeWithDefault returns a new Theme with zero-value fields filled from the default theme.
func (t Theme) mergeWithDefault() Theme {
	def := DefaultTheme()

	t.Timestamp = defaultStyle(t.Timestamp, def.Timestamp)
	t.LevelTrace = defaultStyle(t.LevelTrace, def.LevelTrace)
	t.LevelDebug = defaultStyle(t.LevelDebug, def.LevelDebug)
	t.LevelInfo = defaultStyle(t.LevelInfo, def.LevelInfo)
	t.LevelWarn = defaultStyle(t.LevelWarn, def.LevelWarn)
	t.LevelError = defaultStyle(t.LevelError, def.LevelError)
	t.LevelFatal = defaultStyle(t.LevelFatal, def.LevelFatal)
	t.Message = defaultStyle(t.Message, def.Message)
	t.Source = defaultStyle(t.Source, def.Source)
	t.AttrKey = defaultStyle(t.AttrKey, def.AttrKey)
	t.AttrValue = defaultStyle(t.AttrValue, def.AttrValue)
	t.AttrBracket = defaultStyle(t.AttrBracket, def.AttrBracket)

	return t
}

// DefaultTheme returns a theme with hex colors.
func DefaultTheme() Theme {
	return Theme{
		Timestamp:   lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
		LevelTrace:  lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")).Bold(true),
		LevelDebug:  lipgloss.NewStyle().Foreground(lipgloss.Color("#00AAAA")).Bold(true),
		LevelInfo:   lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true),
		LevelWarn:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFF00")).Bold(true),
		LevelError:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true),
		LevelFatal:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FF00FF")).Bold(true),
		Message:     lipgloss.NewStyle(),
		Source:      lipgloss.NewStyle().Foreground(lipgloss.Color("#84A59D")),
		AttrKey:     lipgloss.NewStyle().Foreground(lipgloss.Color("#00AAAA")),
		AttrValue:   lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")),
		AttrBracket: lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")),
	}
}

// Config holds the configuration for pretty-printing logs.
type Config struct {
	LevelKeys     []string
	LevelMap      map[string]int
	MessageKeys   []string
	TimestampKeys []string
	SourceKeys    []string
	OmitKeys      []string
	WorkingDir    string
	Output        io.Writer
	NoColor       bool
	HideTimestamp bool
	AddSource     bool
	Level         int
	Theme         Theme
	omitMatcher   *OmitMatcher
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	wd, _ := os.Getwd()
	return &Config{
		LevelKeys:     []string{"level"},
		LevelMap:      DefaultLevelMap(),
		MessageKeys:   []string{"msg", "message"},
		TimestampKeys: []string{"time", "ts", "timestamp"},
		SourceKeys:    []string{"source", "caller"},
		WorkingDir:    wd,
		Output:        os.Stdout,
		NoColor:       false,
		Level:         LevelInfo,
		Theme:         DefaultTheme(),
		OmitKeys:      []string{},
		AddSource:     false,
		HideTimestamp: false,
		omitMatcher:   nil,
	}
}

// DefaultLevelMap returns the default mapping from level strings to level values.
func DefaultLevelMap() map[string]int {
	return map[string]int{
		"trace": LevelTrace,
		"debug": LevelDebug,
		"info":  LevelInfo,
		"warn":  LevelWarn,
		"error": LevelError,
		"fatal": LevelFatal,
	}
}

// Option is a functional option for configuring the pretty printer.
type Option func(*Config)

// WithLevelKeys sets the keys to look for the log level.
func WithLevelKeys(keys ...string) Option {
	return func(c *Config) {
		c.LevelKeys = keys
	}
}

// WithLevelMap sets the mapping from level strings to level values.
func WithLevelMap(m map[string]int) Option {
	return func(c *Config) {
		c.LevelMap = m
	}
}

// WithMessageKeys sets the keys to look for the log message.
func WithMessageKeys(keys ...string) Option {
	return func(c *Config) {
		c.MessageKeys = keys
	}
}

// WithTimestampKeys sets the keys to look for the timestamp.
func WithTimestampKeys(keys ...string) Option {
	return func(c *Config) {
		c.TimestampKeys = keys
	}
}

// WithSourceKeys sets the keys to look for the source location.
func WithSourceKeys(keys ...string) Option {
	return func(c *Config) {
		c.SourceKeys = keys
	}
}

// WithWorkingDir sets the working directory for relative source paths.
func WithWorkingDir(dir string) Option {
	return func(c *Config) {
		c.WorkingDir = dir
	}
}

// WithOutput sets the output writer.
func WithOutput(w io.Writer) Option {
	return func(c *Config) {
		c.Output = w
	}
}

// WithNoColor disables colorized output.
func WithNoColor(noColor bool) Option {
	return func(c *Config) {
		c.NoColor = noColor
	}
}

// WithHideTimestamp disables timestamp rendering.
func WithHideTimestamp(hide bool) Option {
	return func(c *Config) {
		c.HideTimestamp = hide
	}
}

// WithTheme sets the color theme.
// Any zero-value fields in the provided theme are filled from the default theme.
func WithTheme(theme Theme) Option {
	return func(c *Config) {
		c.Theme = theme.mergeWithDefault()
	}
}

// WithAddSource enables adding source location to log output.
func WithAddSource(addSource bool) Option {
	return func(c *Config) {
		c.AddSource = addSource
	}
}

// WithLevel sets the minimum level to display.
// Logs below this level will be suppressed.
func WithLevel(level int) Option {
	return func(c *Config) {
		c.Level = level
	}
}

// WithOmitKeys sets field name patterns to omit from output.
// Supports glob patterns (e.g., "request.*", "*_id", "secret*").
func WithOmitKeys(patterns ...string) Option {
	return func(c *Config) {
		c.OmitKeys = patterns
		c.omitMatcher = NewOmitMatcher(patterns)
	}
}

// ShouldOmit returns true if the field name should be omitted from output.
func (c *Config) ShouldOmit(field string) bool {
	if c.omitMatcher == nil {
		return false
	}
	return c.omitMatcher.Match(field)
}
