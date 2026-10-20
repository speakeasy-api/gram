package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/hooks/wire"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

// maxDeviceHeaderLen caps a device-reported header value. Real values (an OS,
// a semver, a harness name) are a few characters, and the speakeasy-hooks
// binary truncates to the same cap before sending.
const maxDeviceHeaderLen = 64

// hookDeviceHeaderAttrs maps the X-Gram-Device-* headers the speakeasy-hooks
// binary stamps on its requests onto attribute keys. The elapsed-ms header is
// handled separately as an integer span attribute.
var hookDeviceHeaderAttrs = map[string]attribute.Key{
	wire.HeaderDeviceOS:             attr.HookDeviceOSKey,
	wire.HeaderDeviceArch:           attr.HookDeviceArchKey,
	wire.HeaderDeviceBinaryVersion:  attr.HookDeviceBinaryVersionKey,
	wire.HeaderDeviceHarness:        attr.HookDeviceHarnessKey,
	wire.HeaderDeviceHarnessVariant: attr.HookDeviceHarnessVariantKey,
	wire.HeaderDeviceHarnessVersion: attr.HookDeviceHarnessVersionKey,
}

type hookDeviceContextKey struct{}

// HookDeviceAttributes returns the machine details (OS, arch, binary build,
// harness) the speakeasy-hooks binary reported on the current hook request,
// keyed by their gram.hook.device.* attribute and already sanitized by
// HookDeviceTelemetry. It is nil when the request reported none: the legacy
// curl client and in-process callers send no X-Gram-Device-* headers. The map
// is shared with the request context, so callers must not modify it.
func HookDeviceAttributes(ctx context.Context) map[attr.Key]string {
	device, _ := ctx.Value(hookDeviceContextKey{}).(map[attr.Key]string)
	return device
}

// HookDeviceTelemetry lifts the X-Gram-Device-* headers stamped by the
// speakeasy-hooks binary onto the hook endpoint's server span, so traces the
// device began carry the machine details (OS, arch, binary build, harness)
// and the on-device elapsed time needed to measure hook performance end to
// end. The string details also ride on the request context for the hook
// telemetry rows (HookDeviceAttributes), whether or not the span is sampled.
// Must be registered after otelhttp so the span is in the request context.
// Header values are device-supplied input: they are bounded and sanitized
// before becoming attributes, and non-hook routes are untouched.
func HookDeviceTelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/rpc/hooks.") {
			next.ServeHTTP(w, r)
			return
		}

		// Allocated on the first reported value, so header-less senders
		// allocate nothing.
		var device map[attr.Key]string
		for header, key := range hookDeviceHeaderAttrs {
			v := sanitizeDeviceHeader(r.Header.Get(header))
			if v == "" {
				continue
			}
			if device == nil {
				device = make(map[attr.Key]string, len(hookDeviceHeaderAttrs))
			}
			device[key] = v
		}
		if device != nil {
			r = r.WithContext(context.WithValue(r.Context(), hookDeviceContextKey{}, device))
		}

		span := trace.SpanFromContext(r.Context())
		if span.IsRecording() {
			attrs := make([]attribute.KeyValue, 0, len(device)+1)
			for key, v := range device {
				attrs = append(attrs, key.String(v))
			}
			if v := r.Header.Get(wire.HeaderDeviceElapsedMS); v != "" {
				// A day bounds any plausible producer (hook processes live
				// seconds, drain runs minutes), so absurd values from a
				// hostile or broken sender are dropped, not recorded.
				if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms >= 0 && ms <= 24*60*60*1000 {
					attrs = append(attrs, attr.HookDeviceElapsedMsKey.Int64(ms))
				}
			}
			if len(attrs) > 0 {
				span.SetAttributes(attrs...)
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sanitizeDeviceHeader bounds an untrusted device-reported header value before
// it becomes an attribute: trimmed, capped in length, and rejected outright
// if it carries anything beyond printable ASCII. The cap is applied in bytes
// before any decoding, so an oversized header costs no allocation; for the
// printable-ASCII values that survive, bytes and characters are the same.
func sanitizeDeviceHeader(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > maxDeviceHeaderLen {
		v = v[:maxDeviceHeaderLen]
	}
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return v
}
