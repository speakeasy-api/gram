// Package guardian provides HTTP client construction with network security
// policy enforcement, OpenTelemetry instrumentation, and optional retry logic.
//
// It addresses three concerns:
//
//   - SSRF prevention: outbound connections are checked at the dialer level
//     against a configurable blocklist of CIDR ranges (all RFC-defined private
//     and reserved ranges by default). Because the check runs inside
//     [net.Dialer.ControlContext] after DNS resolution, it cannot be bypassed
//     by DNS rebinding.
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
package guardian

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
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

type htttpClientOptions struct {
	otelHTTPOptions []otelhttp.Option
	retryConfig     *RetryConfig
	resolver        *net.Resolver
}

// WithOTelHTTPOptions appends additional [otelhttp.Option] values to the
// OpenTelemetry transport instrumentation. Use this to configure trace
// propagation, filters, or span name formatters on a per-client basis.
func WithOTelHTTPOptions(options ...otelhttp.Option) func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.otelHTTPOptions = options
	}
}

// WithDefaultRetryConfig enables retry behaviour using the defaults from
// [DefaultRetryConfig].
func WithDefaultRetryConfig() func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.retryConfig = DefaultRetryConfig()
	}
}

// WithRetryConfig enables retry behaviour using the provided [RetryConfig].
func WithRetryConfig(config *RetryConfig) func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.retryConfig = config
	}
}

func WithResolver(resolver *net.Resolver) func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.resolver = resolver
	}
}

type Policy struct {
	tracerProvider    trace.TracerProvider
	blockedCIDRBlocks []*net.IPNet
	resolver          dns.Resolver
}

// NewDefaultPolicy creates a new Policy that blocks common private and reserved
// IP ranges.
func NewDefaultPolicy(tracerProvider trace.TracerProvider) *Policy {
	return &Policy{
		tracerProvider:    tracerProvider,
		blockedCIDRBlocks: defaultBlockedCIDRBlocks,
		resolver:          dns.NewNetResolver(),
	}
}

// NewUnsafePolicy creates a new Policy with the provided disallowed CIDR blocks.
// It returns an error if any of the CIDR blocks cannot be parsed.
// Use NewDefaultPolicy for a safe default that blocks common private and
// reserved IP ranges.
func NewUnsafePolicy(tracerProvider trace.TracerProvider, disallowedCIDRBlocks []string) (*Policy, error) {
	var disallowedBlocks []*net.IPNet
	for _, cidr := range disallowedCIDRBlocks {
		block, err := parseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("%s: parse cidr: %w", cidr, err)
		}
		disallowedBlocks = append(disallowedBlocks, block)
	}

	return &Policy{
		tracerProvider:    tracerProvider,
		blockedCIDRBlocks: disallowedBlocks,
		resolver:          dns.NewNetResolver(),
	}, nil
}

// WithResolver mutates the Policy in place to replace its resolver and
// returns the receiver for chaining. Intended for tests that need to inject a
// [dns.MockResolver]; production code should use the resolver supplied by the
// constructor.
func (p *Policy) WithResolver(resolver dns.Resolver) *Policy {
	p.resolver = resolver
	return p
}

// PooledClient returns an [http.Client] backed by a pooled transport that
// keeps idle connections alive for reuse. It is appropriate for long-lived
// clients that make repeated requests to the same host(s). Do not use it for
// short-lived or one-off requests as idle connections hold open file
// descriptors until they time out.
func (p *Policy) PooledClient(options ...func(*htttpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultPooledTransport(), options...)
}

// Client returns an [http.Client] that opens a new connection for every
// request (keepalives disabled). Because connections are never held idle,
// the client cannot leak file descriptors, making it safe for short-lived
// or one-off requests where connection reuse is unnecessary.
func (p *Policy) Client(options ...func(*htttpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultTransport(), options...)
}

func (p *Policy) clientWithBaseTransport(transport *http.Transport, options ...func(*htttpClientOptions)) *HTTPClient {
	var opts htttpClientOptions
	for _, option := range options {
		option(&opts)
	}

	dialOpts := []func(*dialerOptions){}
	if opts.resolver != nil {
		dialOpts = append(dialOpts, WithDialerResolver(opts.resolver))
	}
	transport.DialContext = p.Dialer(dialOpts...).DialContext

	otelOpts := []otelhttp.Option{otelhttp.WithTracerProvider(p.tracerProvider)}
	otelOpts = append(otelOpts, opts.otelHTTPOptions...)
	otelTransport := otelhttp.NewTransport(transport, otelOpts...)

	if opts.retryConfig == nil {
		return &http.Client{Transport: otelTransport}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil // avoid noisy logs from retryablehttp
	retryClient.HTTPClient = &http.Client{
		Transport: otelTransport,
	}

	retryClient.RetryWaitMin = opts.retryConfig.WaitMin
	retryClient.RetryWaitMax = opts.retryConfig.WaitMax
	retryClient.RetryMax = opts.retryConfig.MaxAttempts
	retryClient.CheckRetry = opts.retryConfig.CheckRetry
	retryClient.Backoff = opts.retryConfig.Backoff
	retryClient.ErrorHandler = opts.retryConfig.ErrorHandler
	retryClient.PrepareRetry = opts.retryConfig.PrepareRetry

	return retryClient.StandardClient()
}

type dialerOptions struct {
	resolver *net.Resolver
}

func WithDialerResolver(resolver *net.Resolver) func(*dialerOptions) {
	return func(o *dialerOptions) {
		o.resolver = resolver
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

			return p.checkIP(ip)
		},
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
// reject blocked hosts before persisting them. Runtime enforcement still
// happens via [Policy.Dialer] regardless.
//
// For hostnames with multiple resolved addresses, ValidateHost fails closed:
// any single blocked address rejects the host. This is stricter than the
// runtime [net.Dialer], which only fails when it actually attempts a blocked
// address. The asymmetry is intentional — validation should not persist a row
// whose host points anywhere blocked, even if a public address happens to be
// tried first at dial time.
func (p *Policy) ValidateHost(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("%w: empty host", ErrBadHost)
	}

	if ip := net.ParseIP(host); ip != nil {
		return p.checkIP(ip)
	}

	ips, err := p.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("%s: lookup ip: %w: %w", host, ErrBadHost, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%s: %w: no addresses", host, ErrBadHost)
	}

	for _, ip := range ips {
		if err := p.checkIP(ip); err != nil {
			return err
		}
	}

	return nil
}

// checkIP returns [ErrBlockedIP] if ip falls within any of the policy's
// blocked CIDR ranges, and nil otherwise. It is the shared CIDR-membership
// test used by both [Policy.Dialer]'s ControlContext callback and
// [Policy.ValidateHost], so that runtime and management-time enforcement stay
// in sync.
func (p *Policy) checkIP(ip net.IP) error {
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
