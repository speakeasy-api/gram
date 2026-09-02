package netingress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const maxProjectedTokenBytes = 64 * 1024

func NewAttestorTransport(caPEM []byte) (*http.Transport, error) {
	if len(caPEM) == 0 {
		return nil, errors.New("attestor upstream CA bundle is required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("attestor upstream CA bundle contains no certificates")
	}
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport has an unsupported type")
	}
	transport := defaultTransport.Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return transport, nil
}

// AttestorConfig defines the per-ingress reverse proxy. ExpectedHost is the
// customer tailnet FQDN while Upstream is gram-server's private listener.
type AttestorConfig struct {
	Upstream     *url.URL
	ExpectedHost string
	TokenPath    string
	Transport    http.RoundTripper
	Logger       *slog.Logger
	Telemetry    *Telemetry
}

func NewAttestorHandler(config AttestorConfig) (http.Handler, error) {
	if config.Upstream == nil || config.Upstream.Scheme == "" || config.Upstream.Host == "" {
		return nil, errors.New("attestor upstream must be an absolute URL")
	}
	if config.Upstream.Scheme != "https" {
		return nil, errors.New("attestor upstream must use HTTPS")
	}
	if config.Upstream.User != nil || config.Upstream.Path != "" || config.Upstream.RawPath != "" || config.Upstream.RawQuery != "" || config.Upstream.ForceQuery || config.Upstream.Fragment != "" {
		return nil, errors.New("attestor upstream must not contain userinfo, path, query, or fragment")
	}
	if config.Transport == nil {
		return nil, errors.New("attestor upstream transport is required")
	}
	expectedHost, err := canonicalAuthority(config.ExpectedHost)
	if err != nil {
		return nil, fmt.Errorf("validate expected host: %w", err)
	}
	if config.TokenPath == "" {
		return nil, errors.New("projected token path is required")
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			originalHost := request.In.Host
			request.SetURL(config.Upstream)
			request.Out.Host = originalHost
			request.Out.Header.Del(AttestationHeader)
			request.Out.Header.Del("X-Real-Ip")
			StripUnsupportedTailscaleHeaders(request.Out.Header)
			request.Out.Header.Set(AttestationHeader, "Bearer "+projectedTokenFromContext(request.Out.Context()))
		},
		Director:      nil,
		Transport:     config.Transport,
		FlushInterval: 0,
		ErrorLog:      nil,
		BufferPool:    nil,
		ModifyResponse: func(response *http.Response) error {
			if response.Request == nil || response.Body == nil {
				return nil
			}
			ctx := response.Request.Context()
			started, _ := ctx.Value(proxyStartedKey{}).(time.Time)
			response.Body = &telemetryResponseBody{
				ReadCloser: response.Body,
				once:       sync.Once{},
				complete: func() {
					config.Telemetry.Record(ctx, OperationProxy, ResultAllowed, ReasonNone, ProviderTailscale, time.Since(started))
				},
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, request *http.Request, proxyErr error) {
			started, _ := request.Context().Value(proxyStartedKey{}).(time.Time)
			duration := time.Duration(0)
			if !started.IsZero() {
				duration = time.Since(started)
			}
			config.Telemetry.Record(request.Context(), OperationProxy, ResultError, ReasonUpstreamFailed, ProviderTailscale, duration)
			if config.Logger != nil {
				config.Logger.ErrorContext(request.Context(), "private ingress attestor proxy error", attr.SlogError(proxyErr))
			}
			http.Error(w, "private ingress upstream unavailable", http.StatusBadGateway)
		},
	}

	return RouteGuard(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		started := time.Now()
		request = request.WithContext(context.WithValue(request.Context(), proxyStartedKey{}, started))
		host, hostErr := canonicalAuthority(request.Host)
		if hostErr != nil || host != expectedHost {
			config.Telemetry.Record(request.Context(), OperationProxy, ResultDenied, ReasonHostMismatch, ProviderTailscale, time.Since(started))
			http.NotFound(w, request)
			return
		}
		token, readErr := readProjectedToken(config.TokenPath)
		if readErr != nil {
			config.Telemetry.Record(request.Context(), OperationProxy, ResultError, ReasonTokenReadFailed, ProviderTailscale, time.Since(started))
			if config.Logger != nil {
				config.Logger.ErrorContext(request.Context(), "read projected private ingress token", attr.SlogError(readErr))
			}
			http.Error(w, "private ingress attestor unavailable", http.StatusServiceUnavailable)
			return
		}
		proxy.ServeHTTP(w, request.WithContext(withProjectedToken(request.Context(), token)))
	})), nil
}

func canonicalAuthority(value string) (string, error) {
	host, err := requestorigin.CanonicalHost(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize authority: %w", err)
	}
	return host, nil
}

func readProjectedToken(path string) (string, error) {
	// #nosec G304 -- tokenPath is operator-controlled process configuration, not request input.
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read projected token: %w", err)
	}
	if len(contents) == 0 || len(contents) > maxProjectedTokenBytes {
		return "", errors.New("projected token has invalid size")
	}
	token := strings.TrimRight(string(contents), "\r\n")
	if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.ContainsRune(token, '\x00') {
		return "", errors.New("projected token is invalid")
	}
	return token, nil
}

type projectedTokenKey struct{}
type proxyStartedKey struct{}

type telemetryResponseBody struct {
	io.ReadCloser
	complete func()
	once     sync.Once
}

func (b *telemetryResponseBody) Read(buffer []byte) (int, error) {
	count, err := b.ReadCloser.Read(buffer)
	if errors.Is(err, io.EOF) {
		b.once.Do(b.complete)
		return count, err //nolint:wrapcheck // io.Reader callers require the EOF sentinel.
	}
	if err != nil {
		return count, fmt.Errorf("read proxied response body: %w", err)
	}
	return count, nil
}

func (b *telemetryResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.complete)
	if err != nil {
		return fmt.Errorf("close proxied response body: %w", err)
	}
	return nil
}

func withProjectedToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, projectedTokenKey{}, token)
}

func projectedTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(projectedTokenKey{}).(string)
	return token
}
