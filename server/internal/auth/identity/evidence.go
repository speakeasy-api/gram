package identity

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/workos/workos-go/v6/pkg/usermanagement"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// DefaultGoogleUserInfoURL is Google's OpenID Connect userinfo endpoint.
const DefaultGoogleUserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"

// evidenceTimeout bounds the lookups that establish sign-in evidence. A lookup
// that fails or times out leaves the evidence empty, which never qualifies.
const evidenceTimeout = 2 * time.Second

// sessionLookupPages bounds the WorkOS session listing that finds when a
// session was created.
const sessionLookupPages = 3

// Evidence lookup outcomes.
const (
	evidenceOK        = "ok"
	evidenceNoTokens  = "no_tokens"
	evidenceHTTPError = "http_error"
	evidenceTimedOut  = "timeout"
	evidenceNotFound  = "not_found"
)

// Evidence lookup kinds beyond the authentication methods themselves.
const evidenceSession = "session"

// SignInEvidence is what the identity provider vouched for at sign-in, beyond
// the authentication method.
type SignInEvidence struct {
	// HostedDomain is the domain a social provider says manages the account:
	// Google's Workspace hosted domain (hd) or a Microsoft work account's UPN
	// domain.
	HostedDomain string
	// TenantID is a Microsoft account's Entra tenant.
	TenantID string
	// VerifiedEmail is the user's email when WorkOS reports it verified.
	VerifiedEmail string
	// AuthenticatedAt is when WorkOS created the session, i.e. when the user
	// actually authenticated; zero when unknown. A silently reused session
	// keeps its original time.
	AuthenticatedAt time.Time
}

// EvidenceGate reports whether sign-in evidence is collected for the WorkOS
// organization chosen at sign-in.
type EvidenceGate func(ctx context.Context, workosOrganizationID string) bool

// EvidenceConfig configures sign-in evidence collection.
type EvidenceConfig struct {
	Logger        *slog.Logger
	MeterProvider metric.MeterProvider
	// Enabled gates every external lookup; nil disables them.
	Enabled EvidenceGate
}

type evidenceCollector struct {
	logger  *slog.Logger
	lookups metric.Int64Counter
	enabled EvidenceGate
}

func newEvidenceCollector(cfg EvidenceConfig) evidenceCollector {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	var provider metric.MeterProvider = noop.NewMeterProvider()
	if cfg.MeterProvider != nil {
		provider = cfg.MeterProvider
	}
	lookups, err := provider.Meter("github.com/speakeasy-api/gram/server/internal/auth/identity").Int64Counter(
		"sign_in.evidence_lookups",
		metric.WithDescription("Sign-in evidence lookups by kind and outcome."),
		metric.WithUnit("{lookup}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create sign-in evidence counter", attr.SlogError(err))
	}
	return evidenceCollector{logger: logger, lookups: lookups, enabled: cfg.Enabled}
}

func (c evidenceCollector) count(ctx context.Context, kind, outcome string) {
	if c.lookups != nil {
		c.lookups.Add(ctx, 1, metric.WithAttributes(attr.AuthAuthenticationMethod(kind), attr.Outcome(outcome)))
	}
}

func lookupOutcome(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return evidenceTimedOut
	}
	return evidenceHTTPError
}

