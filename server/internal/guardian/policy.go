// Package guardian provides HTTP client construction with network security
// policy enforcement, OpenTelemetry instrumentation, resilience (rate
// limiting and circuit breaking), and optional retry logic.
//
// It addresses these concerns:
//
//   - SSRF prevention: every request URL is resolved and checked against a
//     configurable blocklist of CIDR ranges (all RFC-defined private and
//     reserved ranges by default). The selected connection IP is checked again
//     inside [net.Dialer.ControlContext], preventing DNS rebinding between URL
//     validation and connection establishment.
//
//   - Safe HTTP transports: [net/http.DefaultTransport] and
//     [net/http.DefaultClient] are package-level globals that any code can
//     mutate at runtime, making their behaviour unpredictable. Policy.Client
//     and Policy.PooledClient avoid this by constructing fresh, isolated
//     transports for every call via [github.com/hashicorp/go-cleanhttp].
//
//   - Observability: every returned [http.Client] has its transport wrapped
//     with [go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp] so
//     all outbound HTTP calls are traced without per-call-site boilerplate.
//
//   - Resilience: clients built with [WithResilience] rate limit and circuit
//     break requests per partition (upstream host by default) at the
//     transport layer, so every call — including those made inside
//     third-party SDKs holding a *http.Client — is guarded uniformly.
package guardian

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/dns"
)

type HTTPClient = http.Client

var (
	ErrBadHost   = fmt.Errorf("bad host")
	ErrBlockedIP = fmt.Errorf("blocked ip")
)

var defaultBlockedCIDRBlocks = []*net.IPNet{
	// Source: https://www.rfc-editor.org/rfc/rfc5735
	mustParseCIDR("10.0.0.0/8"),         /* Private network - RFC 1918 */
	mustParseCIDR("172.16.0.0/12"),      /* Private network - RFC 1918 */
	mustParseCIDR("192.168.0.0/16"),     /* Private network - RFC 1918 */
	mustParseCIDR("127.0.0.0/8"),        /* Loopback - RFC 1122, Section 3.2.1.3 */
	mustParseCIDR("0.0.0.0/8"),          /* Current network (only valid as source address) - RFC 1122, Section 3.2.1.3 */
	mustParseCIDR("169.254.0.0/16"),     /* Link-local - RFC 3927 */
	mustParseCIDR("192.0.0.0/24"),       /* IETF Protocol Assignments - RFC 5736 */
	mustParseCIDR("192.0.2.0/24"),       /* TEST-NET-1, documentation and examples - RFC 5737 */
	mustParseCIDR("198.51.100.0/24"),    /* TEST-NET-2, documentation and examples - RFC 5737 */
	mustParseCIDR("203.0.113.0/24"),     /* TEST-NET-3, documentation and examples - RFC 5737 */
	mustParseCIDR("192.88.99.0/24"),     /* IPv6 to IPv4 relay (includes 2002::/16) - RFC 3068 */
	mustParseCIDR("198.18.0.0/15"),      /* Network benchmark tests - RFC 2544 */
	mustParseCIDR("224.0.0.0/4"),        /* IP multicast (former Class D network) - RFC 3171 */
	mustParseCIDR("240.0.0.0/4"),        /* Reserved (former Class E network) - RFC 1112, Section 4 */
	mustParseCIDR("255.255.255.255/32"), /* Broadcast - RFC 919, Section 7 */
	mustParseCIDR("100.64.0.0/10"),      /* Shared Address Space - RFC 6598 */

	// Source: https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml
	mustParseCIDR("::/128"),        /* Unspecified Address - RFC 4291 */
	mustParseCIDR("::1/128"),       /* Loopback - RFC 4291 */
	mustParseCIDR("100::/64"),      /* Discard prefix - RFC 6666 */
	mustParseCIDR("2001::/23"),     /* IETF Protocol Assignments - RFC 2928 */
	mustParseCIDR("2001:2::/48"),   /* Benchmarking - RFC5180 */
	mustParseCIDR("2001:db8::/32"), /* Addresses used in documentation and example source code - RFC 3849 */
	mustParseCIDR("2001::/32"),     /* Teredo tunneling - RFC4380 - RFC8190 */
	mustParseCIDR("fc00::/7"),      /* Unique local address - RFC 4193 - RFC 8190 */
	mustParseCIDR("fe80::/10"),     /* Link-local address - RFC 4291 */
	mustParseCIDR("ff00::/8"),      /* Multicast - RFC 3513 */
	mustParseCIDR("2002::/16"),     /* 6to4 - RFC 3056 */
	mustParseCIDR("64:ff9b::/96"),  /* IPv4/IPv6 translation - RFC 6052 */
	mustParseCIDR("2001:10::/28"),  /* Deprecated (previously ORCHID) - RFC 4843 */
	mustParseCIDR("2001:20::/28"),  /* ORCHIDv2 - RFC7343 */
}

