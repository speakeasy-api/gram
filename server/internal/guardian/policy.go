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
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration
	RetryMax     int
	CheckRetry   retryablehttp.CheckRetry
	Backoff      retryablehttp.Backoff
	ErrorHandler retryablehttp.ErrorHandler
	PrepareRetry retryablehttp.PrepareRetry
}

var defaultRetryClient = retryablehttp.NewClient()

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		RetryWaitMin: defaultRetryClient.RetryWaitMin,
		RetryWaitMax: defaultRetryClient.RetryWaitMax,
		RetryMax:     defaultRetryClient.RetryMax,
		CheckRetry:   defaultRetryClient.CheckRetry,
		Backoff:      defaultRetryClient.Backoff,
		ErrorHandler: defaultRetryClient.ErrorHandler,
		PrepareRetry: defaultRetryClient.PrepareRetry,
	}
}

type htttpClientOptions struct {
	otelHTTPOptions []otelhttp.Option
	retryConfig     *RetryConfig
}

func WithOTelHTTPOptions(options ...otelhttp.Option) func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.otelHTTPOptions = options
	}
}

func WithRetryConfig(config *RetryConfig) func(*htttpClientOptions) {
	return func(o *htttpClientOptions) {
		o.retryConfig = config
	}
}

type Policy struct {
	tracerProvider    trace.TracerProvider
	blockedCIDRBlocks []*net.IPNet
}

// NewDefaultPolicy creates a new Policy that blocks common private and reserved
// IP ranges.
func NewDefaultPolicy(tracerProvider trace.TracerProvider) *Policy {
	return &Policy{
		tracerProvider:    tracerProvider,
		blockedCIDRBlocks: defaultBlockedCIDRBlocks,
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
	}, nil
}

func (p *Policy) PooledClient(options ...func(*htttpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultPooledTransport(), options...)
}

func (p *Policy) Client(options ...func(*htttpClientOptions)) *HTTPClient {
	return p.clientWithBaseTransport(cleanhttp.DefaultTransport(), options...)
}

func (p *Policy) clientWithBaseTransport(transport *http.Transport, options ...func(*htttpClientOptions)) *HTTPClient {
	var opts htttpClientOptions
	for _, option := range options {
		option(&opts)
	}

	transport.DialContext = p.Dialer().DialContext

	otelOpts := []otelhttp.Option{otelhttp.WithTracerProvider(p.tracerProvider)}
	otelOpts = append(otelOpts, opts.otelHTTPOptions...)
	otelTransport := otelhttp.NewTransport(transport, otelOpts...)

	if opts.retryConfig == nil {
		return &http.Client{Transport: otelTransport}
	}

	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = &http.Client{
		Transport: otelTransport,
	}

	retryClient.RetryWaitMin = opts.retryConfig.RetryWaitMin
	retryClient.RetryWaitMax = opts.retryConfig.RetryWaitMax
	retryClient.RetryMax = opts.retryConfig.RetryMax
	retryClient.CheckRetry = opts.retryConfig.CheckRetry
	retryClient.Backoff = opts.retryConfig.Backoff
	retryClient.ErrorHandler = opts.retryConfig.ErrorHandler
	retryClient.PrepareRetry = opts.retryConfig.PrepareRetry

	return retryClient.StandardClient()
}

func (p *Policy) Dialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
		ControlContext: func(ctx context.Context, network string, address string, conn syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return fmt.Errorf("%s: split host port: %w: %w", address, ErrBadHost, err)
			}

			ip := net.ParseIP(host)
			if ip == nil {
				return fmt.Errorf("%s: %w: bad ip", address, ErrBadHost)
			}

			for _, block := range p.blockedCIDRBlocks {
				if block.Contains(ip) {
					return fmt.Errorf("%s: %w", ip, ErrBlockedIP)
				}
			}

			return nil
		},
	}
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
