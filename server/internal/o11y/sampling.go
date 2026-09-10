package o11y

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	slogmulti "github.com/samber/slog-multi"
	slogsampling "github.com/samber/slog-sampling"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// samplingHandler routes only explicitly eligible operational events through
// the sampler. Both branches use the same sink; child handlers share the
// upstream sampling state but retain independent inherited attribute metadata.
type samplingHandler struct {
	next    slog.Handler
	sampled slog.Handler

	// middleware owns the shared sampler state and wraps each derived sink once.
	middleware slogmulti.Middleware

	attrs samplingAttributes
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
	middleware := options.NewMiddleware()
	return &samplingHandler{
		next:       next,
		sampled:    middleware(next),
		middleware: middleware,
		attrs:      samplingAttributes{bucket: "", invalid: false, hasError: false},
	}
}

func (h *samplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *samplingHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level >= slog.LevelWarn {
		if err := h.next.Handle(ctx, record); err != nil {
			return fmt.Errorf("sampling handler: handle protected record: %w", err)
		}
		return nil
	}

	metadata := h.attrs
	record.Attrs(func(a slog.Attr) bool {
		metadata.inspect(a)
		return !metadata.hasError && !metadata.invalid
	})

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
	for _, a := range attrs {
		metadata.inspect(a)
		if metadata.hasError || metadata.invalid {
			return h.next.WithAttrs(attrs)
		}
	}
	next := h.next.WithAttrs(attrs)
	return &samplingHandler{
		next:       next,
		sampled:    h.middleware(next),
		middleware: h.middleware,
		attrs:      metadata,
	}
}

func (h *samplingHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.next.WithGroup(name)
}

// inspect only examines flat attributes. Groups and LogValuer values bypass
// sampling without traversal or resolution: they can hide error attributes.
// Conflicting or malformed markers also retain the event.
func (m *samplingAttributes) inspect(a slog.Attr) {
	kind := a.Value.Kind()
	if kind == slog.KindGroup || kind == slog.KindLogValuer {
		m.invalid = true
		return
	}
	switch a.Key {
	case string(attr.ErrorMessageKey):
		m.hasError = true
	case string(attr.LogsSamplingBucketKey):
		if kind != slog.KindString || a.Value.String() == "" {
			m.invalid = true
		} else if m.bucket != "" && m.bucket != a.Value.String() {
			m.invalid = true
		} else {
			m.bucket = a.Value.String()
		}
	}
}
