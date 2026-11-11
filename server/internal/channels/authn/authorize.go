package authn

import (
	"crypto/subtle"
	_ "embed"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
)

var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

//go:embed consent.html
var consentHTML string

var consentTemplate = template.Must(template.Must(template.New("consent").Parse(consentHTML)).Parse(consentpage.LogoTemplate))

func (s *Service) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, v := range q {
		if key != "resource" && len(v) != 1 {
			oauthError(w, 400, "invalid_request")
			return
		}
	}
	client, err := s.resolveClient(r.Context(), q.Get("client_id"), true)
	if err != nil {
		oauthError(w, 400, "invalid_client")
		return
	}
	redirect := q.Get("redirect_uri")
	// Never redirect an invalid request to an unregistered URI.
	if !slices.Contains(client.RedirectUris, redirect) {
		oauthError(w, 400, "invalid_request")
		return
	}
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || !pkceChallenge.MatchString(q.Get("code_challenge")) {
		oauthError(w, 400, "invalid_request")
		return
	}
	target, targetErr := s.target(r.Context(), q.Get("resource"))
	if targetErr != nil || oauthwire.ValidateResourceIndicators(oauthwire.ResourceIndicatorsFrom(q), target.Resource) != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	if q.Get("scope") != "" && q.Get("scope") != Scope {
		oauthError(w, 400, "invalid_scope")
		return
	}
	r = r.WithContext(WithTarget(r.Context(), target))
	ctx, identity, err := s.browser(r)
	if err != nil {
		if !identity.Principal.IsZero() {
			oauthError(w, http.StatusForbidden, "access_denied")
			return
		}
		// A relative target is safe when dashboard and API origins differ.
		target := "/channels/oauth/authorize?" + q.Encode()
		http.Redirect(w, r, "/rpc/auth.login?redirect="+url.QueryEscape(target), http.StatusSeeOther)
		return
	}
	userEmail := ""
	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth.Email != nil {
		userEmail = *auth.Email
	}
	proof := randomToken()
	csrf := randomToken()
	challenge := randomToken()
	binding := browserBinding(proof, identity)
	err = repo.New(s.config.DB).CreateChallenge(r.Context(), repo.CreateChallengeParams{ID: challenge, OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, Principal: identity.Principal.String(), Resource: target.Resource, BrowserHash: binding, CsrfHash: digest(csrf), ClientID: pgtype.Text{String: client.ClientID, Valid: true}, RedirectUri: redirect, State: q.Get("state"), CodeChallenge: q.Get("code_challenge"), ExpiresAt: timestamp(time.Now().Add(10 * time.Minute))})
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	//nolint:exhaustruct // Optional cookie fields must remain unset for this host-only browser proof.
	http.SetCookie(w, &http.Cookie{Name: proofCookie, Value: proof, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// As on MCP consent, do not restrict form-action: Chrome also applies it
	// to the POST redirect to registered web, loopback, and native-app callbacks.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	_ = consentTemplate.Execute(w, struct {
		Styles                                                                         template.CSS
		Name, RedirectURI, Organization, Project, Resource, UserEmail, Challenge, CSRF string
	}{consentpage.Styles, client.ClientName, redirect, target.OrganizationSlug, target.ProjectSlug, target.Resource, userEmail, challenge, csrf})
}
func (s *Service) consent(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	// Login may return through the configured dashboard proxy. No request Host
	// or arbitrary Origin can add another consent origin.
	if origin == "" || (origin != s.origin && origin != s.dashboardOrigin) || !parseForm(w, r) {
		oauthError(w, 403, "invalid_request")
		return
	}
	target, targetErr := s.target(r.Context(), r.PostForm.Get("resource"))
	if targetErr != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	r = r.WithContext(WithTarget(r.Context(), target))
	ctx, identity, err := s.browser(r)
	if err != nil {
		oauthError(w, 403, "access_denied")
		return
	}
	cookie, err := r.Cookie(proofCookie)
	if err != nil || cookie.Value == "" {
		oauthError(w, 403, "invalid_request")
		return
	}
	binding := browserBinding(cookie.Value, identity)
	tx, err := s.config.DB.Begin(ctx)
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := repo.New(tx)
	challenge, err := queries.ConsumeChallenge(ctx, repo.ConsumeChallengeParams{ID: r.PostForm.Get("challenge"), OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, BrowserHash: binding, CsrfHash: digest(r.PostForm.Get("csrf"))})
	if err != nil {
		oauthError(w, 403, "invalid_request")
		return
	}
	if challenge.Principal != identity.Principal.String() || challenge.Resource != target.Resource {
		oauthError(w, 403, "invalid_request")
		return
	}
	client, err := queries.GetClient(ctx, challenge.ClientID.String)
	if err != nil || !slices.Contains(client.RedirectUris, challenge.RedirectUri) {
		oauthError(w, 400, "invalid_client")
		return
	}
	redirectTarget, err := url.Parse(challenge.RedirectUri)
	if err != nil {
		oauthError(w, 400, "invalid_request")
		return
	}
	values := redirectTarget.Query()
	values.Set("state", challenge.State)
	decision := r.PostForm.Get("decision")
	switch decision {
	case "deny":
		values.Set("error", "access_denied")
	case "allow":
		if err = queries.LockIdentity(ctx, digest(identity.OrganizationID+"\x00"+identity.ProjectID.String()+"\x00"+identity.Principal.String()+"\x00"+challenge.ClientID.String)); err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		if err = queries.RevokePriorGrants(ctx, repo.RevokePriorGrantsParams{OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, Principal: identity.Principal.String(), ClientID: challenge.ClientID.String}); err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		grantID := uuid.New()
		code := randomToken()
		_, err = queries.CreateGrant(ctx, repo.CreateGrantParams{ID: grantID, ClientID: challenge.ClientID.String, OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, Resource: challenge.Resource, Principal: identity.Principal.String(), ExpiresAt: timestamp(time.Now().Add(grantLifetime))})
		if err == nil {
			err = queries.CreateCredential(ctx, repo.CreateCredentialParams{OrganizationID: identity.OrganizationID, ProjectID: identity.ProjectID, TokenHash: digest(code), Kind: "code", GrantID: grantID, RedirectUri: challenge.RedirectUri, CodeChallenge: challenge.CodeChallenge, ExpiresAt: timestamp(time.Now().Add(5 * time.Minute))})
		}
		if err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		values.Set("code", code)
	default:
		oauthError(w, 400, "invalid_request")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	redirectTarget.RawQuery = values.Encode()
	http.Redirect(w, r, redirectTarget.String(), http.StatusSeeOther)
}
func validVerifier(verifier, challenge string) bool {
	return pkceVerifier.MatchString(verifier) && subtle.ConstantTimeCompare([]byte(digest(verifier)), []byte(challenge)) == 1
}

func browserBinding(proof string, id Identity) string {
	return digest(proof + "\x00" + id.Principal.String() + "\x00" + id.BrowserOrganizationID + "\x00" + id.BrowserSessionID + "\x00" + id.OrganizationID + "\x00" + id.ProjectID.String())
}
