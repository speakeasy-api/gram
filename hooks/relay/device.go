package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/wire"
)

// BinaryVersion is stamped by the main package with the release version so
// device telemetry can attribute behavior to the exact binary build.
var BinaryVersion = "dev"

// processStart is the fallback anchor for the device-side elapsed time on
// requests outside the per-event deliver path (spool drains, skill uploads):
// there elapsed measures time into the run. Live sends anchor on the event's
// own receive time instead (see harnessInfo.start) — in the long-lived hook
// server, process age says nothing about one event's overhead.
var processStart = time.Now()

// deviceTraceParent mints the fallback W3C trace context, once per process —
// one trace per drain run when replaying the spool. Live sends carry a
// per-event trace context minted in withHarnessInfo instead: the hook server
// handles many sessions' events in one process, and a process-scoped trace
// id would collapse them all into one server-side trace. An empty string
// (randomness unavailable) skips the header.
var deviceTraceParent = sync.OnceValue(mintTraceParent)

// mintTraceParent builds a sampled W3C traceparent with random trace and
// span ids, or "" when randomness is unavailable. The ingest endpoint's
// route prefix is trusted for inbound trace context, so the server's spans —
// including SDK retries and the shared org-key replay, which reuse the same
// context — parent under this device-begun trace and share one trace id end
// to end.
func mintTraceParent() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return "00-" + hex.EncodeToString(b[:16]) + "-" + hex.EncodeToString(b[16:24]) + "-01"
}

// harnessInfo carries the per-event coding-agent identity and trace anchors
// from deliver to the transport, which cannot see the payload.
type harnessInfo struct {
	name    string
	variant string
	version string
	// traceparent is this event's W3C trace context. Minted per event so
	// every hook invocation gets its own trace even when the hook server
	// processes many of them in one process; the SDK retries and the shared
	// org-key replay reuse the same context and stay under the one trace.
	traceparent string
	// start anchors the device-side elapsed time to this event's receipt,
	// measuring the binary's per-event overhead (auth, envelope build, any
	// earlier sends) rather than the age of a long-lived server process.
	start time.Time
}

type harnessInfoKey struct{}

func withHarnessInfo(ctx context.Context, base *agenthooks.Event) context.Context {
	version := ""
	// Cursor is the only provider that exposes its version to hook processes.
	if base.Provider == agenthooks.ProviderCursor {
		version = sanitizeHeaderValue(os.Getenv("CURSOR_VERSION"))
	}
	start := base.Time
	if start.IsZero() {
		start = time.Now()
	}
	return context.WithValue(ctx, harnessInfoKey{}, harnessInfo{
		name:        adapterSlug(base.Provider),
		variant:     string(base.Variant),
		version:     version,
		traceparent: mintTraceParent(),
		start:       start,
	})
}

// sanitizeHeaderValue bounds an environment-supplied value before it becomes
// an HTTP header: net/http rejects invalid header bytes at send time, which
// would turn every ingest attempt into a transport failure and could fail a
// gating hook closed over a cosmetic value. Truncated to the server's 64-char
// attribute cap; anything beyond printable ASCII drops the value entirely.
func sanitizeHeaderValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 64 {
		v = v[:64]
	}
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return v
}

// deviceTransport stamps every request with the on-device trace context and
// the X-Gram-Device-* telemetry headers the server lifts onto its spans:
// enough machine detail (OS, arch, binary build, harness) to diagnose hook
// issues per platform, and the device-side elapsed time to measure the
// binary's own overhead end to end.
type deviceTransport struct {
	base http.RoundTripper
}

func (t *deviceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	hi, hasEvent := req.Context().Value(harnessInfoKey{}).(harnessInfo)
	traceParent := ""
	if hasEvent {
		traceParent = hi.traceparent
	}
	if traceParent == "" {
		traceParent = deviceTraceParent()
	}
	if traceParent != "" && req.Header.Get("traceparent") == "" {
		req.Header.Set("traceparent", traceParent)
	}
	elapsedFrom := processStart
	if hasEvent && !hi.start.IsZero() {
		elapsedFrom = hi.start
	}
	req.Header.Set(wire.HeaderDeviceOS, runtime.GOOS)
	req.Header.Set(wire.HeaderDeviceArch, runtime.GOARCH)
	req.Header.Set(wire.HeaderDeviceBinaryVersion, BinaryVersion)
	req.Header.Set(wire.HeaderDeviceElapsedMS, strconv.FormatInt(time.Since(elapsedFrom).Milliseconds(), 10))
	if hasEvent {
		if hi.name != "" {
			req.Header.Set(wire.HeaderDeviceHarness, hi.name)
		}
		if hi.variant != "" {
			req.Header.Set(wire.HeaderDeviceHarnessVariant, hi.variant)
		}
		if hi.version != "" {
			req.Header.Set(wire.HeaderDeviceHarnessVersion, hi.version)
		}
	}
	return t.base.RoundTrip(req)
}
