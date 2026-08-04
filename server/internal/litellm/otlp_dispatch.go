package litellm

import (
	"mime"
	"net/http"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

const canonicalOTLPMetricsPath = "/rpc/hooks.otel/v1/metrics"

// OTLPMetricsDispatch owns the canonical OTLP metrics route, which is shared
// between harness telemetry (Claude Code/codex, JSON) and LiteLLM exporters.
// LiteLLM traffic is discriminated by provenance rather than payload shape:
// protobuf bodies can only come from an OTLP exporter (harness clients speak
// JSON), and JSON bodies are routed by the authenticated key's litellm- name
// prefix. Everything else falls through to the harness handler unchanged,
// including requests that fail authentication, so the harness path renders
// its own errors.
//
// The service is resolved through a getter because middleware is installed
// before the LiteLLM service can be constructed; requests only flow once the
// server is fully wired.
func OTLPMetricsDispatch(service func() *Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s := service()
			if s == nil || r.Method != http.MethodPost || r.URL.Path != canonicalOTLPMetricsPath {
				next.ServeHTTP(w, r)
				return
			}
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err == nil && (mediaType == "application/x-protobuf" || mediaType == "application/protobuf") {
				s.metricHTTPHandler().ServeHTTP(w, r)
				return
			}
			ctx, err := s.authenticateOTLPRequest(r.Context(), r.Header)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil && strings.HasPrefix(authCtx.APIKeyName, auth.LiteLLMAPIKeyNamePrefix) {
				s.metricHTTPHandler().ServeHTTP(w, r.WithContext(ctx))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
