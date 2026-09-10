package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	slogsampling "github.com/samber/slog-sampling"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// samplingHandler routes only explicitly eligible operational events through
// the sampler. Both branches use the same sink; child handlers share the
// upstream sampling state but retain independent inherited attribute metadata.
type samplingHandler struct {
	next    slog.Handler
	sampled slog.Handler
	attrs   samplingAttributes
}

type samplingAttributes struct {
	bucket   string
	invalid  bool
	hasError bool
}

var _ slog.Handler = (*samplingHandler)(nil)

func newSamplingHandler(next slog.Handler, rate float64) slog.Handler {
	// Only the registered HTTP success bucket reaches this sampler. MatchAll
	// therefore needs one counter regardless of incoming attribute cardinality.
	// Keep the initial burst, then retain each event with the given probability.
	options := slogsampling.ThresholdSamplingOption{
		Tick:                time.Minute,
		Threshold:           10,
		Rate:                rate,
		Matcher:             slogsampling.MatchAll(),
		Buffer:              nil,
		OnAccepted:          nil,
		OnDropped:           nil,
		IncludeDroppedCount: false,
	}
	return &samplingHandler{
		next:    next,
		sampled: options.NewMiddleware()(next),
		attrs:   samplingAttributes{bucket: "", invalid: false, hasError: false},
	}
}

func (h *samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *samplingHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelWarn || h.attrs.hasError || h.attrs.invalid {
		if err := h.next.Handle(ctx, record); err != nil {
			return fmt.Errorf("sampling handler: handle protected record: %w", err)
		}
		return nil
	}

	metadata := h.attrs
	var resolved []slog.Attr
	index := 0
	record.Attrs(func(a slog.Attr) bool {
		normalized, changed := metadata.inspect(a)
		if changed && resolved == nil {
			// Ordinary scalar attributes need no copy. Freeze LogValuer results
			// only when present, so the sink sees the values used for eligibility.
			resolved = make([]slog.Attr, 0, record.NumAttrs())
			record.Attrs(func(original slog.Attr) bool {
				resolved = append(resolved, original)
				return true
			})
		}
		if resolved != nil {
			resolved[index] = normalized
		}
		index++
		return !metadata.hasError && !metadata.invalid
	})

	if resolved != nil {
		record = slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
		record.AddAttrs(resolved...)
	}
	handler := h.next
	if !metadata.hasError && !metadata.invalid && metadata.bucket == attr.LogsSamplingBucketHTTPResponseSuccess {
		handler = h.sampled
	}
	if err := handler.Handle(ctx, record); err != nil {
		return fmt.Errorf("sampling handler: handle record: %w", err)
	}
	return nil
}

func (h *samplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	metadata := h.attrs
	normalized := attrs
	copied := false
	for i, a := range attrs {
		resolved, changed := metadata.inspect(a)
		if changed && !copied {
			normalized = slices.Clone(attrs)
			copied = true
		}
		if copied {
			normalized[i] = resolved
		}
	}
	return &samplingHandler{
		next:    h.next.WithAttrs(normalized),
		sampled: h.sampled.WithAttrs(normalized),
		attrs:   metadata,
	}
}

func (h *samplingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &samplingHandler{
		next:    h.next.WithGroup(name),
		sampled: h.sampled.WithGroup(name),
		attrs:   h.attrs,
	}
}

// inspect recognizes reserved keys at any group depth. Conflicting or malformed
// markers fail open; an error key always wins, even when its value is empty.
// Resolved values are returned without modifying caller-owned groups.
func (m *samplingAttributes) inspect(a slog.Attr) (slog.Attr, bool) {
	if a.Key == string(attr.ErrorMessageKey) {
		m.hasError = true
		return a, false
	}

	changed := a.Value.Kind() == slog.KindLogValuer
	if changed {
		a.Value = a.Value.Resolve()
	}
	if a.Key == string(attr.LogsSamplingBucketKey) {
		if a.Value.Kind() != slog.KindString || a.Value.String() == "" {
			m.invalid = true
		} else if m.bucket != "" && m.bucket != a.Value.String() {
			m.invalid = true
		} else {
			m.bucket = a.Value.String()
		}
	}

	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		var normalized []slog.Attr
		for i, child := range group {
			resolved, childChanged := m.inspect(child)
			if childChanged && normalized == nil {
				normalized = slices.Clone(group)
			}
			if normalized != nil {
				normalized[i] = resolved
			}
		}
		if normalized != nil {
			a.Value = slog.GroupValue(normalized...)
			changed = true
		}
	}
	return a, changed
}
