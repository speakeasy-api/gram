package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/wire"
)

// BinaryVersion is stamped by the main package with the release version so
// device telemetry can attribute behavior to the exact binary build.
var BinaryVersion = "dev"

// processStart anchors the device-side elapsed time reported on each request:
// one hook invocation is one process, so time since start is the binary's own
// overhead (config, auth, envelope build, and any earlier sends) before the
// request left the machine. Spool drains share this transport; their requests
// are marked replayed on the server, and there elapsed measures time into the
// drain run instead.
var processStart = time.Now()

// deviceTraceParent mints the W3C trace context once per process — one trace
// per hook invocation, or per drain run when replaying the spool. The ingest
// endpoint's route prefix is trusted for inbound trace context, so the
// server's spans — including SDK retries and the shared org-key replay —
// parent under this device-begun trace and share one trace id end to end. An
// empty string (randomness unavailable) skips the header.
var deviceTraceParent = sync.OnceValue(func() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return "00-" + hex.EncodeToString(b[:16]) + "-" + hex.EncodeToString(b[16:24]) + "-01"
})

// harnessInfo carries the per-event coding-agent identity from deliver to the
// transport, which cannot see the payload.
type harnessInfo struct {
	name    string
	variant string
	version string
}

type harnessInfoKey struct{}

func withHarnessInfo(ctx context.Context, base *agenthooks.Event) context.Context {
	version := ""
	// Cursor is the only provider that exposes its version to hook processes.
	if base.Provider == agenthooks.ProviderCursor {
		version = os.Getenv("CURSOR_VERSION")
	}
	return context.WithValue(ctx, harnessInfoKey{}, harnessInfo{
		name:    adapterSlug(base.Provider),
		variant: string(base.Variant),
		version: version,
	})
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
	if tp := deviceTraceParent(); tp != "" && req.Header.Get("traceparent") == "" {
		req.Header.Set("traceparent", tp)
	}
	req.Header.Set(wire.HeaderDeviceOS, runtime.GOOS)
	req.Header.Set(wire.HeaderDeviceArch, runtime.GOARCH)
	req.Header.Set(wire.HeaderDeviceBinaryVersion, BinaryVersion)
	req.Header.Set(wire.HeaderDeviceElapsedMS, strconv.FormatInt(time.Since(processStart).Milliseconds(), 10))
	if hi, ok := req.Context().Value(harnessInfoKey{}).(harnessInfo); ok {
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
