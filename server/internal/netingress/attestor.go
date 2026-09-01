package netingress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

const maxProjectedTokenBytes = 64 * 1024

// AttestorConfig defines the per-ingress reverse proxy. ExpectedHost is the
// customer tailnet FQDN while Upstream is gram-server's private listener.
type AttestorConfig struct {
	Upstream     *url.URL
	ExpectedHost string
	TokenPath    string
	Transport    http.RoundTripper
	Logger       *slog.Logger
}

func NewAttestorHandler(config AttestorConfig) (http.Handler, error) {
	if config.Upstream == nil || config.Upstream.Scheme == "" || config.Upstream.Host == "" {
		return nil, errors.New("attestor upstream must be an absolute URL")
	}
	if config.Upstream.Scheme != "http" && config.Upstream.Scheme != "https" {
		return nil, errors.New("attestor upstream must use HTTP or HTTPS")
	}
	if config.Upstream.User != nil || config.Upstream.Path != "" || config.Upstream.RawPath != "" || config.Upstream.RawQuery != "" || config.Upstream.ForceQuery || config.Upstream.Fragment != "" {
		return nil, errors.New("attestor upstream must not contain userinfo, path, query, or fragment")
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
		ModifyResponse: func(*http.Response) error {
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, request *http.Request, proxyErr error) {
			if config.Logger != nil {
				config.Logger.ErrorContext(request.Context(), "private ingress attestor proxy error", attr.SlogError(proxyErr))
			}
			http.Error(w, "private ingress upstream unavailable", http.StatusBadGateway)
		},
	}

	return RouteGuard(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		host, hostErr := canonicalAuthority(request.Host)
		if hostErr != nil || host != expectedHost {
			http.NotFound(w, request)
			return
		}
		token, readErr := readProjectedToken(config.TokenPath)
		if readErr != nil {
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

func withProjectedToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, projectedTokenKey{}, token)
}

func projectedTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(projectedTokenKey{}).(string)
	return token
}
