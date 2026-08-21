package platformmcp

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// StaleWatermarkThreshold is how far behind the observation watermark may fall
// before a diagnostic reports itself stale. Telemetry reaches the read model
// through batched ingestion, so a small lag is normal; beyond this the answer
// is old enough that a caller acting on it could be reasoning about a system
// state that has already changed.
const StaleWatermarkThreshold = 5 * time.Minute

// Freshness qualifies every diagnostic result. It is deliberately reported
// beside the data rather than folded into it: an empty result is
// FreshnessNoObservations, which is the absence of evidence and never evidence
// of health.
type Freshness string

const (
	// FreshnessCurrent means the watermark is within StaleWatermarkThreshold of
	// the read, or already past the end of the window.
	FreshnessCurrent Freshness = "current"
	FreshnessStale   Freshness = "stale"
	// FreshnessUnavailable means the scope holds no observations at all. It is
	// paired with DataEnvelope.NoObservations, which says the same thing
	// positively: a caller must not read either as "nothing went wrong".
	FreshnessUnavailable Freshness = "unavailable"
)

// DiagnosticWindow is the closed set of windows a diagnostic may be asked for.
// Callers name a window rather than supplying timestamps: an open time grammar
// is a query language, and this surface deliberately has none.
//
// Each tool additionally caps how far back it will look, because the cost of a
// read is not the same for a summary and for a row-level drill-down.
type DiagnosticWindow string

const (
	DiagnosticWindowLastHour  DiagnosticWindow = "1h"
	DiagnosticWindowLastDay   DiagnosticWindow = "24h"
	DiagnosticWindowLastWeek  DiagnosticWindow = "7d"
	DiagnosticWindowLastMonth DiagnosticWindow = "30d"
)

// DefaultDiagnosticWindow is what an unspecified window resolves to when a tool
// states no narrower default.
const DefaultDiagnosticWindow = DiagnosticWindowLastDay

// windowSpec is one tool's window policy: what an unspecified window means and
// how far back the tool will look at all.
type windowSpec struct {
	Fallback DiagnosticWindow
	Max      DiagnosticWindow
}

// Per-tool window policies. A summary may look back a month; a row-level
// drill-down may not.
var (
	overviewWindowSpec    = windowSpec{Fallback: DiagnosticWindowLastDay, Max: DiagnosticWindowLastMonth}
	diagnosticsWindowSpec = windowSpec{Fallback: DiagnosticWindowLastHour, Max: DiagnosticWindowLastDay}
	drilldownWindowSpec   = windowSpec{Fallback: DiagnosticWindowLastDay, Max: DiagnosticWindowLastDay}
	metricsWindowSpec     = windowSpec{Fallback: DiagnosticWindowLastDay, Max: DiagnosticWindowLastWeek}
)

// ErrDiagnosticWindowInvalid is returned for a window outside the closed set.
var ErrDiagnosticWindowInvalid = fmt.Errorf("window must be one of %s, %s, %s, %s",
	DiagnosticWindowLastHour, DiagnosticWindowLastDay, DiagnosticWindowLastWeek, DiagnosticWindowLastMonth)

// ErrDiagnosticWindowTooLong is returned for a window this tool will not look
// back over. It is refused rather than clamped for the same reason an unknown
// window is: a caller must never be told about a different interval than the
// one it asked for.
var ErrDiagnosticWindowTooLong = errors.New("window is longer than this tool allows")

func (w DiagnosticWindow) duration() (time.Duration, bool) {
	switch w {
	case DiagnosticWindowLastHour:
		return time.Hour, true
	case DiagnosticWindowLastDay:
		return 24 * time.Hour, true
	case DiagnosticWindowLastWeek:
		return 7 * 24 * time.Hour, true
	case DiagnosticWindowLastMonth:
		return 30 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

// ResolvedWindow is the window a diagnostic actually read, echoed on every
// result. A caller never has to infer it from what it asked for.
type ResolvedWindow struct {
	Window DiagnosticWindow `json:"window"`
	From   string           `json:"from"`
	To     string           `json:"to"`

	start time.Time
	end   time.Time
}

// resolveWindow turns a requested window name into the interval to read.
// An empty request resolves to DefaultDiagnosticWindow; anything outside the
// closed set is refused rather than silently clamped, so a caller never
// receives a different window than it believes it asked for.
func resolveWindow(requested string, now time.Time, spec windowSpec) (ResolvedWindow, error) {
	window := DiagnosticWindow(strings.ToLower(strings.TrimSpace(requested)))
	if window == "" {
		window = spec.Fallback
		if window == "" {
			window = DefaultDiagnosticWindow
		}
	}
	duration, ok := window.duration()
	if !ok {
		return ResolvedWindow{}, ErrDiagnosticWindowInvalid
	}
	if maxDuration, ok := spec.Max.duration(); ok && duration > maxDuration {
		return ResolvedWindow{}, fmt.Errorf("%w: %s allows at most %s", ErrDiagnosticWindowTooLong, window, spec.Max)
	}
	// Truncated to the second the window is advertised at, so the interval a
	// caller is told about is exactly the interval that was queried. Formatting
	// a sub-second `now` as RFC3339 would round the boundary away and quietly
	// disagree with the nanosecond bounds the reads use.
	end := now.UTC().Truncate(time.Second)
	start := end.Add(-duration)
	return ResolvedWindow{
		Window: window,
		From:   start.Format(time.RFC3339),
		To:     end.Format(time.RFC3339),
		start:  start,
		end:    end,
	}, nil
}

// DataEnvelope accompanies every diagnostic result. It says when the answer was
// computed, how far the underlying observations reach, and whether that is
// current enough to act on.
type DataEnvelope struct {
	QueriedAt   string    `json:"queried_at"`
	DataThrough string    `json:"data_through,omitempty"`
	Freshness   Freshness `json:"freshness"`
	// NoObservations states positively that the scope produced nothing in the
	// window. Freshness alone cannot carry this: "unavailable" describes the
	// pipeline, and a caller must not read either as evidence of health.
	NoObservations bool           `json:"no_observations"`
	ResolvedWindow ResolvedWindow `json:"resolved_window"`
}

// newDataEnvelope classifies a read against its observation watermark.
//
// A watermark at or past the end of the window means the window is fully
// covered, so a historical read stays fresh instead of being reported stale
// merely for being about the past. Otherwise the lag is measured against the
// moment of the read.
func newDataEnvelope(now time.Time, watermark time.Time, window ResolvedWindow) DataEnvelope {
	envelope := DataEnvelope{
		QueriedAt:      now.UTC().Format(time.RFC3339),
		DataThrough:    "",
		Freshness:      FreshnessUnavailable,
		NoObservations: true,
		ResolvedWindow: window,
	}
	if watermark.IsZero() {
		return envelope
	}
	envelope.NoObservations = false
	envelope.DataThrough = watermark.UTC().Format(time.RFC3339)
	switch {
	case !watermark.Before(window.end):
		envelope.Freshness = FreshnessCurrent
	case now.Sub(watermark) > StaleWatermarkThreshold:
		envelope.Freshness = FreshnessStale
	default:
		envelope.Freshness = FreshnessCurrent
	}
	return envelope
}

// watermarkTime converts a ClickHouse nanosecond watermark, where zero means
// "no observations", into a time. It does not treat the Unix epoch as data.
func watermarkTime(unixNano int64) time.Time {
	if unixNano <= 0 {
		return time.Time{}
	}
	return time.Unix(0, unixNano).UTC()
}
