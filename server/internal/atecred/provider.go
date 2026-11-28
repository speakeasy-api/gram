package atecred

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/simplelru"
	credprovider "go.ate.dev/substrate/api/credprovider/v1"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// maxCachedActorCredentials bounds in-memory cached actor credentials in the
	// credential provider process.
	maxCachedActorCredentials = 4096

	// cacheExpirySafetyMargin guarantees any cached credential returned to ATE's
	// egress gateway still has at least this much lifetime left before expiry.
	cacheExpirySafetyMargin = 30 * time.Second

	// maxMintResponseBytes bounds the JSON response body read from the Speakeasy
	// server's internal mint endpoint.
	maxMintResponseBytes = 64 << 10 // 64 KiB
)

var (
	// ErrMintDenied marks a permanent authorization or actor-state refusal from
	// the Speakeasy mint endpoint (mapped to gRPC PermissionDenied -> HTTP 403).
	ErrMintDenied = errors.New("credential mint denied")

	// ErrMintUnavailable marks a transient transport, timeout, or upstream server
	// failure (mapped to gRPC Unavailable -> retryable HTTP 503).
	ErrMintUnavailable = errors.New("credential mint unavailable")
)

// MintClient calls the Speakeasy server's internal mint endpoint on behalf of
// the ATE credential provider.
type MintClient interface {
	// Mint requests a short-lived turn credential for req.
	Mint(ctx context.Context, req MintRequest) (MintResponse, error)
}

// HTTPDoer executes one outbound HTTP request to the Speakeasy mint endpoint.
// *guardian.HTTPClient satisfies this interface in production.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPMintClientConfig configures HTTPMintClient.
type HTTPMintClientConfig struct {
	// EndpointURL is the exact HTTPS (or local HTTP) mint endpoint URL.
	EndpointURL string

	// TokenSource supplies the Authorization bearer token for the server.
	TokenSource oauth2.TokenSource

	// HTTPClient is the underlying guarded HTTP client.
	HTTPClient HTTPDoer

	// RequestTimeout bounds one mint request. Defaults to DefaultRequestTimeout.
	RequestTimeout time.Duration
}

// HTTPMintClient is the production MintClient calling POST /internal/assistants/ate/credentials.
type HTTPMintClient struct {
	endpointURL    string
	tokenSource    oauth2.TokenSource
	httpClient     HTTPDoer
	requestTimeout time.Duration
}

// NewHTTPMintClient constructs an HTTPMintClient.
func NewHTTPMintClient(cfg HTTPMintClientConfig) *HTTPMintClient {
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return &HTTPMintClient{
		endpointURL:    cfg.EndpointURL,
		tokenSource:    cfg.TokenSource,
		httpClient:     cfg.HTTPClient,
		requestTimeout: timeout,
	}
}

// Mint calls the Speakeasy server's internal mint endpoint and maps HTTP
// failures strictly to ErrMintDenied (4xx) or ErrMintUnavailable (5xx/429/net).
func (c *HTTPMintClient) Mint(ctx context.Context, req MintRequest) (MintResponse, error) {
	if c == nil || c.endpointURL == "" || c.tokenSource == nil || c.httpClient == nil {
		return MintResponse{}, fmt.Errorf("%w: mint client is not configured", ErrMintUnavailable)
	}

	callCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	tok, err := tokenWithContext(callCtx, c.tokenSource)
	if err != nil || tok == nil || tok.AccessToken == "" {
		return MintResponse{}, fmt.Errorf("%w: acquire provider auth token: %w", ErrMintUnavailable, err)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return MintResponse{}, fmt.Errorf("%w: encode mint request: %w", ErrMintDenied, err)
	}

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.endpointURL, bytes.NewReader(body))
	if err != nil {
		return MintResponse{}, fmt.Errorf("%w: build mint request: %w", ErrMintUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return MintResponse{}, fmt.Errorf("%w: call mint endpoint: %w", ErrMintUnavailable, err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMintResponseBytes+1))
	if err != nil {
		return MintResponse{}, fmt.Errorf("%w: read mint response: %w", ErrMintUnavailable, err)
	}
	if len(raw) > maxMintResponseBytes {
		return MintResponse{}, fmt.Errorf("%w: mint response exceeds size limit", ErrMintUnavailable)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var parsed MintResponse
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return MintResponse{}, fmt.Errorf("%w: decode mint response: %w", ErrMintUnavailable, err)
		}
		if !validHeaderSecret(parsed.Credential) || parsed.ExpiresAt.IsZero() {
			return MintResponse{}, fmt.Errorf("%w: mint response returned invalid credential payload", ErrMintUnavailable)
		}
		return parsed, nil
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return MintResponse{}, fmt.Errorf("%w: server returned status %d", ErrMintDenied, resp.StatusCode)
	default:
		return MintResponse{}, fmt.Errorf("%w: server returned status %d", ErrMintUnavailable, resp.StatusCode)
	}
}