type RetryConfig struct {
	WaitMin     time.Duration // Minimum time to wait
	WaitMax     time.Duration // Maximum time to wait
	MaxAttempts int           // Maximum number of retries

	// CheckRetry specifies the policy for handling retries, and is called
	// after each request. The default policy is [retryablehttp.DefaultRetryPolicy].
	CheckRetry retryablehttp.CheckRetry

	// Backoff specifies the policy for how long to wait between retries
	Backoff retryablehttp.Backoff

	// ErrorHandler specifies the custom error handler to use, if any
	ErrorHandler retryablehttp.ErrorHandler

	// PrepareRetry can prepare the request for retry operation, for example re-sign it
	PrepareRetry retryablehttp.PrepareRetry
}

// DefaultRetryConfig returns a [RetryConfig] populated with the defaults from
// [github.com/hashicorp/go-retryablehttp]. Use it as a starting point when
// only a few fields need to be overridden.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		WaitMin:      1 * time.Second,
		WaitMax:      30 * time.Second,
		MaxAttempts:  4,
		CheckRetry:   retryablehttp.DefaultRetryPolicy,
		Backoff:      retryablehttp.DefaultBackoff,
		ErrorHandler: nil,
		PrepareRetry: nil,
	}
}

type httpClientOptions struct {
	otelHTTPOptions   []otelhttp.Option
	retryConfig       *RetryConfig
	allowedCIDRBlocks []*net.IPNet
	resilience        *resilienceOptions
	allowedSchemes    map[string]struct{}
}

// ClientOption configures a single [Policy.Client] / [Policy.PooledClient]
// call. Values are produced by the With* helpers in this package (e.g.
// [WithAllowedCIDRBlocks]); callers outside the package hold and pass them
// but cannot construct new ones.
type ClientOption = func(*httpClientOptions)

// WithOTelHTTPOptions appends additional [otelhttp.Option] values to the
// OpenTelemetry transport instrumentation. Use this to configure trace
// propagation, filters, or span name formatters on a per-client basis.
func WithOTelHTTPOptions(options ...otelhttp.Option) func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		o.otelHTTPOptions = options
	}
}

// WithDefaultRetryConfig enables retry behaviour using the defaults from
// [DefaultRetryConfig].
func WithDefaultRetryConfig() func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		o.retryConfig = DefaultRetryConfig()
	}
}

// WithAllowedCIDRBlocks permits this client to dial IPs inside the given CIDR
// blocks even when the policy's blocklist would otherwise reject them. Use it
// only for clients whose destinations are trusted and not user-controlled —
// e.g. the assistant runtime client dialing GKE runner pods by the (RFC1918)
// pod IP it resolved from the Kubernetes API. The relaxation is scoped to this
// client; the policy's global enforcement is unchanged. Invalid CIDRs are
// ignored, so validate them at the configuration boundary.
func WithAllowedCIDRBlocks(cidrs ...string) func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		for _, cidr := range cidrs {
			if cidr == "" {
				continue
			}
			if block, err := parseCIDR(cidr); err == nil {
				o.allowedCIDRBlocks = append(o.allowedCIDRBlocks, block)
			}
		}
	}
}

// WithRetryConfig enables retry behaviour using the provided [RetryConfig].
func WithRetryConfig(config *RetryConfig) func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		o.retryConfig = config
	}
}

// WithAllowedSchemes replaces the policy's client URL scheme defaults with
// HTTPS plus the specified schemes. Scheme names are case-insensitive and
// surrounding whitespace is ignored. If no non-empty schemes remain, the
// policy defaults are preserved. The restriction is enforced for every
// request, including redirects and retry attempts.
func WithAllowedSchemes(schemes ...string) func(*httpClientOptions) {
	return func(o *httpClientOptions) {
		mapped := make(map[string]struct{})
		for _, scheme := range schemes {
			scheme = strings.ToLower(strings.TrimSpace(scheme))
			if scheme == "" {
				continue
			}
			mapped[scheme] = struct{}{}
		}

		if len(mapped) == 0 {
			return
		}

		mapped["https"] = struct{}{}

		o.allowedSchemes = mapped
	}
}