func (a *WorkOSAdapter) signInEvidence(ctx context.Context, resp usermanagement.AuthenticateResponse) SignInEvidence {
	var ev SignInEvidence
	if resp.User.EmailVerified {
		ev.VerifiedEmail = resp.User.Email
	}
	c := a.evidence
	ctx, cancel := context.WithTimeout(ctx, evidenceTimeout)
	defer cancel()
	if c.enabled == nil || resp.OrganizationID == "" || !c.enabled(ctx, resp.OrganizationID) {
		return ev
	}

	ev.AuthenticatedAt = a.sessionCreatedAt(ctx, resp.User.ID, extractSessionIDFromJWT(resp.AccessToken))

	method := string(resp.AuthenticationMethod)
	switch resp.AuthenticationMethod {
	case usermanagement.GoogleOAuth:
		if resp.OAuthTokens == nil || resp.OAuthTokens.AccessToken == "" {
			c.missingTokens(ctx, method)
			return ev
		}
		hd, err := a.googleHostedDomain(ctx, resp.OAuthTokens.AccessToken)
		if err != nil {
			c.count(ctx, method, lookupOutcome(ctx, err))
			return ev
		}
		c.count(ctx, method, evidenceOK)
		ev.HostedDomain = hd
		return ev
	case usermanagement.MicrosoftOAuth:
		if resp.OAuthTokens == nil || resp.OAuthTokens.AccessToken == "" {
			c.missingTokens(ctx, method)
			return ev
		}
		c.count(ctx, method, evidenceOK)
		ms := microsoftEvidence(resp.OAuthTokens.AccessToken)
		ev.HostedDomain, ev.TenantID = ms.HostedDomain, ms.TenantID
		return ev
	default:
		return ev
	}
}

func (c evidenceCollector) missingTokens(ctx context.Context, method string) {
	c.count(ctx, method, evidenceNoTokens)
	c.logger.WarnContext(ctx, "workos returned no oauth tokens; enable returning OAuth tokens on the WorkOS social connection so the sign-in can qualify",
		attr.SlogAuthAuthenticationMethod(method))
}

// sessionCreatedAt finds when WorkOS created the session, or zero.
func (a *WorkOSAdapter) sessionCreatedAt(ctx context.Context, workosUserID, sessionID string) time.Time {
	if workosUserID == "" || sessionID == "" {
		return time.Time{}
	}
	after := ""
	for range sessionLookupPages {
		resp, err := a.client.ListSessions(ctx, workosUserID, usermanagement.ListSessionsOpts{
			Limit:  100,
			Before: "",
			After:  after,
			Order:  usermanagement.Desc,
		})
		if err != nil {
			a.evidence.count(ctx, evidenceSession, lookupOutcome(ctx, err))
			return time.Time{}
		}
		for _, session := range resp.Data {
			if session.ID != sessionID {
				continue
			}
			created, err := time.Parse(time.RFC3339Nano, session.CreatedAt)
			if err != nil {
				a.evidence.count(ctx, evidenceSession, evidenceNotFound)
				return time.Time{}
			}
			a.evidence.count(ctx, evidenceSession, evidenceOK)
			return created
		}
		if resp.ListMetadata.After == "" || len(resp.Data) == 0 {
			break
		}
		after = resp.ListMetadata.After
	}
	a.evidence.count(ctx, evidenceSession, evidenceNotFound)
	return time.Time{}
}

// googleHostedDomain asks Google which Workspace manages the account. A
// personal Google account has no hd, even when its email is a company address.
func (a *WorkOSAdapter) googleHostedDomain(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.googleUserInfoURL, nil)
	if err != nil {
		return "", fmt.Errorf("build google userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	if a.client.HTTPClient == nil {
		return "", errors.New("google userinfo: no http client")
	}
	res, err := a.client.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("google userinfo: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("google userinfo: status %d", res.StatusCode)
	}
	var info struct {
		HD string `json:"hd"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&info); err != nil {
		return "", fmt.Errorf("decode google userinfo: %w", err)
	}
	return strings.ToLower(info.HD), nil
}

// microsoftEvidence reads the tenant and user principal name from the Graph
// access token WorkOS obtained server to server. Only a work or school
// account's upn counts; tokens that aren't readable JWTs (personal accounts
// get opaque ones) yield no evidence.
func microsoftEvidence(accessToken string) SignInEvidence {
	var ev SignInEvidence
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ev
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ev
	}
	var claims struct {
		TID string `json:"tid"`
		UPN string `json:"upn"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ev
	}
	at := strings.LastIndexByte(claims.UPN, '@')
	if claims.TID == "" || at <= 0 || at == len(claims.UPN)-1 {
		return ev
	}
	ev.TenantID = claims.TID
	ev.HostedDomain = strings.ToLower(claims.UPN[at+1:])
	return ev
}
