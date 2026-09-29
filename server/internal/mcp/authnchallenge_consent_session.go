package mcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

func (s *Service) startConsentSessionHandoff(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	if enabled, _, _ := s.agentAuthorizationRollout(r.Context(), endpoint.LogWith(s.logger), endpoint); !enabled {
		return oops.C(oops.CodeNotFound)
	}
	secret, err := generateOpaqueToken()
	if err != nil {
		return err
	}
	ticket := consentSessionHandoff{ID: uuid.NewString(), ChallengeID: state.ID, CSRFToken: secret, SessionToken: ""}
	if err := s.consentSessionCache.Store(r.Context(), ticket); err != nil {
		return err
	}
	target := *s.serverURL
	target.Path = "/oauth/agent-consent-session"
	target.RawPath = ""
	target.RawQuery = url.Values{"ticket": {ticket.ID}}.Encode()
	target.Fragment = ""
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(struct {
		URL string `json:"url"`
	}{URL: target.String()})
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
	if r.Host != s.serverURL.Host {
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
		return consentSessionTemplate.Execute(w, struct {
			Ready               bool
			CSRF, SiteURL, Slug string
			Styles              template.CSS
		}{Ready: ready, CSRF: consentSessionCSRF(ticket, token), SiteURL: s.siteURL.String(), Slug: endpoint.Slug, Styles: consentPageStyles})
	}
	if !ready {
		return oops.C(oops.CodeForbidden)
	}
	if err := http.NewCrossOriginProtection().Check(r); err != nil {
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
		return err
	}
	target, err := endpoint.ConsentURL(state.mintOriginOr(s.serverURL.String()), state.ID)
	if err != nil {
		return err
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