type Policy struct {
	tracerProvider        trace.TracerProvider
	blockedCIDRBlocks     []*net.IPNet
	resolver              dns.Resolver
	limiter               Limiter
	breaker               Breaker
	tlsRootCAs            *x509.CertPool
	defaultAllowedSchemes map[string]struct{}
}

// WithResolver is a functional option that sets the Policy's resolver.
// This is intended for tests that need to inject a [dns.MockResolver]; production
// code should use the default resolver supplied by the constructor.
func WithResolver(resolver dns.Resolver) func(*Policy) {
	return func(p *Policy) {
		p.resolver = resolver
	}
}

// WithTLSRootCAs is a functional option that replaces the root CA pool used
// by clients built from this Policy. This is intended for tests that fetch
// from httptest.NewTLSServer instances (whose certificates are self-signed);
// production code should rely on the system pool by leaving this unset.
func WithTLSRootCAs(pool *x509.CertPool) func(*Policy) {
	return func(p *Policy) {
		p.tlsRootCAs = pool
	}
}

// WithLimiter sets the rate limiter backing clients built with
// [WithResilience]. Defaults to a [NoopLimiter] that admits everything: pass
// an [InProcLimiter] for per-process limits or a [RedisRateLimiter] when
// limits must hold across replicas. A [NewNoopLimiter]-built NoopLimiter
// still admits everything but reports the bucket-count gauge, previewing
// partition cardinality before enforcement is switched on.
func WithLimiter(limiter Limiter) func(*Policy) {
	return func(p *Policy) {
		p.limiter = limiter
	}
}

// WithBreaker sets the circuit breaker backing clients built with
// [WithResilience]. Defaults to a [NoopBreaker] that admits everything: pass
// an [InProcBreaker] to enforce breaker policies. A [NewNoopBreaker]-built
// NoopBreaker still admits everything but reports the instance-count gauge,
// previewing partition cardinality before enforcement is switched on.
func WithBreaker(breaker Breaker) func(*Policy) {
	return func(p *Policy) {
		p.breaker = breaker
	}
}

// NewDefaultPolicy creates a new Policy that blocks common private and reserved
// IP ranges.
func NewDefaultPolicy(tracerProvider trace.TracerProvider, options ...func(*Policy)) *Policy {
	return newPolicy(tracerProvider, defaultBlockedCIDRBlocks, options...)
}

func newPolicy(tracerProvider trace.TracerProvider, blockedCIDRBlocks []*net.IPNet, options ...func(*Policy)) *Policy {
	// Limiter and breaker state deliberately lives on the Policy: clients are
	// often constructed per call site or per request, and resilience state
	// must outlive them. Resilience names are the partition key namespace,
	// so one shared pair serves every named configuration. The
	// no-op defaults make resilience enforcement strictly opt-in via
	// WithLimiter/WithBreaker.
	policy := &Policy{
		tracerProvider:    tracerProvider,
		blockedCIDRBlocks: blockedCIDRBlocks,
		resolver:          dns.NewNetResolver(),
		limiter:           nil,
		breaker:           nil,
		tlsRootCAs:        nil,
	}

	for _, option := range options {
		option(policy)
	}

	if policy.limiter == nil {
		policy.limiter = NewNoopLimiter(
			slog.New(slog.DiscardHandler),
			metricnoop.NewMeterProvider(),
		)
	}

	if policy.breaker == nil {
		policy.breaker = NewNoopBreaker(
			slog.New(slog.DiscardHandler),
			metricnoop.NewMeterProvider(),
		)
	}

	return policy
}

// NewUnsafePolicy creates a new Policy with the provided disallowed CIDR blocks.
// Clients built from this policy permit HTTP and HTTPS by default, preserving
// the unrestricted transport behavior required by local development and tests.
// It returns an error if any of the CIDR blocks cannot be parsed. Use
// NewDefaultPolicy for a safe default that blocks common private and reserved
// IP ranges and permits HTTPS only.
func NewUnsafePolicy(tracerProvider trace.TracerProvider, disallowedCIDRBlocks []string, options ...func(*Policy)) (*Policy, error) {
	var disallowedBlocks []*net.IPNet
	for _, cidr := range disallowedCIDRBlocks {
		block, err := parseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("%s: parse cidr: %w", cidr, err)
		}
		disallowedBlocks = append(disallowedBlocks, block)
	}

	policy := newPolicy(tracerProvider, disallowedBlocks, options...)
	policy.defaultAllowedSchemes = map[string]struct{}{
		"http":  {},
		"https": {},
	}
	return policy, nil
}