// ServerConfig configures the ATE CredentialProvider gRPC service.
type ServerConfig struct {
	// Logger receives structured refusal and fault logs (never credential values).
	Logger *slog.Logger

	// Atespace is the single ATE atespace this provider serves. Requests for any
	// other atespace are refused with PermissionDenied.
	Atespace string

	// CacheTTL bounds in-memory reuse of a minted actor credential. Defaults to
	// DefaultCacheTTL.
	CacheTTL time.Duration

	// Client calls the Speakeasy server's internal mint endpoint on cache miss.
	Client MintClient

	// Now supplies the current clock; defaults to time.Now.
	Now func() time.Time
}

type cachedCredential struct {
	value       string
	cachedUntil time.Time
}

// Server implements substrate.credprovider.v1.CredentialProviderServer.
type Server struct {
	credprovider.UnimplementedCredentialProviderServer

	logger   *slog.Logger
	atespace string
	cacheTTL time.Duration
	client   MintClient
	now      func() time.Time

	mu    sync.Mutex
	cache *simplelru.LRU[ActorRef, cachedCredential]
	group singleflight.Group
}

// NewServer constructs an ATE CredentialProvider gRPC server.
func NewServer(cfg ServerConfig) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	cacheTTL := cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = DefaultCacheTTL
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	entries, err := simplelru.NewLRU[ActorRef, cachedCredential](maxCachedActorCredentials, nil)
	if err != nil {
		panic(fmt.Sprintf("create ATE credential cache: %v", err))
	}
	return &Server{
		UnimplementedCredentialProviderServer: credprovider.UnimplementedCredentialProviderServer{},
		logger:                                logger,
		atespace:                              cfg.Atespace,
		cacheTTL:                              cacheTTL,
		client:                                cfg.Client,
		now:                                   nowFn,
		mu:                                    sync.Mutex{},
		cache:                                 entries,
		group:                                 singleflight.Group{},
	}
}

func grpcStatusErr(code codes.Code, msg string) error {
	return status.Error(code, msg) //nolint:wrapcheck // gRPC status errors are returned directly to preserve status.Code semantics.
}

