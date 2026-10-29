package adminmcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

const staffChallengePrefix = "adminMCPChallenge:"
const staffBrowserProofCookie = "__Host-admin-mcp-proof"
const staffCodeLifetime = 10 * time.Minute
const staffAuthorizationLifetime = 24 * time.Hour

type staffChallenge struct {
	ID            string    `json:"id"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	State         string    `json:"state"`
	CodeChallenge string    `json:"code_challenge"`
	CSRFToken     string    `json:"csrf_token"`
	BrowserProof  string    `json:"browser_proof"`
	SessionHash   string    `json:"session_hash"`
	ResourceURI   string    `json:"resource_uri"`
	Scopes        []string  `json:"scopes"`
	CreatedAt     time.Time `json:"created_at"`
}

func (c staffChallenge) CacheKey() string              { return staffChallengePrefix + c.ID }
func (c staffChallenge) AdditionalCacheKeys() []string { return []string{} }
func (c staffChallenge) TTL() time.Duration            { return staffCodeLifetime }

var _ cache.CacheableObject[staffChallenge] = (*staffChallenge)(nil)

type staffAuthorizationStore interface {
	Authorize(context.Context, staffAuthorization) error
}

type staffAuthorization struct {
	Subject       string
	ClientID      string
	SessionEnc    string
	ResourceURI   string
	Scopes        []string
	CodeHash      string
	CodeChallenge string
	RedirectURI   string
	ExpiresAt     time.Time
	GrantExpires  time.Time
}

// StaffOAuthAuthorization runs the browser-only, staff-verified consent flow.
// Its handlers must be mounted only behind the tailnet ingress.
type StaffOAuthAuthorization struct {
	clients  staffClientStore
	store    staffAuthorizationStore
	cache    cache.TypedCacheObject[staffChallenge]
	verifier adminSessionVerifier
	cipher   *encryption.Client
	resource string
	writes   WriteConfig
}

func NewStaffOAuthAuthorization(clients staffClientStore, store staffAuthorizationStore, challengeCache cache.Cache, verifier adminSessionVerifier, cipher *encryption.Client, resource string) *StaffOAuthAuthorization {
	return &StaffOAuthAuthorization{
		clients: clients, store: store, cache: cache.NewTypedObjectCache[staffChallenge](nil, challengeCache, cache.SuffixNone),
		verifier: verifier, cipher: cipher, resource: resource, writes: WriteConfig{Enabled: false, Operations: nil},
	}
}

func (s *StaffOAuthAuthorization) AuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			staffBrowserError(w, r, http.StatusMethodNotAllowed, oautherr.CodeInvalidRequest, "method not allowed", oauthui.ReasonInvalidLink)
			return
		}
		if !s.ready() {
			staffBrowserError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "authorization unavailable", oauthui.ReasonDefault)
			return
		}
		request := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
		if err := request.ValidateRedirectableFields(); err != nil {
			staffBrowserRequestError(w, r, err)
			return
		}
		if err := oauthwire.ValidateRedirectURI(request.RedirectURI); err != nil {
			staffBrowserRequestError(w, r, err)
			return
		}
		client, err := s.clients.GetClient(r.Context(), request.ClientID)
		if err != nil || !slices.Contains(client.RedirectURIs, request.RedirectURI) {
			staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidClient, "unknown client_id or redirect_uri", oauthui.ReasonInvalidClient)
			return
		}
		if err := request.ValidatePostRedirect(); err != nil {
			staffPreAuthenticationError(w, r, client, request.RedirectURI, request.State, err)
			return
		}
		if err := oauthwire.ValidateResourceIndicators(request.Resources, s.resource); err != nil {
			staffPreAuthenticationError(w, r, client, request.RedirectURI, request.State, err)
			return
		}
		if !validStaffPKCEChallenge(request.CodeChallenge) {
			staffPreAuthenticationError(w, r, client, request.RedirectURI, request.State, &oauthwire.Error{Code: "invalid_request", Description: "invalid PKCE S256 challenge"})
			return
		}
		scopes, ok := normalizeRequestedScopes(r.URL.Query().Get("scope"), s.writes.WritesAvailable())
		if !ok {
			staffPreAuthenticationError(w, r, client, request.RedirectURI, request.State, &oauthwire.Error{Code: "invalid_scope", Description: "requested scope is not available"})
			return
		}
		csrf, err := staffOpaqueToken()
		if err != nil {
			staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not start authorization", oauthui.ReasonDefault)
			return
		}
		proof, err := staffOpaqueToken()
		if err != nil {
			staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not start authorization", oauthui.ReasonDefault)
			return
		}
		challenge := staffChallenge{ID: uuid.NewString(), ClientID: client.ID, RedirectURI: request.RedirectURI, State: request.State, CodeChallenge: request.CodeChallenge, CSRFToken: csrf, BrowserProof: staffTokenHash(proof), SessionHash: "", ResourceURI: s.resource, Scopes: scopes, CreatedAt: time.Now()}
		if err := s.cache.Store(r.Context(), challenge); err != nil {
			staffBrowserError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "could not start authorization", oauthui.ReasonDefault)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: staffBrowserProofCookie + "-" + challenge.ID, Value: proof, Path: "/", MaxAge: int(staffCodeLifetime.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:exhaustruct // Host-only session proof; Domain and Expires intentionally omitted.
		http.Redirect(w, r, Path+"/connect?state="+url.QueryEscape(challenge.ID), http.StatusFound)
	})
}

func (s *StaffOAuthAuthorization) ConnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		if !s.ready() {
			staffBrowserError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "authorization unavailable", oauthui.ReasonDefault)
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.connectGet(w, r)
		case http.MethodPost:
			s.connectPost(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			staffBrowserError(w, r, http.StatusMethodNotAllowed, oautherr.CodeInvalidRequest, "method not allowed", oauthui.ReasonInvalidLink)
		}
	})
}

func (s *StaffOAuthAuthorization) connectGet(w http.ResponseWriter, r *http.Request) {
	challenge, err := s.cache.Get(r.Context(), staffChallengePrefix+r.URL.Query().Get("state"))
	if reason := staffChallengeErrorReason(challenge, err, s.resource); reason != oauthui.ReasonDefault {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or expired", reason)
		return
	}
	if !staffBrowserProofMatches(r, challenge) {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization browser is invalid", oauthui.ReasonSessionChanged)
		return
	}
	client, err := s.clients.GetClient(r.Context(), challenge.ClientID)
	if err != nil || !slices.Contains(client.RedirectURIs, challenge.RedirectURI) {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidClient, "authorization client is unavailable", oauthui.ReasonInvalidClient)
		return
	}
	_, sessionID, err := s.staffSession(r)
	if err != nil {
		var authErr *oops.ShareableError
		if !errors.Is(err, errMissingStaffCookie) && (!errors.As(err, &authErr) || authErr.Code != oops.CodeUnauthorized) {
			staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeAccessDenied, "staff login is required", oauthui.ReasonDefault)
			return
		}
		// A logged-out session can leave a path-scoped browser cookie behind.
		// Clear it before returning to normal staff login, without touching /admin.
		if !errors.Is(err, errMissingStaffCookie) {
			http.SetCookie(w, &http.Cookie{Name: constants.AdminSessionCookie, Path: Path, MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}) //nolint:exhaustruct // Only the scoped cookie is expired.
		}
		// The relative return target is server-owned and contains only the opaque
		// challenge ID, never an untrusted redirect URI or client-supplied URL.
		returnTo := Path + "/connect?state=" + url.QueryEscape(challenge.ID)
		http.Redirect(w, r, "/admin/auth.login?return_to="+url.QueryEscape(returnTo), http.StatusFound)
		return
	}
	challenge.SessionHash = staffTokenHash(sessionID)
	if err := s.cache.Store(r.Context(), challenge); err != nil {
		staffBrowserError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "could not prepare authorization", oauthui.ReasonDefault)
		return
	}
	view := staffConsentView{
		ClientName:  client.Name,
		RedirectURI: challenge.RedirectURI,
		State:       challenge.ID,
		CSRF:        challenge.CSRFToken,
		Write:       slices.Contains(challenge.Scopes, ScopeWrite),
	}
	var page strings.Builder
	if err := staffConsentPage.Execute(&page, view); err != nil {
		staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not render authorization page", oauthui.ReasonDefault)
		return
	}
	// Chrome also checks form-action on the 303 after consent. Only this
	// registered callback may receive the resulting browser navigation.
	callback, err := url.Parse(challenge.RedirectURI)
	if err != nil {
		staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not render authorization page", oauthui.ReasonDefault)
		return
	}
	callbackSource := callback.Scheme + ":"
	if callback.Host != "" {
		callbackSource = callback.Scheme + "://" + callback.Host
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' "+callbackSource+"; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page.String()))
}

// staffConsentView is the data rendered on the staff consent page.
type staffConsentView struct {
	// ClientName is the registered MCP client's display name.
	ClientName string
	// RedirectURI is the registered callback that receives the authorization code.
	RedirectURI string
	// State is the opaque challenge ID posted back on approve or deny.
	State string
	// CSRF binds the form post to this challenge.
	CSRF string
	// Write is true when the client requested admin:write.
	Write bool
}

var staffConsentPage = template.Must(template.New("staff-consent").Parse(`<!doctype html>
<html>
<head><title>Connect Staff Admin MCP</title></head>
<body>
<h1>Connect Staff Admin MCP</h1>
<p>{{.ClientName}} requests access to staff administration. The callback is {{.RedirectURI}}.</p>
{{if .Write}}
<p><strong>This client also requests admin:write.</strong> It may prepare staff changes. Each change still needs your separate approval in this admin site before it runs.</p>
{{else}}
<p>Access is read-only.</p>
{{end}}
<form method="post" action="/admin-mcp/connect">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="csrf_token" value="{{.CSRF}}">
<button type="submit" name="action" value="approve">Approve</button>
<button type="submit" name="action" value="deny">Deny</button>
</form>
</body>
</html>`))

func (s *StaffOAuthAuthorization) connectPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		staffBrowserError(w, r, http.StatusBadRequest, "invalid_request", "could not parse authorization", oauthui.ReasonDefault)
		return
	}
	challenge, err := s.cache.GetAndDelete(r.Context(), staffChallengePrefix+r.PostForm.Get("state"))
	if reason := staffChallengeErrorReason(challenge, err, s.resource); reason != oauthui.ReasonDefault {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or expired", reason)
		return
	}
	if !staffBrowserProofMatches(r, challenge) {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization browser is invalid", oauthui.ReasonSessionChanged)
		return
	}
	if challenge.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challenge.CSRFToken)) != 1 {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization confirmation is invalid", oauthui.ReasonSessionChanged)
		return
	}
	client, err := s.clients.GetClient(r.Context(), challenge.ClientID)
	if err != nil || !slices.Contains(client.RedirectURIs, challenge.RedirectURI) {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidClient, "authorization client is unavailable", oauthui.ReasonInvalidClient)
		return
	}
	staff, sessionID, err := s.staffSession(r)
	if err != nil {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeAccessDenied, "staff login is required", oauthui.ReasonDefault)
		return
	}
	if challenge.SessionHash == "" || subtle.ConstantTimeCompare([]byte(challenge.SessionHash), []byte(staffTokenHash(sessionID))) != 1 {
		staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeAccessDenied, "staff session changed during authorization", oauthui.ReasonSessionChanged)
		return
	}
	if r.PostForm.Get("action") != "approve" {
		staffRedirectError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "authorization denied"})
		return
	}
	code, err := staffOpaqueToken()
	if err != nil {
		staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not complete authorization", oauthui.ReasonDefault)
		return
	}
	encryptedSession, err := s.cipher.Encrypt([]byte(sessionID))
	if err != nil {
		staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not complete authorization", oauthui.ReasonDefault)
		return
	}
	// Revalidate against the current switch: writes may have been turned off
	// between authorize and consent.
	scopes, ok := normalizeRequestedScopes(strings.Join(challenge.Scopes, " "), s.writes.WritesAvailable())
	if !ok {
		staffRedirectError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "invalid_scope", Description: "requested scope is not available"})
		return
	}
	now := time.Now()
	input := staffAuthorization{
		Subject: urn.NewUserSubject(staff.OIDCSubject).String(), ClientID: challenge.ClientID,
		SessionEnc: encryptedSession, ResourceURI: s.resource, Scopes: scopes,
		CodeHash: staffTokenHash(code), CodeChallenge: challenge.CodeChallenge, RedirectURI: challenge.RedirectURI,
		ExpiresAt: now.Add(staffAuthorizationLifetime), GrantExpires: now.Add(staffCodeLifetime),
	}
	if err := s.store.Authorize(r.Context(), input); err != nil {
		if errors.Is(err, errStaffGrant) {
			staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidClient, "authorization client is unavailable", oauthui.ReasonInvalidClient)
		} else {
			staffBrowserError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "could not complete authorization", oauthui.ReasonDefault)
		}
		return
	}
	callback, err := url.Parse(challenge.RedirectURI)
	if err != nil {
		staffBrowserError(w, r, http.StatusInternalServerError, "server_error", "could not complete authorization", oauthui.ReasonDefault)
		return
	}
	query := callback.Query()
	query.Set("code", code)
	if challenge.State != "" {
		query.Set("state", challenge.State)
	}
	callback.RawQuery = query.Encode()
	http.Redirect(w, r, callback.String(), http.StatusSeeOther)
}

func staffBrowserProofMatches(r *http.Request, challenge staffChallenge) bool {
	cookie, err := r.Cookie(staffBrowserProofCookie + "-" + challenge.ID)
	return err == nil && cookie.Value != "" && challenge.BrowserProof != "" &&
		subtle.ConstantTimeCompare([]byte(staffTokenHash(cookie.Value)), []byte(challenge.BrowserProof)) == 1
}

var errMissingStaffCookie = errors.New("missing admin session cookie")

// errStaffIdentityMismatch identifies a verified session with an unusable staff identity.
var errStaffIdentityMismatch = errors.New("staff session identity mismatch")

func (s *StaffOAuthAuthorization) staffSession(r *http.Request) (*contextvalues.AdminAuthContext, string, error) {
	cookie, err := r.Cookie(constants.AdminSessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, "", errMissingStaffCookie
	}
	ctx, err := s.verifier.Authorize(r.Context(), cookie.Value, &security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}) //nolint:exhaustruct // Only name is used.
	if err != nil {
		return nil, "", fmt.Errorf("verify staff browser session: %w", err)
	}
	staff, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok || staff == nil || staff.SessionID != cookie.Value || staff.OIDCSubject == "" || staff.Email == "" {
		return nil, "", errStaffIdentityMismatch
	}
	return staff, cookie.Value, nil
}

func (s *StaffOAuthAuthorization) ready() bool {
	return s != nil && s.clients != nil && s.store != nil && s.verifier != nil && s.cipher != nil && s.resource != ""
}

// staffChallengeErrorReason distinguishes proven expiry from invalid links and
// cache outages without changing the authorization failure's wire contract.
func staffChallengeErrorReason(challenge staffChallenge, err error, resource string) oauthui.Reason {
	switch {
	case err != nil && !errors.Is(err, redisCache.ErrCacheMiss):
		return oauthui.ReasonUnavailable
	case err != nil || challenge.ID == "" || challenge.ResourceURI != resource:
		return oauthui.ReasonInvalidLink
	case time.Since(challenge.CreatedAt) >= staffCodeLifetime:
		return oauthui.ReasonExpired
	default:
		return oauthui.ReasonDefault
	}
}

func staffPreAuthenticationError(w http.ResponseWriter, r *http.Request, client staffOAuthClient, redirectURI, state string, err error) {
	callback, oauthErr, buildErr := staffErrorCallback(redirectURI, state, err)
	if buildErr != nil {
		staffBrowserError(w, r, http.StatusBadRequest, oauthErr.Code, oauthErr.Description, oauthui.ReasonDefault)
		return
	}
	if oauthwire.MayAutoRedirectBeforeAuthentication(redirectURI) {
		http.Redirect(w, r, callback, http.StatusSeeOther)
		return
	}
	page := oauthui.AuthorizationError{
		Recovery:       nil,
		Setup:          false,
		Status:         http.StatusBadRequest,
		Reason:         oauthui.ReasonDefault,
		Approval:       false,
		ErrorID:        "",
		CallbackURL:    callback,
		Code:           oauthErr.Code,
		Description:    oauthErr.Description,
		ClientName:     client.Name,
		ClientID:       client.ID,
		ClientIDOrigin: "",
		ServerName:     "Speakeasy Admin",
	}
	_ = oauthui.WriteAuthorizationError(r.Context(), w, page)
}

func staffRedirectError(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	callback, oauthErr, buildErr := staffErrorCallback(redirectURI, state, err)
	if buildErr != nil {
		staffBrowserError(w, r, http.StatusBadRequest, oauthErr.Code, oauthErr.Description, oauthui.ReasonDefault)
		return
	}
	http.Redirect(w, r, callback, http.StatusSeeOther)
}

func staffErrorCallback(redirectURI, state string, err error) (string, *oauthwire.Error, error) {
	oauthErr, ok := errors.AsType[*oauthwire.Error](err)
	if !ok {
		oauthErr = &oauthwire.Error{Code: oautherr.CodeInvalidRequest, Description: "invalid request"}
	}
	callback, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		return "", oauthErr, fmt.Errorf("parse authorization error callback: %w", parseErr)
	}
	query := callback.Query()
	query.Set(oauthwire.ParamError, oauthErr.Code)
	query.Set(oauthwire.ParamErrorDescription, oauthErr.Description)
	if state != "" {
		query.Set(oauthwire.ParamState, state)
	}
	callback.RawQuery = query.Encode()
	return callback.String(), oauthErr, nil
}

func validStaffPKCEChallenge(challenge string) bool {
	if len(challenge) != 43 {
		return false
	}
	for _, c := range challenge {
		if c < 'A' || c > 'Z' {
			if c < 'a' || c > 'z' {
				if c < '0' || c > '9' {
					if c != '-' && c != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}

func staffBrowserError(w http.ResponseWriter, r *http.Request, status int, code, description string, reason oauthui.Reason) {
	_ = oauthui.WriteAuthorizationError(r.Context(), w, oauthui.AuthorizationError{
		Recovery:       nil,
		Setup:          false,
		Status:         status,
		Reason:         reason,
		Approval:       false,
		ErrorID:        "",
		Code:           code,
		Description:    description,
		CallbackURL:    "",
		ClientName:     "",
		ClientID:       "",
		ClientIDOrigin: "",
		ServerName:     "Speakeasy Admin",
	})
}

func staffBrowserRequestError(w http.ResponseWriter, r *http.Request, err error) {
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		staffBrowserError(w, r, http.StatusBadRequest, oauthErr.Code, oauthErr.Description, oauthui.ReasonInvalidLink)
		return
	}
	staffBrowserError(w, r, http.StatusBadRequest, oautherr.CodeInvalidRequest, "invalid request", oauthui.ReasonInvalidLink)
}
