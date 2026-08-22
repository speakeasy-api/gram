package platformmcp

import (
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
	FreshnessFresh Freshness = "fresh"
	FreshnessStale Freshness = "stale"
	// FreshnessNoObservations means nothing was observed for the scope at all.
	// A caller must not read it as "nothing went wrong".
	FreshnessNoObservations Freshness = "no_observations"
)

// DiagnosticWindow is the closed set of windows a diagnostic may be asked for.
// Callers name a window rather than supplying timestamps: an open time grammar
// is a query language, and this surface deliberately has none.
type DiagnosticWindow string

const (
	DiagnosticWindowLastHour  DiagnosticWindow = "1h"
	DiagnosticWindowLastDay   DiagnosticWindow = "24h"
	DiagnosticWindowLastWeek  DiagnosticWindow = "7d"
	DiagnosticWindowLastMonth DiagnosticWindow = "30d"
)

// DefaultDiagnosticWindow is what an unspecified window resolves to.
const DefaultDiagnosticWindow = DiagnosticWindowLastDay

// ErrDiagnosticWindowInvalid is returned for a window outside the closed set.
var ErrDiagnosticWindowInvalid = fmt.Errorf("window must be one of %s, %s, %s, %s",
	DiagnosticWindowLastHour, DiagnosticWindowLastDay, DiagnosticWindowLastWeek, DiagnosticWindowLastMonth)

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
func resolveWindow(requested string, now time.Time) (ResolvedWindow, error) {
	window := DiagnosticWindow(strings.ToLower(strings.TrimSpace(requested)))
	if window == "" {
		window = DefaultDiagnosticWindow
	}
	duration, ok := window.duration()
	if !ok {
		return ResolvedWindow{}, ErrDiagnosticWindowInvalid
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
	QueriedAt      string         `json:"queried_at"`
	DataThrough    string         `json:"data_through,omitempty"`
	Freshness      Freshness      `json:"freshness"`
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
		Freshness:      FreshnessNoObservations,
		ResolvedWindow: window,
	}
	if watermark.IsZero() {
		return envelope
	}
	envelope.DataThrough = watermark.UTC().Format(time.RFC3339)
	switch {
	case !watermark.Before(window.end):
		envelope.Freshness = FreshnessFresh
	case now.Sub(watermark) > StaleWatermarkThreshold:
		envelope.Freshness = FreshnessStale
	default:
		envelope.Freshness = FreshnessFresh
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
