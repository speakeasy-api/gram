package platformmcp

import (
	"errors"
	"fmt"
	"slices"
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

// ErrDiagnosticWindowInvalid is returned for a window outside a tool's closed
// set. The set differs per tool, so the message names the windows that tool
// accepts rather than every window the type can express.
var ErrDiagnosticWindowInvalid = errors.New("unsupported window")

// windowPolicy is one tool's closed set of windows and the one an unspecified
// request resolves to. Each tool carries its own: a window that answers "what
// has this project been doing" is not the window that answers "why is this MCP
// failing right now", and a diagnosis read over weeks averages a live fault
// into healthy traffic until the attribution can no longer see it.
type windowPolicy struct {
	allowed  []DiagnosticWindow
	fallback DiagnosticWindow
}

// overviewWindowPolicy bounds get_project_overview: a project summary is a
// trend question, so it reaches back a month.
var overviewWindowPolicy = windowPolicy{
	allowed: []DiagnosticWindow{
		DiagnosticWindowLastHour,
		DiagnosticWindowLastDay,
		DiagnosticWindowLastWeek,
		DiagnosticWindowLastMonth,
	},
	fallback: DiagnosticWindowLastDay,
}

// diagnosticsWindowPolicy bounds get_mcp_diagnostics. It is deliberately
// tighter than the overview's. Fault attribution reasons in ratios — a
// dominant failure class, and this server's failure rate against the rest of
// the organization's — and readiness can only exonerate while it is fresh, so
// a long window pairs a minutes-old probe with weeks of outcomes and dilutes a
// current fault below every threshold that would have caught it.
var diagnosticsWindowPolicy = windowPolicy{
	allowed:  []DiagnosticWindow{DiagnosticWindowLastHour, DiagnosticWindowLastDay},
	fallback: DiagnosticWindowLastHour,
}

func (p windowPolicy) permits(window DiagnosticWindow) bool {
	return slices.Contains(p.allowed, window)
}

// invalid names the windows this policy accepts, so a refused caller learns
// what to ask for instead of only that it asked wrongly.
func (p windowPolicy) invalid() error {
	names := make([]string, 0, len(p.allowed))
	for _, window := range p.allowed {
		names = append(names, string(window))
	}
	return fmt.Errorf("%w: window must be one of %s", ErrDiagnosticWindowInvalid, strings.Join(names, ", "))
}

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

// resolveWindow turns a requested window name into the interval to read, under
// the asking tool's policy. An empty request resolves to that policy's
// fallback; anything outside its closed set is refused rather than silently
// clamped, so a caller never receives a different window than it believes it
// asked for.
func resolveWindow(requested string, now time.Time, policy windowPolicy) (ResolvedWindow, error) {
	window := DiagnosticWindow(strings.ToLower(strings.TrimSpace(requested)))
	if window == "" {
		window = policy.fallback
	}
	duration, ok := window.duration()
	if !ok || !policy.permits(window) {
		return ResolvedWindow{}, policy.invalid()
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