// PooledClient returns an [http.Client] that validates every request URL and
// uses a pooled transport that keeps idle connections alive for reuse.
// [NewDefaultPolicy] permits HTTPS requests by default; [NewUnsafePolicy]
// permits HTTP and HTTPS. Use [WithAllowedSchemes] to replace those defaults.
// PooledClient is appropriate for long-lived clients that make repeated
// requests to the same host(s). Do not use it for short-lived or one-off
// requests as idle connections hold open file descriptors until they time out.
func (p *Policy) PooledClient(options ...func(*httpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultPooledTransport(), options...)
}

// Client returns an [http.Client] that validates every request URL and opens a
// new connection for every request (keepalives disabled). [NewDefaultPolicy]
// permits HTTPS requests by default; [NewUnsafePolicy] permits HTTP and HTTPS.
// Use [WithAllowedSchemes] to replace those defaults. Because connections are
// never held idle, the client cannot leak file descriptors, making it safe for
// short-lived or one-off requests where connection reuse is unnecessary.
func (p *Policy) Client(options ...func(*httpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultTransport(), options...)
}

func (p *Policy) clientWithBaseTransport(transport *http.Transport, options ...func(*httpClientOptions)) *HTTPClient {
	var opts httpClientOptions
	for _, option := range options {
		option(&opts)
	}

	if opts.allowedSchemes == nil {
		opts.allowedSchemes = p.defaultAllowedSchemes
	}
	if opts.allowedSchemes == nil {
		opts.allowedSchemes = map[string]struct{}{"https": {}}
	}

	dialOpts := []func(*dialerOptions){}
	if len(opts.allowedCIDRBlocks) > 0 {
		dialOpts = append(dialOpts, WithDialerAllowedCIDRBlocks(opts.allowedCIDRBlocks))
	}
	transport.DialContext = p.clientDialContext(dialOpts...)

	// Merge into any existing transport TLS config rather than replacing
	// it, so a future option that sets client certificates or pinning is
	// not silently discarded when a root pool is also configured.
	if p.tlsRootCAs != nil {
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{RootCAs: p.tlsRootCAs, MinVersion: tls.VersionTLS12}
		} else {
			transport.TLSClientConfig.RootCAs = p.tlsRootCAs
		}
	}

	otelOpts := []otelhttp.Option{otelhttp.WithTracerProvider(p.tracerProvider)}
	otelOpts = append(otelOpts, opts.otelHTTPOptions...)

	// The annotator sits inside otelhttp, where the request context carries
	// the outbound HTTP span, and stamps the gram.resilience.* dimensions
	// derived per request by the resilience transport.
	var base http.RoundTripper = transport
	if opts.resilience != nil {
		base = &resilienceSpanAnnotator{next: transport}
	}

	// Retries sit outside the resilience layer so every attempt is admitted
	// and counted individually, and resilience sits outside otelhttp so only
	// requests that actually go out produce HTTP spans.
	var roundTripper http.RoundTripper = otelhttp.NewTransport(base, otelOpts...)
	if opts.resilience != nil {
		roundTripper = &resilienceTransport{
			next:    roundTripper,
			name:    opts.resilience.name,
			config:  opts.resilience.config,
			limiter: p.limiter,
			breaker: p.breaker,
		}
	}
	// URL validation is the outermost transport layer so malformed and blocked
	// requests do not consume resilience capacity or create outbound spans.
	roundTripper = &validatingTransport{
		next:              roundTripper,
		policy:            p,
		allowedSchemes:    opts.allowedSchemes,
		allowedCIDRBlocks: opts.allowedCIDRBlocks,
	}

	if opts.retryConfig == nil {
		return &http.Client{Transport: roundTripper}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil // avoid noisy logs from retryablehttp
	retryClient.HTTPClient = &http.Client{
		Transport: roundTripper,
	}

	checkRetry := opts.retryConfig.CheckRetry
	if opts.resilience != nil {
		checkRetry = noRetryOnResilienceDenial(checkRetry)
	}

	retryClient.RetryWaitMin = opts.retryConfig.WaitMin
	retryClient.RetryWaitMax = opts.retryConfig.WaitMax
	retryClient.RetryMax = opts.retryConfig.MaxAttempts
	retryClient.ErrorHandler = opts.retryConfig.ErrorHandler
	retryClient.PrepareRetry = opts.retryConfig.PrepareRetry

	// Nil CheckRetry/Backoff must keep retryablehttp's defaults: the client
	// invokes Backoff unconditionally and would panic on a nil value.
	if checkRetry != nil {
		retryClient.CheckRetry = checkRetry
	}
	if opts.retryConfig.Backoff != nil {
		retryClient.Backoff = opts.retryConfig.Backoff
	}

	return retryClient.StandardClient()
}

