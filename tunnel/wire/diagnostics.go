package wire

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	HeaderCapabilities    = "X-Gram-Tunnel-Agent-Capabilities"
	HeaderControlToken    = "X-Gram-Tunnel-Control-Token"
	HeaderTargetDisplay   = "X-Gram-Tunnel-Target-Display"
	DiagnosticsCapability = "diagnostics.v1"
	ControlStatusPath     = ControlPathPrefix + "status"
	MaxDiagnosticsBytes   = 8 << 10
	MaxTargetDisplayBytes = 2 << 10
	DiagnosticsInterval   = 30 * time.Second
	DiagnosticsFreshness  = 90 * time.Second
)

// DiagnosticStep contains only bounded categories and timing, never resolver or TLS error text.
type DiagnosticStep struct {
	State          string `json:"state"`
	DurationMillis int64  `json:"duration_ms"`
	Failure        string `json:"failure"`
}

// HTTPProgress contains aggregate gauges, with no per-request identities or data.
type HTTPProgress struct {
	// WaitingHeaders counts requests that have not received final response headers.
	WaitingHeaders uint64 `json:"waiting_headers"`
	// OpenResponses counts response bodies not yet ended or closed, including SSE.
	OpenResponses uint64 `json:"open_responses"`
}

// DiagnosticsReport is the allowlisted diagnostics.v1 wire contract. Ages are
// relative to serialization on the agent, avoiding dependence on customer clocks.
type DiagnosticsReport struct {
	// Older agents omit HTTPProgress; absence means unavailable.
	HTTPProgress                *HTTPProgress  `json:"http_progress,omitempty"`
	Version                     int            `json:"version"`
	Sequence                    uint64         `json:"sequence"`
	SampleAgeMillis             int64          `json:"sample_age_ms"`
	TargetState                 string         `json:"target_state"`
	ConsecutiveFailures         uint32         `json:"consecutive_failures"`
	DNS                         DiagnosticStep `json:"dns"`
	TCP                         DiagnosticStep `json:"tcp"`
	TLS                         DiagnosticStep `json:"tls"`
	RequestsTotal               uint64         `json:"requests_total"`
	TransportErrorsTotal        uint64         `json:"transport_errors_total"`
	LastHTTPStatus              int            `json:"last_http_status"`
	LastHTTPResponseAgeMillis   int64          `json:"last_http_response_age_ms"`
	LastTransportError          string         `json:"last_transport_error"`
	LastTransportErrorAgeMillis int64          `json:"last_transport_error_age_ms"`
}

// Validate checks numeric bounds and allowed metric categories.
func (r DiagnosticsReport) Validate() error {
	if r.Version != 1 || !oneOf(r.TargetState, "pending", "reachable", "unreachable", "unknown") || !validAge(r.SampleAgeMillis) || !validAge(r.LastHTTPResponseAgeMillis) || !validAge(r.LastTransportErrorAgeMillis) {
		return errors.New("invalid diagnostic report")
	}
	if r.LastHTTPStatus != 0 && (r.LastHTTPStatus < 100 || r.LastHTTPStatus > 599) {
		return errors.New("invalid diagnostic HTTP status")
	}
	if !validFailure(r.LastTransportError) {
		return errors.New("invalid transport failure category")
	}
	for _, step := range []DiagnosticStep{r.DNS, r.TCP, r.TLS} {
		if !oneOf(step.State, "pass", "fail", "not_applicable", "not_tested") || step.DurationMillis < 0 || step.DurationMillis > 5000 || !validFailure(step.Failure) {
			return errors.New("invalid diagnostic step")
		}
	}
	return nil
}

func validAge(age int64) bool { return age >= -1 && age <= int64((24*time.Hour)/time.Millisecond) }

func validFailure(value string) bool {
	return oneOf(value, "", "dns_not_found", "dns_timeout", "dns_error", "tcp_refused", "tcp_timeout", "tls_expired", "tls_untrusted", "tls_name_mismatch", "tls_error", "proxy", "unknown")
}

func oneOf(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}

// SupportsDiagnostics ignores malformed optional headers without rejecting the tunnel.
func SupportsDiagnostics(capabilities, token string) bool {
	if len(capabilities) > 256 || len(token) != 64 {
		return false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return false
	}
	for capability := range strings.SplitSeq(capabilities, ",") {
		if strings.TrimSpace(capability) == DiagnosticsCapability {
			return true
		}
	}
	return false
}

// TargetDisplay rebuilds an address without userinfo, query or fragment. The
// hostname, port and escaped path are permitted configuration, not metric labels.
func TargetDisplay(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return ""
	}
	display := (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}).String()
	if len(display) > MaxTargetDisplayBytes || strings.ContainsAny(display, "\r\n") {
		return ""
	}
	return display
}

// DecodeDiagnostics requires the v1 fields and discards unknown fields.
func DecodeDiagnostics(data []byte) (*DiagnosticsReport, error) {
	required := []string{"version", "sequence", "sample_age_ms", "target_state", "consecutive_failures", "dns", "tcp", "tls", "requests_total", "transport_errors_total", "last_http_status", "last_http_response_age_ms", "last_transport_error", "last_transport_error_age_ms"}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, errors.New("invalid diagnostic report")
	}
	present := func(values map[string]json.RawMessage, keys ...string) bool {
		for _, key := range keys {
			if len(values[key]) == 0 || string(values[key]) == "null" {
				return false
			}
		}
		return true
	}
	if !present(fields, required...) {
		return nil, errors.New("incomplete diagnostic report")
	}
	for _, name := range []string{"dns", "tcp", "tls", "http_progress"} {
		raw, exists := fields[name]
		if name == "http_progress" && !exists {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) != nil {
			return nil, errors.New("invalid diagnostic step")
		}
		keys := []string{"state", "duration_ms", "failure"}
		if name == "http_progress" {
			keys = []string{"waiting_headers", "open_responses"}
		}
		if !present(nested, keys...) {
			return nil, errors.New("incomplete diagnostic step")
		}
	}
	var report DiagnosticsReport
	if json.Unmarshal(data, &report) != nil {
		return nil, errors.New("invalid diagnostic report")
	}
	if err := report.Validate(); err != nil {
		return nil, err
	}
	return &report, nil
}

// GatewayDisplay sanitizes WebSocket configuration before writing it to logs.
func GatewayDisplay(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Hostname() == "" {
		return ""
	}
	scheme := u.Scheme
	u.Scheme = "http"
	display := TargetDisplay(u.String())
	if display == "" {
		return ""
	}
	return scheme + strings.TrimPrefix(display, "http")
}