// FetchSecret verifies that the gateway-attested actor SPIFFE ID matches the
// credential URI and configured atespace, then returns a short-lived Speakeasy
// turn credential from cache or the server mint endpoint.
func (s *Server) FetchSecret(ctx context.Context, req *credprovider.FetchSecretRequest) (*credprovider.FetchSecretResponse, error) {
	if s == nil || s.client == nil || !ValidATEName(s.atespace) {
		return nil, grpcStatusErr(codes.Unavailable, "credential provider is not configured")
	}

	spiffeAtespace, spiffeActorName, err := ParseActorSPIFFEID(req.GetActorSpiffeId())
	if err != nil {
		s.logger.WarnContext(ctx, "ATE credential request denied: invalid actor SPIFFE ID", attr.SlogError(err))
		return nil, grpcStatusErr(codes.PermissionDenied, "invalid actor SPIFFE identity")
	}

	ref, err := ParseCredentialURI(req.GetUri())
	if err != nil {
		s.logger.WarnContext(ctx, "ATE credential request denied: invalid credential URI", attr.SlogError(err))
		return nil, grpcStatusErr(codes.InvalidArgument, err.Error())
	}

	if spiffeAtespace != s.atespace || ref.Atespace != s.atespace || ref.ActorName != spiffeActorName {
		s.logger.WarnContext(ctx, "ATE credential request denied: actor identity mismatch")
		return nil, grpcStatusErr(codes.PermissionDenied, "actor SPIFFE identity does not match credential URI or configured atespace")
	}

	now := s.now()
	if value, ok := s.getCached(ref, now); ok {
		return &credprovider.FetchSecretResponse{OpaqueBytes: []byte(value)}, nil
	}

	flightKey := ref.Atespace + "\x00" + ref.ActorName + "\x00" + ref.ActorUID
	res, err, _ := s.group.Do(flightKey, func() (any, error) {
		if value, ok := s.getCached(ref, s.now()); ok {
			return value, nil
		}
		minted, mintErr := s.client.Mint(ctx, MintRequest(ref))
		if mintErr != nil {
			return "", fmt.Errorf("mint actor credential: %w", mintErr)
		}
		if !validHeaderSecret(minted.Credential) {
			return "", fmt.Errorf("%w: minted credential failed header validation", ErrMintUnavailable)
		}
		s.putCached(ref, minted.Credential, minted.ExpiresAt, s.now())
		return minted.Credential, nil
	})
	if err != nil {
		if errors.Is(err, ErrMintDenied) {
			s.logger.WarnContext(ctx, "ATE credential mint denied by server", attr.SlogError(err))
			return nil, grpcStatusErr(codes.PermissionDenied, "actor is not permitted to mint a credential")
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrMintUnavailable) {
			s.logger.ErrorContext(ctx, "ATE credential mint unavailable", attr.SlogError(err))
			return nil, grpcStatusErr(codes.Unavailable, "credential mint temporarily unavailable")
		}
		s.logger.ErrorContext(ctx, "ATE credential mint failed", attr.SlogError(err))
		return nil, grpcStatusErr(codes.Unavailable, "credential mint failed")
	}

	cred, _ := res.(string)
	if !validHeaderSecret(cred) {
		return nil, grpcStatusErr(codes.Unavailable, "credential provider returned invalid secret")
	}
	return &credprovider.FetchSecretResponse{OpaqueBytes: []byte(cred)}, nil
}

func (s *Server) getCached(ref ActorRef, now time.Time) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.cache.Get(ref)
	if !ok {
		return "", false
	}
	if !now.Before(entry.cachedUntil) {
		s.cache.Remove(ref)
		return "", false
	}
	return entry.value, true
}

func (s *Server) putCached(ref ActorRef, value string, expiresAt, now time.Time) {
	safeExpiry := expiresAt.Add(-cacheExpirySafetyMargin)
	if !safeExpiry.After(now) {
		return
	}
	cachedUntil := now.Add(s.cacheTTL)
	if safeExpiry.Before(cachedUntil) {
		cachedUntil = safeExpiry
	}
	s.mu.Lock()
	s.cache.Add(ref, cachedCredential{value: value, cachedUntil: cachedUntil})
	s.mu.Unlock()
}

// validHeaderSecret checks that a credential is non-empty and contains only
// printable visible ASCII characters so ATE's egress gateway never rejects it
// for control characters or whitespace.
func validHeaderSecret(raw string) bool {
	if raw == "" {
		return false
	}
	for i := range len(raw) {
		b := raw[i]
		if b <= 0x20 || b >= 0x7f {
			return false
		}
	}
	return true
}

func tokenWithContext(ctx context.Context, ts oauth2.TokenSource) (*oauth2.Token, error) {
	type result struct {
		tok *oauth2.Token
		err error
	}
	ch := make(chan result, 1)
	go func() {
		tok, err := ts.Token()
		ch <- result{tok: tok, err: err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("fetch token from source: %w", res.err)
		}
		return res.tok, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("token acquisition timed out: %w", ctx.Err())
	}
}