type resolvedRequestHost struct {
	host string
	ips  []net.IP
}

type resolvedRequestHostContextKey struct{}

type validatingTransport struct {
	next              http.RoundTripper
	policy            *Policy
	allowedSchemes    map[string]struct{}
	allowedCIDRBlocks []*net.IPNet
}

func (t *validatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ips, err := t.policy.validateURL(req.Context(), req.URL, t.allowedSchemes, t.allowedCIDRBlocks)
	if err != nil {
		return nil, fmt.Errorf("validate request url: %w", err)
	}

	ctx := context.WithValue(req.Context(), resolvedRequestHostContextKey{}, resolvedRequestHost{
		host: req.URL.Hostname(),
		ips:  ips,
	})
	resp, err := t.next.RoundTrip(req.Clone(ctx))
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}

	return resp, nil
}

type dialerOptions struct {
	resolver          *net.Resolver
	allowedCIDRBlocks []*net.IPNet
}

func WithDialerResolver(resolver *net.Resolver) func(*dialerOptions) {
	return func(o *dialerOptions) {
		o.resolver = resolver
	}
}

// WithDialerAllowedCIDRBlocks permits the dialer to connect to IPs inside the
// given blocks even when the policy blocklist covers them. See
// [WithAllowedCIDRBlocks] for when this is appropriate.
func WithDialerAllowedCIDRBlocks(blocks []*net.IPNet) func(*dialerOptions) {
	return func(o *dialerOptions) {
		o.allowedCIDRBlocks = blocks
	}
}

// Dialer returns a [net.Dialer] that enforces the policy's CIDR blocklist via
// [net.Dialer.ControlContext]. The check runs after DNS resolution on the
// raw IP address, so it cannot be bypassed by hostnames that resolve to
// blocked ranges. If the resolved IP falls within a blocked CIDR block the
// dial fails with [ErrBlockedIP]; malformed addresses fail with [ErrBadHost].
//
// Client and PooledClient use this dialer internally. Use Dialer directly only
// when you need to build a custom [http.Transport].
func (p *Policy) Dialer(options ...func(*dialerOptions)) *net.Dialer {
	var opts dialerOptions
	for _, option := range options {
		option(&opts)
	}

	resolver := opts.resolver
	if resolver == nil {
		resolver = p.resolver.Resolver()
	}

	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
		Resolver:  resolver,
		ControlContext: func(ctx context.Context, network string, address string, conn syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%s: split host port: %w: %w", address, ErrBadHost, err)
			}

			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("%s: %w: bad ip", address, ErrBadHost)
			}

			return p.checkIP(ip, opts.allowedCIDRBlocks)
		},
	}
}
func (p *Policy) clientDialContext(options ...func(*dialerOptions)) func(context.Context, string, string) (net.Conn, error) {
	var opts dialerOptions
	for _, option := range options {
		option(&opts)
	}

	dialer := p.Dialer(options...)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("%s: split host port: %w: %w", address, ErrBadHost, err)
		}

		var ips []net.IP
		if ip := net.ParseIP(host); ip != nil {
			if err := p.checkIP(ip, opts.allowedCIDRBlocks); err != nil {
				return nil, err
			}
			ips = []net.IP{ip}
		} else if resolved, ok := ctx.Value(resolvedRequestHostContextKey{}).(resolvedRequestHost); ok &&
			strings.EqualFold(resolved.host, host) {
			ips = resolved.ips
		} else {
			ips, err = p.resolveHost(ctx, host, opts.allowedCIDRBlocks)
			if err != nil {
				return nil, err
			}
		}

		dialErrors := make([]error, 0, len(ips))
		for _, ip := range ips {
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			dialErrors = append(dialErrors, dialErr)
		}

		return nil, fmt.Errorf("%s: dial resolved addresses: %w", address, errors.Join(dialErrors...))
	}
}

