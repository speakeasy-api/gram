// Package catalogsource provides a dedicated authenticated, read-only catalog
// transfer boundary. Its verifier must never authorize administrative writes.
package catalogsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"golang.org/x/oauth2"
	"google.golang.org/api/idtoken"
	"google.golang.org/api/impersonate"
)

const ExportPath = "/admin/catalog.export"
const maxTokenBytes = 16 << 10

var errUnauthorized = errors.New("catalog source authentication failed")

// Validator is implemented by Google's signature, expiry and audience validator.
type Validator interface {
	Validate(context.Context, string, string) (*idtoken.Payload, error)
}

type Verifier interface {
	Verify(context.Context, string) error
}

type GoogleVerifier struct {
	audience  string
	subjects  map[string]struct{}
	validator Validator
}

// NewGoogleVerifier copies the immutable numeric subject allowlist. A nil
// validator selects Google's real verifier, which fetches only Google JWKs.
func NewGoogleVerifier(ctx context.Context, audience string, subjects []string, validator Validator) (*GoogleVerifier, error) {
	if strings.TrimSpace(audience) == "" || len(subjects) == 0 {
		return nil, errors.New("catalog source audience and subjects are required")
	}
	allowed := make(map[string]struct{}, len(subjects))
	for _, s := range subjects {
		if s == "" || strings.Trim(s, "0123456789") != "" {
			return nil, errors.New("catalog source subjects must be numeric IDs")
		}
		allowed[s] = struct{}{}
	}
	if validator == nil {
		var err error
		validator, err = idtoken.NewValidator(ctx)
		if err != nil {
			return nil, fmt.Errorf("initialize catalog source validator: %w", err)
		}
	}
	return &GoogleVerifier{audience: audience, subjects: allowed, validator: validator}, nil
}

func (v *GoogleVerifier) Verify(ctx context.Context, token string) error {
	if v == nil || v.validator == nil || v.audience == "" || len(v.subjects) == 0 || !validToken(token) {
		return errUnauthorized
	}
	p, err := v.validator.Validate(ctx, token, v.audience)
	if err != nil || p == nil {
		return errUnauthorized
	}
	if (p.Issuer != "https://accounts.google.com" && p.Issuer != "accounts.google.com") || p.Audience != v.audience || p.Expires <= time.Now().Unix() {
		return errUnauthorized
	}
	if _, ok := v.subjects[p.Subject]; !ok {
		return errUnauthorized
	}
	return nil
}

func validToken(token string) bool {
	if len(token) == 0 || len(token) > maxTokenBytes {
		return false
	}
	for _, c := range token {
		if c <= 32 || c >= 127 {
			return false
		}
	}
	return true
}

// NewExportHandler authorizes only this read-only GET endpoint. Do not install
// it as middleware on the admin router.
func NewExportHandler(export func(context.Context) (mcpregistry.CatalogBundle, error), verifier Verifier, logger *slog.Logger) http.Handler {
	if logger == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "catalog export unavailable", http.StatusServiceUnavailable)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		headers := r.Header.Values("Authorization")
		token := ""
		if len(headers) == 1 && len(headers[0]) <= maxTokenBytes+7 && len(headers[0]) >= 7 && strings.EqualFold(headers[0][:7], "Bearer ") {
			token = headers[0][7:]
		}
		if !validToken(token) || verifier == nil || verifier.Verify(r.Context(), token) != nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if export == nil {
			http.Error(w, "catalog export unavailable", http.StatusInternalServerError)
			return
		}
		bundle, err := export(r.Context())
		if err != nil {
			logger.ErrorContext(r.Context(), "catalog export failed", attr.SlogError(err))
			http.Error(w, "catalog export unavailable", http.StatusInternalServerError)
			return
		}
		var buf boundedBuffer
		if err := json.NewEncoder(&buf).Encode(bundle); err != nil {
			logger.ErrorContext(r.Context(), "encode catalog export failed", attr.SlogError(err))
			http.Error(w, "catalog export unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(buf.Bytes())
	})
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > mcpregistry.CatalogBundleByteLimit-b.Len() {
		return 0, errors.New("catalog bundle exceeds size limit")
	}
	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer catalog export: %w", err)
	}
	return n, nil
}

// Client pins the source URL at construction. It never follows redirects.
type Client struct {
	url    string
	http   *http.Client //nolint:forbidigo // Fixed trusted deployment URL, not a user-controlled outbound request.
	tokens oauth2.TokenSource
}

