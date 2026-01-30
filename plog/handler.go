package plog

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Handler implements slog.Handler for pretty-printed output.
type Handler struct {
	cfg       *Config
	formatter *Formatter
	mu        *sync.Mutex // shared across clones for atomic writes
	groups    []string
	attrs     []Attr
}

// NewHandler creates a new slog Handler with the given options.
func NewHandler(opts ...Option) *Handler {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return &Handler{
		cfg:       cfg,
		formatter: NewFormatter(cfg),
		mu:        &sync.Mutex{},
		groups:    nil,
		attrs:     nil,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return slogLevelToLevel(level) >= h.cfg.Level
}

// Handle formats and writes the log record.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	record := &Record{
		Level:    slogLevelToLevel(r.Level),
		LevelRaw: r.Level.String(),
		Message:  r.Message,
		Time:     r.Time,
		Attrs:    nil,
		Source:   nil,
	}

	// Add source if enabled and available
	if h.cfg.AddSource && r.PC != 0 {
		fs := r.Source()
		if fs != nil {
			record.Source = &Source{
				File:     fs.File,
				Line:     fs.Line,
				Function: fs.Function,
			}
		}
	}

	// Collect attributes, filtering omitted keys
	var attrs []Attr

	// Add pre-defined attrs from WithAttrs
	for _, attr := range h.attrs {
		if !h.cfg.ShouldOmit(attr.Key) {
			attrs = append(attrs, attr)
		}
	}

	// Add attrs from the record
	r.Attrs(func(a slog.Attr) bool {
		attr := slogAttrToAttr(h.groups, a)
		if !h.cfg.ShouldOmit(attr.Key) {
			attrs = append(attrs, attr)
		}
		return true
	})

	record.Attrs = attrs

	// Write with mutex for atomic output
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.formatter.Format(h.cfg.Output, record)
}

// WithAttrs returns a new Handler with the given attributes added.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]Attr, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	for _, a := range attrs {
		newAttrs = append(newAttrs, slogAttrToAttr(h.groups, a))
	}
	return &Handler{
		cfg:       h.cfg,
		formatter: h.formatter,
		mu:        h.mu, // share mutex
		groups:    h.groups,
		attrs:     newAttrs,
	}
}

// WithGroup returns a new Handler with the given group name added.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name
	return &Handler{
		cfg:       h.cfg,
		formatter: h.formatter,
		mu:        h.mu, // share mutex
		groups:    newGroups,
		attrs:     h.attrs,
	}
}

// slogLevelToLevel converts slog.Level to our level values.
func slogLevelToLevel(level slog.Level) int {
	switch {
	case level < slog.LevelDebug:
		return LevelTrace
	case level < slog.LevelInfo:
		return LevelDebug
	case level < slog.LevelWarn:
		return LevelInfo
	case level < slog.LevelError:
		return LevelWarn
	default:
		return LevelError
	}
}

// slogAttrToAttr converts a slog.Attr to our Attr type.
func slogAttrToAttr(groups []string, a slog.Attr) Attr {
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	return Attr{Key: key, Value: slogValueToAny(a.Value)}
}

// slogValueToAny converts a slog.Value to any.
func slogValueToAny(v slog.Value) any {
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindTime:
		return v.Time().Format(time.RFC3339Nano)
	case slog.KindGroup:
		attrs := v.Group()
		m := make(map[string]any, len(attrs))
		for _, a := range attrs {
			m[a.Key] = slogValueToAny(a.Value)
		}
		return m
	case slog.KindAny:
		return v.Any()
	default:
		return v.Any()
	}
}

// DefaultLogger creates a new slog.Logger with the pretty handler.
func DefaultLogger(opts ...Option) *slog.Logger {
	return slog.New(NewHandler(opts...))
}

// SetDefault sets the default slog logger to use the pretty handler.
func SetDefault(opts ...Option) {
	slog.SetDefault(DefaultLogger(opts...))
}

// NewLogger creates a new slog.Logger that writes to the given writer.
func NewLogger(w io.Writer, opts ...Option) *slog.Logger {
	allOpts := append([]Option{WithOutput(w)}, opts...)
	return slog.New(NewHandler(allOpts...))
}

// NewConsoleLogger creates a logger that writes to stdout.
func NewConsoleLogger(opts ...Option) *slog.Logger {
	return NewLogger(os.Stdout, opts...)
}