// ValidateHost checks whether the given host is permitted by the policy's
// CIDR blocklist. If host is an IP literal, it is checked directly; otherwise
// host is resolved via the policy's resolver and every returned address is
// checked. Returns [ErrBlockedIP] when any address falls within a blocked
// CIDR, and [ErrBadHost] when host is empty, fails to resolve, or resolves to
// no addresses.
//
// ValidateHost is intended for management-time URL validation so that callers
// reject blocked hosts before persisting them. [Policy.Client] and
// [Policy.PooledClient] resolve and validate every request host, then dial the
// validated IPs directly. Reusing that resolution for the connection avoids a
// redundant lookup and prevents DNS rebinding between validation and dialing.
//
// For hostnames with multiple resolved addresses, ValidateHost fails closed:
// any single blocked address rejects the host.
func (p *Policy) ValidateHost(ctx context.Context, host string) error {
	_, err := p.resolveHost(ctx, host, nil)
	return err
}

func (p *Policy) resolveHost(ctx context.Context, host string, allowedCIDRBlocks []*net.IPNet) ([]net.IP, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", ErrBadHost)
	}

	if ip := net.ParseIP(host); ip != nil {
		if err := p.checkIP(ip, allowedCIDRBlocks); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}

	ips, err := p.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%s: lookup ip: %w: %w", host, ErrBadHost, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%s: %w: no addresses", host, ErrBadHost)
	}

	for _, ip := range ips {
		if err := p.checkIP(ip, allowedCIDRBlocks); err != nil {
			return nil, err
		}
	}

	return ips, nil
}

// ValidateHTTPURL checks that rawURL is an absolute HTTP or HTTPS URL whose
// host is permitted by the policy's CIDR blocklist. It validates the URL
// string and resolves the host; it does not connect. Client and PooledClient
// apply the same host validation before every request and redirect, but permit
// only HTTPS unless [WithAllowedSchemes] explicitly enables HTTP.
func (p *Policy) ValidateHTTPURL(ctx context.Context, rawURL string) (*url.URL, error) {
	return p.validateAbsoluteURL(ctx, rawURL, false)
}

// ValidateHTTPSURL is [Policy.ValidateHTTPURL] restricted to https. Client and
// PooledClient enforce this same HTTPS-only policy by default, including on
// redirects.
func (p *Policy) ValidateHTTPSURL(ctx context.Context, rawURL string) (*url.URL, error) {
	return p.validateAbsoluteURL(ctx, rawURL, true)
}

func (p *Policy) validateAbsoluteURL(ctx context.Context, rawURL string, httpsOnly bool) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	allowedSchemes := map[string]struct{}{
		"https": {},
	}

	if !httpsOnly {
		allowedSchemes["http"] = struct{}{}
	}

	if _, err := p.validateURL(ctx, u, allowedSchemes, nil); err != nil {
		return nil, err
	}

	return u, nil
}

func (p *Policy) validateURL(
	ctx context.Context,
	u *url.URL,
	allowedSchemes map[string]struct{},
	allowedCIDRBlocks []*net.IPNet,
) ([]net.IP, error) {
	if u == nil {
		return nil, fmt.Errorf("url is nil")
	}

	if _, allowed := allowedSchemes[u.Scheme]; !allowed {
		return nil, fmt.Errorf("%s: url scheme not allowed", u.Scheme)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("url must include a host")
	}

	ips, err := p.resolveHost(ctx, u.Hostname(), allowedCIDRBlocks)
	if err != nil {
		return nil, fmt.Errorf("validate host: %w", err)
	}

	return ips, nil
}

// checkIP returns [ErrBlockedIP] if ip falls within any of the policy's
// blocked CIDR ranges unless an allowed block contains it. It is the shared
// CIDR-membership test used by request validation and [Policy.Dialer]'s
// ControlContext callback so both enforcement layers stay in sync.
func (p *Policy) checkIP(ip net.IP, allowedCIDRBlocks []*net.IPNet) error {
	for _, block := range allowedCIDRBlocks {
		if block.Contains(ip) {
			return nil
		}
	}

	for _, block := range p.blockedCIDRBlocks {
		if block.Contains(ip) {
			return fmt.Errorf("%s: %w", ip, ErrBlockedIP)
		}
	}
	return nil
}

func parseCIDR(cidr string) (*net.IPNet, error) {
	_, block, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse CIDR %s: %w", cidr, err)
	}

	return block, nil
}

func mustParseCIDR(cidr string) *net.IPNet {
	v, err := parseCIDR(cidr)
	if err != nil {
		panic(err)
	}

	return v
}
