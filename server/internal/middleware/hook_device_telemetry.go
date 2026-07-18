package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/hooks/wire"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// hookDeviceHeaderAttrs maps the X-Gram-Device-* headers the speakeasy-hooks
// binary stamps on its requests onto span attribute keys. The elapsed-ms
// header is handled separately as an integer attribute.
var hookDeviceHeaderAttrs = map[string]attribute.Key{
	wire.HeaderDeviceOS:             attr.HookDeviceOSKey,
	wire.HeaderDeviceArch:           attr.HookDeviceArchKey,
	wire.HeaderDeviceBinaryVersion:  attr.HookDeviceBinaryVersionKey,
	wire.HeaderDeviceHarness:        attr.HookDeviceHarnessKey,
	wire.HeaderDeviceHarnessVariant: attr.HookDeviceHarnessVariantKey,
	wire.HeaderDeviceHarnessVersion: attr.HookDeviceHarnessVersionKey,
}

// HookDeviceTelemetry lifts the X-Gram-Device-* headers stamped by the
// speakeasy-hooks binary onto the hook endpoint's server span, so traces the
// device began carry the machine details (OS, arch, binary build, harness)
// and the on-device elapsed time needed to measure hook performance end to
// end. Must be registered after otelhttp so the span is in the request
// context. Header values are device-supplied input: they are bounded and
// sanitized before becoming attributes, and non-hook routes are untouched.
func HookDeviceTelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/rpc/hooks.") {
			span := trace.SpanFromContext(r.Context())
			if span.IsRecording() {
				attrs := make([]attribute.KeyValue, 0, len(hookDeviceHeaderAttrs)+1)
				for header, key := range hookDeviceHeaderAttrs {
					if v := sanitizeDeviceHeader(r.Header.Get(header)); v != "" {
						attrs = append(attrs, key.String(v))
					}
				}
				if v := r.Header.Get(wire.HeaderDeviceElapsedMS); v != "" {
					if ms, err := strconv.ParseInt(v, 10, 64); err == nil && ms >= 0 {
						attrs = append(attrs, attr.HookDeviceElapsedMsKey.Int64(ms))
					}
				}
				if len(attrs) > 0 {
					span.SetAttributes(attrs...)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// sanitizeDeviceHeader bounds an untrusted device-reported header value before
// it becomes a span attribute: trimmed, capped in length, and rejected outright
// if it carries anything beyond printable ASCII.
func sanitizeDeviceHeader(v string) string {
	v = conv.TruncateString(strings.TrimSpace(v), 64)
	for _, r := range v {
		if r < 0x20 || r > 0x7e {
			return ""
		}
	}
	return v
}