//nolint:forbidigo // HTTPS endpoint is fixed at trusted startup; custom clients permit TLS trust configuration.
func NewClient(sourceURL string, tokens oauth2.TokenSource, client *http.Client) (*Client, error) {
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(sourceURL, "#") || u.Path != ExportPath || u.RawPath != "" || u.Opaque != "" {
		return nil, errors.New("catalog source must be an HTTPS catalog export URL without credentials, query or fragment")
	}
	if tokens == nil {
		return nil, errors.New("catalog source token source is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if copyClient.Timeout == 0 {
		copyClient.Timeout = 60 * time.Second
	}
	return &Client{url: u.String(), tokens: tokens, http: &copyClient}, nil
}

func (c *Client) Fetch(ctx context.Context) (mcpregistry.CatalogBundle, error) {
	var empty mcpregistry.CatalogBundle
	var token *oauth2.Token
	var err error
	if ctx.Err() != nil {
		return empty, errors.New("catalog source token unavailable")
	}
	if source, ok := c.tokens.(ContextTokenSource); ok {
		token, err = source.TokenContext(ctx)
	} else {
		token, err = c.tokens.Token()
	}
	if err != nil || token == nil || !token.Valid() || !validToken(token.AccessToken) {
		return empty, errors.New("catalog source token unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return empty, errors.New("catalog source request failed")
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return empty, errors.New("catalog source request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return empty, errors.New("catalog source returned unsuccessful status")
	}
	bundle, err := mcpregistry.DecodeCatalogBundle(resp.Body)
	if err != nil {
		return empty, errors.New("catalog source returned invalid bundle")
	}
	return bundle, nil
}

// ContextTokenSource supports cancellation during credential discovery and refresh.
// Client uses this interface when available, while accepting ordinary test sources.
type ContextTokenSource interface {
	TokenContext(context.Context) (*oauth2.Token, error)
}

const tokenAcquisitionTimeout = 30 * time.Second

// NewGoogleTokenSource defers credential discovery and token acquisition until
// Token is called. Empty reader uses workload identity; a configured dedicated
// reader uses ADC impersonation. ctx is used only by the legacy Token method;
// Client.Fetch supplies its own request context through TokenContext.
func NewGoogleTokenSource(ctx context.Context, audience, reader string) (oauth2.TokenSource, error) {
	if strings.TrimSpace(audience) == "" {
		return nil, errors.New("catalog source audience is required")
	}
	return &lazyTokenSource{ctx: ctx, audience: audience, reader: reader, gate: make(chan struct{}, 1), token: nil, newSource: googleTokenSource}, nil
}

func googleTokenSource(ctx context.Context, audience, reader string) (oauth2.TokenSource, error) {
	if reader == "" {
		source, err := idtoken.NewTokenSource(ctx, audience)
		if err != nil {
			return nil, errors.New("catalog source credentials unavailable")
		}
		return source, nil
	}
	source, err := impersonate.IDTokenSource(ctx, impersonate.IDTokenConfig{TargetPrincipal: reader, Audience: audience, IncludeEmail: false, Delegates: nil})
	if err != nil {
		return nil, errors.New("catalog source credentials unavailable")
	}
	return source, nil
}

//nolint:containedctx // Legacy oauth2.TokenSource has no context argument; Fetch always uses TokenContext instead.
type lazyTokenSource struct {
	ctx              context.Context
	audience, reader string
	gate             chan struct{}
	token            *oauth2.Token
	newSource        func(context.Context, string, string) (oauth2.TokenSource, error)
}

func (s *lazyTokenSource) Token() (*oauth2.Token, error) { return s.TokenContext(s.ctx) }

func (s *lazyTokenSource) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, tokenAcquisitionTimeout)
	defer cancel()
	// Waiting for another refresh is cancellable too. Never hold a mutex across
	// network I/O or cache a Google source bound to a previous request context.
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, errors.New("catalog source token unavailable")
	}
	defer func() { <-s.gate }()
	if ctx.Err() != nil {
		return nil, errors.New("catalog source token unavailable")
	}
	if s.token.Valid() {
		token := *s.token
		return &token, nil
	}
	source, err := s.newSource(ctx, s.audience, s.reader)
	if err != nil {
		return nil, errors.New("catalog source credentials unavailable")
	}
	token, err := source.Token()
	if err != nil || ctx.Err() != nil || token == nil || !token.Valid() || !validToken(token.AccessToken) {
		return nil, errors.New("catalog source token unavailable")
	}
	cached := *token
	s.token = &cached
	result := cached
	return &result, nil
}
