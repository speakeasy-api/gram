package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
)

// A handoff ticket is separate from OAuth state and never authenticates a user.
// Only the confirmed record, keyed by the original challenge, holds a real
// dashboard session reference. That credential stays in the server-side cache.
type consentSessionHandoff struct {
	ID           string `json:"id"`
	ChallengeID  string `json:"challenge_id"`
	CSRFToken    string `json:"csrf_token,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
}

func (h consentSessionHandoff) CacheKey() string   { return "consentSession:" + h.ID }
func (h consentSessionHandoff) TTL() time.Duration { return 5 * time.Minute }

// This browser security flow must not send users or handoff tickets over plain
// HTTP. Preserve the existing literal-loopback exception for local development;
// deployment configuration alone must not make a remote HTTP link acceptable.
func validateConsentSessionURL(u *url.URL) error {
	if u == nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("consent URL must be absolute and omit userinfo, query and fragment")
	}
	if _, err := requestorigin.CanonicalHost(u.Host); err != nil {
		return fmt.Errorf("invalid consent URL host: %w", err)
	}
	if u.Scheme != "https" && !cimd.IsLoopbackRedirectURI(u) {
		return fmt.Errorf("consent URL requires HTTPS except on loopback")
	}
	return nil
}

// CanonicalHost validates and case-folds the hostname but discards the port.
// Keep the effective port here: only the scheme's default is interchangeable
// with omission. Never consult forwarded headers for this security boundary.
func consentSessionAuthority(rawHost, scheme string) (string, error) {
	host, err := requestorigin.CanonicalHost(rawHost)
	if err != nil {
		return "", fmt.Errorf("invalid consent authority: %w", err)
	}
	u := &url.URL{Host: rawHost}
	port := u.Port()
	if port == "" {
		switch scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return "", fmt.Errorf("invalid consent authority scheme")
		}
	} else {
		// CanonicalHost has already checked the numeric port and its range.
		value, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return "", fmt.Errorf("invalid consent authority port: %w", err)
		}
		port = strconv.FormatUint(value, 10)
	}
	return net.JoinHostPort(host, port), nil
}

func checkConsentSessionOrigin(r *http.Request, scheme string) error {
	host, err := consentSessionAuthority(r.Host, scheme)
	if err != nil {
		return err
	}
	// The standard library compares Host strings literally in its Origin
	// fallback. Normalize a clone so default ports and DNS case agree, without
	// mutating the actual request or bypassing Sec-Fetch-Site checks.
	checked := r.Clone(r.Context())
	checked.Host = host
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != scheme || u.Path != "" {
			return fmt.Errorf("invalid consent request origin")
		}
		if err := validateConsentSessionURL(u); err != nil {
			return err
		}
		authority, err := consentSessionAuthority(u.Host, u.Scheme)
		if err != nil || authority != host {
			return fmt.Errorf("consent request origin does not match host")
		}
		checked.Header.Set("Origin", u.Scheme+"://"+authority)
	}
	if err := http.NewCrossOriginProtection().Check(checked); err != nil {
		return fmt.Errorf("check consent request origin: %w", err)
	}
	return nil
}

func (s *Service) validateConsentSessionURLs() error {
	for _, u := range []*url.URL{s.serverURL, s.siteURL} {
		if err := validateConsentSessionURL(u); err != nil {
			return oops.E(oops.CodeUnavailable, err, "account confirmation is unavailable")
		}
	}
	return nil
}

// The cached mint origin can differ from the canonical dashboard origin. Check
// the constructed return URL too; its state query is required, not URL config.
func (s *Service) consentSessionReturnURL(endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (string, error) {
	target, err := endpoint.ConsentURL(state.mintOriginOr(s.serverURL.String()), state.ID)
	if err != nil {
		return "", fmt.Errorf("build consent session return URL: %w", err)
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("parse consent session return URL: %w", err)
	}
	u.RawQuery, u.ForceQuery = "", false
	if err := validateConsentSessionURL(u); err != nil {
		return "", oops.E(oops.CodeUnavailable, err, "account confirmation return URL is unavailable")
	}
	return target, nil
}

func (s *Service) startConsentSessionHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	if err := s.validateConsentSessionURLs(); err != nil {
		return err
	}
	if _, err := s.consentSessionReturnURL(endpoint, state); err != nil {
		return err
	}
	if enabled, _, _ := s.agentAuthorizationRollout(r.Context(), endpoint.LogWith(s.logger), endpoint); !enabled {
		return oops.C(oops.CodeNotFound)
	}
	secret, err := generateOpaqueToken()
	if err != nil {
		return err
	}
	ticket := consentSessionHandoff{ID: uuid.NewString(), ChallengeID: state.ID, CSRFToken: secret, SessionToken: ""}
	if err := s.consentSessionCache.Store(r.Context(), ticket); err != nil {
		return fmt.Errorf("store consent session handoff ticket: %w", err)
	}
	target := *s.serverURL
	target.Path = "/oauth/agent-consent-session"
	target.RawPath = ""
	target.RawQuery = url.Values{"ticket": {ticket.ID}}.Encode()
	target.Fragment = ""
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		URL string `json:"url"`
	}{URL: target.String()}); err != nil {
		return fmt.Errorf("encode consent session handoff URL: %w", err)
	}
	return nil
}

func consentSessionCSRF(ticket consentSessionHandoff, token string) string {
	mac := hmac.New(sha256.New, []byte(ticket.CSRFToken))
	_, _ = mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// HandleConsentSessionHandoff runs only on the canonical server origin where
// the dashboard cookie is available. GET is read-only; confirmation requires a
// same-origin POST with a CSRF proof bound to that exact authenticated session.
func (s *Service) HandleConsentSessionHandoff(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if err := s.validateConsentSessionURLs(); err != nil {
		return err
	}
	requestAuthority, requestErr := consentSessionAuthority(r.Host, s.serverURL.Scheme)
	serverAuthority, serverErr := consentSessionAuthority(s.serverURL.Host, s.serverURL.Scheme)
	if requestErr != nil || serverErr != nil || requestAuthority != serverAuthority {
		return oops.C(oops.CodeNotFound)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return oops.C(oops.CodeBadRequest)
	}
	if err := r.ParseForm(); err != nil {
		return oops.C(oops.CodeBadRequest)
	}
	ticketID := r.URL.Query().Get("ticket")
	ticket, err := s.consentSessionCache.Get(r.Context(), "consentSession:"+ticketID)
	if err != nil || ticket.SessionToken != "" || ticket.CSRFToken == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	state, err := s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+ticket.ChallengeID)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(r.Context(), state.Endpoint)
	if err != nil {
		return err
	}
	if err := endpoint.ValidateGlobalChallenge(r.Context(), s.db, state.Endpoint, state.UserSessionIssuerID); err != nil {
		return oauthAuthorityError(err)
	}
	// Validate before consuming the ticket or persisting any session reference.
	target, err := s.consentSessionReturnURL(endpoint, state)
	if err != nil {
		return err
	}
	if state.AgentAuthorizationTarget == nil || !state.AgentAuthorizationTarget.matches(endpoint) {
		return oops.C(oops.CodeForbidden)
	}
	if _, err := s.loadConsentHuman(r.Context(), state, *state.AgentAuthorizationTarget); err != nil {
		return oops.E(oops.CodeForbidden, err, "consent authorizer is not eligible")
	}
	cookie, err := r.Cookie(constants.SessionCookie)
	token := ""
	if err == nil {
		token = cookie.Value
	}
	ctx, authErr := s.sessions.Authenticate(r.Context(), token)
	auth, ok := contextvalues.GetAuthContext(ctx)
	ready := token != "" && authErr == nil && ok && auth != nil && contextvalues.HasValidatedGramSession(ctx) && auth.UserID == state.AuthorizerUserID && auth.ActiveOrganizationID == endpoint.OrganizationID && !contextvalues.IsSupportSession(ctx) && !contextvalues.IsLegacyImpersonatedSession(ctx)
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := consentSessionTemplate.Execute(w, struct {
			Ready               bool
			CSRF, SiteURL, Slug string
			Styles              template.CSS
		}{Ready: ready, CSRF: consentSessionCSRF(ticket, token), SiteURL: s.siteURL.String(), Slug: endpoint.Slug, Styles: consentPageStyles}); err != nil {
			return fmt.Errorf("render consent session confirmation: %w", err)
		}
		return nil
	}
	if !ready {
		return oops.C(oops.CodeForbidden)
	}
	if err := checkConsentSessionOrigin(r, s.serverURL.Scheme); err != nil {
		return oops.C(oops.CodeForbidden)
	}
	if !hmac.Equal([]byte(r.PostForm.Get("csrf_token")), []byte(consentSessionCSRF(ticket, token))) {
		return oops.C(oops.CodeUnauthorized)
	}
	// Elect one confirmation winner. Replays cannot relink the browser session.
	consumed, err := s.consentSessionCache.GetAndDelete(r.Context(), ticket.CacheKey())
	if err != nil || consumed != ticket {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.consentSessionCache.Store(r.Context(), consentSessionHandoff{ID: state.ID, ChallengeID: state.ID, CSRFToken: "", SessionToken: token}); err != nil {
		return fmt.Errorf("store confirmed consent session: %w", err)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return nil
}

var consentSessionTemplate = template.Must(template.Must(template.New("consent-session").Parse(`<!doctype html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>Confirm Gram account</title><style>{{.Styles}}</style></head>
<body class="bg-background text-foreground min-h-screen"><main class="mx-auto flex min-h-screen w-full max-w-[52rem] flex-col gap-6 px-4 py-12 sm:py-16">
<div class="text-foreground">{{template "speakeasyWordmark"}}</div><h1 class="text-display-xs">Confirm Gram account</h1>
{{if .Ready}}<p>Use your signed-in Gram account to manage agent connections while authorizing {{.Slug}}. This does not attach an account or approve the OAuth request.</p>
<form method="post"><input type="hidden" name="csrf_token" value="{{.CSRF}}"><button class="bg-card interact:bg-accent h-10 border px-4 text-sm" type="submit">Continue to agent connections</button></form>
{{else}}<p>Sign in to Gram as the consent authorizer and select this organization. Then return here and reload this page.</p><a href="{{.SiteURL}}" target="_blank" rel="noopener noreferrer">Open Gram to sign in</a>{{end}}
</main></body></html>`)).Parse(consentLogoHTML))
