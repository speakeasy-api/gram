package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/speakeasy-api/gram/tunnel/wire"
)

// diagnostics keeps bounded, payload-free observations. Probing is leased by
// authenticated gateway polls and never issues synthetic MCP/HTTP requests.
type diagnostics struct {
	mu         sync.Mutex
	report     wire.DiagnosticsReport
	sampledAt  time.Time
	responseAt time.Time
	errorAt    time.Time
	lastPoll   time.Time
	wake       chan struct{}
}

func newDiagnostics() *diagnostics {
	step := wire.DiagnosticStep{State: "not_tested"}
	return &diagnostics{report: wire.DiagnosticsReport{HTTPProgress: &wire.HTTPProgress{}, Version: 1, TargetState: "pending", DNS: step, TCP: step, TLS: step}, wake: make(chan struct{}, 1)}
}

func ageMillis(at, now time.Time) int64 {
	if at.IsZero() {
		return -1
	}
	return min(max(now.Sub(at).Milliseconds(), 0), int64((24*time.Hour)/time.Millisecond))
}

func (d *diagnostics) snapshot() wire.DiagnosticsReport {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	d.lastPoll = now
	select {
	case d.wake <- struct{}{}:
	default:
	}
	report := d.report
	progress := *d.report.HTTPProgress
	report.HTTPProgress = &progress
	report.SampleAgeMillis = ageMillis(d.sampledAt, now)
	report.LastHTTPResponseAgeMillis = ageMillis(d.responseAt, now)
	report.LastTransportErrorAgeMillis = ageMillis(d.errorAt, now)
	return report
}

func (d *diagnostics) run(ctx context.Context, target *url.URL) {
	ticker := time.NewTicker(wire.DiagnosticsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-d.wake:
		}
		d.mu.Lock()
		due := !d.lastPoll.IsZero() && time.Since(d.lastPoll) < time.Minute && (d.sampledAt.IsZero() || time.Since(d.sampledAt) >= wire.DiagnosticsInterval)
		d.mu.Unlock()
		if !due {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		state, dns, tcp, tlsStep := probeTarget(probeCtx, target)
		cancel()
		if ctx.Err() != nil {
			return
		}
		d.mu.Lock()
		d.report.Sequence++
		d.report.TargetState, d.report.DNS, d.report.TCP, d.report.TLS = state, dns, tcp, tlsStep
		if state == "unreachable" {
			if d.report.ConsecutiveFailures < ^uint32(0) {
				d.report.ConsecutiveFailures++
			}
		} else {
			d.report.ConsecutiveFailures = 0
		}
		d.sampledAt = time.Now()
		d.mu.Unlock()
	}
}

type observedTransport struct {
	base        http.RoundTripper
	diagnostics *diagnostics
}

func (t observedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	d := t.diagnostics
	d.mu.Lock()
	d.report.RequestsTotal++
	d.report.HTTPProgress.WaitingHeaders++
	d.mu.Unlock()
	response, err := t.base.RoundTrip(req)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.report.HTTPProgress.WaitingHeaders--
	if err != nil && (req.Context().Err() != nil || errors.Is(err, context.Canceled)) {
		return response, err
	}
	if err != nil {
		d.report.TransportErrorsTotal++
		d.report.LastTransportError = transportFailure(err)
		d.errorAt = time.Now()
	} else {
		d.report.LastHTTPStatus = response.StatusCode
		d.responseAt = time.Now()
		if response.Body != nil && response.Body != http.NoBody {
			d.report.HTTPProgress.OpenResponses++
			body := &observedBody{ReadCloser: response.Body, diagnostics: d}
			// HTTP upgrades require the original body's write interface.
			if writer, ok := response.Body.(io.Writer); ok {
				response.Body = &observedReadWriteBody{observedBody: body, Writer: writer}
			} else {
				response.Body = body
			}
		}
	}
	return response, err
}

// observedBody updates a gauge only when a response ends. Reads are forwarded
// unchanged: no parsing, buffering, byte counting, or per-chunk locking.
type observedBody struct {
	io.ReadCloser
	diagnostics *diagnostics
	once        sync.Once
}

func (b *observedBody) finished() {
	b.once.Do(func() {
		b.diagnostics.mu.Lock()
		b.diagnostics.report.HTTPProgress.OpenResponses--
		b.diagnostics.mu.Unlock()
	})
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.finished()
	}
	return n, err
}

func (b *observedBody) Close() error {
	defer b.finished()
	return b.ReadCloser.Close()
}

type observedReadWriteBody struct {
	*observedBody
	io.Writer
}

func transportFailure(err error) string {
	var dns *net.DNSError
	var invalid x509.CertificateInvalidError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var netErr net.Error
	switch {
	case errors.As(err, &dns):
		if dns.IsNotFound {
			return "dns_not_found"
		}
		if dns.IsTimeout {
			return "dns_timeout"
		}
		return "dns_error"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "tls_expired"
		}
		return "tls_error"
	case errors.As(err, &authority):
		return "tls_untrusted"
	case errors.As(err, &hostname):
		return "tls_name_mismatch"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "tcp_refused"
	case errors.As(err, &netErr) && netErr.Timeout():
		return "tcp_timeout"
	default:
		return "unknown"
	}
}

func probeTarget(ctx context.Context, target *url.URL) (string, wire.DiagnosticStep, wire.DiagnosticStep, wire.DiagnosticStep) {
	dns := wire.DiagnosticStep{State: "not_tested"}
	tcp, tlsStep := dns, dns
	if target.Scheme != "http" && target.Scheme != "https" {
		return "unknown", dns, tcp, tlsStep
	}
	if target.Scheme == "http" {
		tlsStep.State = "not_applicable"
	}
	request := &http.Request{URL: target}
	// Honor the same environment proxy decision as http.DefaultTransport. A
	// direct probe would bypass the configured proxy and report misleading health.
	proxy, err := http.ProxyFromEnvironment(request)
	if err != nil || proxy != nil {
		tcp.Failure = "proxy"
		return "unknown", dns, tcp, tlsStep
	}
	host := target.Hostname()
	if net.ParseIP(host) != nil {
		dns.State = "not_applicable"
	} else {
		start := time.Now()
		_, err = net.DefaultResolver.LookupIPAddr(ctx, host)
		dns.DurationMillis = min(time.Since(start).Milliseconds(), 5000)
		if err != nil {
			dns.State = "fail"
			dns.Failure = transportFailure(err)
			return "unreachable", dns, tcp, tlsStep
		}
		dns.State = "pass"
	}
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	tcp.DurationMillis = min(time.Since(start).Milliseconds(), 5000)
	if err != nil {
		tcp.State = "fail"
		tcp.Failure = transportFailure(err)
		return "unreachable", dns, tcp, tlsStep
	}
	defer func() { _ = conn.Close() }()
	tcp.State = "pass"
	if target.Scheme == "https" {
		start = time.Now()
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
		err = tlsConn.HandshakeContext(ctx)
		tlsStep.DurationMillis = min(time.Since(start).Milliseconds(), 5000)
		if err != nil {
			tlsStep.State = "fail"
			tlsStep.Failure = transportFailure(err)
			return "unreachable", dns, tcp, tlsStep
		}
		tlsStep.State = "pass"
	}
	return "reachable", dns, tcp, tlsStep
}
