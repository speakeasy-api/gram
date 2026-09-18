package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/assertion/idjag"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// FederatedChallenge is server-side, short-lived state, never an identity or a
// retained credential. The upstream verifier is unrelated to downstream PKCE.
type FederatedChallenge struct {
	OrganizationID string    `json:"organization_id"`
	IssuerID       uuid.UUID `json:"issuer_id"`
	ClientID       uuid.UUID `json:"client_id"`
	Configuration  string    `json:"configuration"`
	CallbackURL    string    `json:"callback_url"`
	Nonce          string    `json:"nonce"`
	Verifier       string    `json:"verifier"`
	BrowserHash    string    `json:"browser_hash,omitempty"`
	// Only set after verified OIDC identity and organization authorization. No
	// credential is stored here: the minimal login handoff precedes this step.
	ExplicitRetry     bool                              `json:"explicit_retry,omitempty"`
	OfflineRequested  bool                              `json:"offline_requested,omitempty"`
	ConfigurationHash string                            `json:"offline_configuration_hash,omitempty"`
	ValidatedUserID   string                            `json:"validated_user_id,omitempty"`
	ValidatedIdentity *remotesessions.FederatedIdentity `json:"validated_identity,omitempty"`
}

// FederatedConsentBinding survives removal of the ephemeral OIDC secrets.
type FederatedConsentBinding struct {
	IssuerID uuid.UUID `json:"issuer_id"`
	ClientID uuid.UUID `json:"client_id"`
}

func (b *FederatedConsentBinding) matches(issuerID, clientID uuid.UUID) bool {
	return b != nil && b.IssuerID != uuid.Nil && b.ClientID != uuid.Nil && b.IssuerID == issuerID && b.ClientID == clientID
}

func (s *Service) federatedProvider(ctx context.Context, endpoint *ResolvedMcpEndpoint) (*remotesessions.FederatedProvider, uuid.UUID, uuid.UUID, string, error) {
	row, err := usersessionsrepo.New(s.db).GetUserSessionIssuerByID(ctx, usersessionsrepo.GetUserSessionIssuerByIDParams{ID: endpoint.UserSessionIssuerID, ProjectID: endpoint.ProjectID, OrganizationID: endpoint.OrganizationID})
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, "", fmt.Errorf("resolve federated login provider: %w", err)
	}
	if !row.TrustedRemoteSessionClientID.Valid {
		return nil, uuid.Nil, uuid.Nil, "", nil
	}
	if row.ProjectID.Valid || !row.OrganizationID.Valid || row.OrganizationID.String != endpoint.OrganizationID || !row.TrustedRemoteSessionIssuerID.Valid || s.remoteChallengeMgr == nil {
		return nil, uuid.Nil, uuid.Nil, "", errors.New("invalid federated issuer configuration")
	}
	// Reject an unusable callback before discovery or any provider traffic. This
	// check is deliberately after the unlinked branch, preserving WorkOS HTTP dev.
	callback, err := endpoint.IDPCallbackURL(s.serverURL.String())
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, "", remotesessions.ErrFederatedConfiguration
	}
	if _, err := federatedCallbackURL(callback); err != nil {
		return nil, uuid.Nil, uuid.Nil, "", fmt.Errorf("resolve federated login provider: %w", err)
	}
	provider, err := s.remoteChallengeMgr.LoadFederatedProvider(ctx, endpoint.OrganizationID, row.TrustedRemoteSessionIssuerID.UUID, row.TrustedRemoteSessionClientID.UUID)
	if err != nil {
		return nil, uuid.Nil, uuid.Nil, "", fmt.Errorf("resolve federated login provider: %w", err)
	}
	version := provider.Fingerprint() + ":" + row.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
	return provider, row.TrustedRemoteSessionIssuerID.UUID, row.TrustedRemoteSessionClientID.UUID, version, nil
}

// prepareFederatedLogin leaves WorkOS entirely unchanged when no trusted client
// is linked. Federation first visits the callback origin to establish a host-only
// browser binding: custom-domain authorize pages cannot set that origin's cookie.
func (s *Service) prepareFederatedLogin(ctx context.Context, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState) (*url.URL, error) {
	return s.prepareBoundFederatedLogin(ctx, endpoint, state, "")
}

// retryHuman is supplied only by the validated, consumed consent retry action.
func (s *Service) prepareBoundFederatedLogin(ctx context.Context, endpoint *ResolvedMcpEndpoint, state *AuthnChallengeState, retryHuman string) (*url.URL, error) {
	provider, issuerID, clientID, version, err := s.federatedProvider(ctx, endpoint)
	if err != nil || provider == nil {
		return nil, err
	}
	if retryHuman != "" && !state.FederatedBinding.matches(issuerID, clientID) {
		return nil, remotesessions.ErrFederatedConfiguration
	}
	callback, err := endpoint.IDPCallbackURL(s.serverURL.String())
	if err != nil {
		return nil, err
	}
	target, err := federatedCallbackURL(callback)
	if err != nil {
		return nil, err
	}
	nonce, err := generateOpaqueToken()
	if err != nil {
		return nil, err
	}
	verifier, err := generateOpaqueToken()
	if err != nil {
		return nil, err
	}
	state.Federation = &FederatedChallenge{OrganizationID: endpoint.OrganizationID, IssuerID: issuerID, ClientID: clientID, Configuration: version, CallbackURL: callback, Nonce: nonce, Verifier: verifier, BrowserHash: "", ExplicitRetry: retryHuman != "", ValidatedUserID: retryHuman}
	if err := s.authnChallengeCache.Store(ctx, *state); err != nil {
		return nil, fmt.Errorf("store federated login challenge: %w", err)
	}
	target.RawQuery = url.Values{"state": {state.ID}, "federated_start": {"1"}}.Encode()
	return target, nil
}

func federationCookieName(id string) string { return "__Host-gram-federation-" + id }

func federatedBrowserCookie(id, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: federationCookieName(id), Value: value, Quoted: false,
		Path: "/", Domain: "", Expires: time.Time{}, RawExpires: "", MaxAge: maxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Partitioned: false, Raw: "", Unparsed: nil,
	}
}

// startFederatedLogin is entered only after atomically consuming the initial
// challenge. Rotating its ID means the bootstrap link cannot reveal or reuse the
// callback state, including when a link is opened in two browsers.
func (s *Service) startFederatedLogin(w http.ResponseWriter, r *http.Request, state *AuthnChallengeState, provider *remotesessions.FederatedProvider) error {
	browser, err := generateOpaqueToken()
	if err != nil {
		return err
	}
	state.ID = uuid.NewString()
	state.Federation.BrowserHash = sha256Hex(browser)
	var target *url.URL
	if s.federatedLoginConsumer != nil {
		target, err = provider.BuildAuthorizationURLWithOffline(state.Federation.CallbackURL, state.ID, state.Federation.Nonce, state.Federation.Verifier, state.Federation.OfflineRequested)
	} else {
		target, err = provider.BuildAuthorizationURL(state.Federation.CallbackURL, state.ID, state.Federation.Nonce, state.Federation.Verifier)
	}
	if err != nil {
		return fmt.Errorf("build federated authorization URL: %w", err)
	}
	if err := s.authnChallengeCache.Store(r.Context(), *state); err != nil {
		return fmt.Errorf("store bound federated login challenge: %w", err)
	}
	http.SetCookie(w, federatedBrowserCookie(state.ID, browser, int(state.TTL().Seconds())))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusFound)
	return nil
}

func validateFederatedBrowser(r *http.Request, state AuthnChallengeState) error {
	if state.Federation == nil || state.Federation.BrowserHash == "" || state.CreatedAt.IsZero() || time.Since(state.CreatedAt) > state.TTL() || state.CreatedAt.After(time.Now().Add(time.Minute)) {
		return errors.New("invalid federated challenge")
	}
	cookie, err := r.Cookie(federationCookieName(state.ID))
	if err != nil || subtle.ConstantTimeCompare([]byte(sha256Hex(cookieValue(cookie))), []byte(state.Federation.BrowserHash)) != 1 {
		return errors.New("federated browser binding failed")
	}
	return nil
}

func cookieValue(cookie *http.Cookie) string {
	if cookie == nil {
		return ""
	}
	return cookie.Value
}

func (s *Service) resolveFederatedHuman(ctx context.Context, endpoint *ResolvedMcpEndpoint, identity *remotesessions.FederatedIdentity) (string, error) {
	// The shared directory contract accepts an absent verification claim. An
	// explicit negative OIDC assertion must never be used as an email mapping.
	if strings.TrimSpace(identity.Email) == "" || (identity.EmailVerified != nil && !*identity.EmailVerified) {
		return "", oops.E(oops.CodeForbidden, nil, "Your account is not provisioned for this organization. Contact your administrator")
	}
	store, err := idjag.NewPostgresStore(s.db)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, nil, "Identity verification is temporarily unavailable")
	}
	userID, err := store.ResolveUser(ctx, endpoint.OrganizationID, strings.TrimSpace(identity.Email))
	if err != nil && !errors.Is(err, idjag.ErrNotProvisioned) {
		return "", oops.E(oops.CodeUnavailable, nil, "Identity verification is temporarily unavailable. Restart login")
	}
	if err != nil || userID == "" {
		return "", oops.E(oops.CodeForbidden, nil, "Your account is not provisioned for this organization. Contact your administrator")
	}
	return userID, nil
}

// FederatedLoginConsumer is the explicit AIM-69 extension point. It runs only
// after a verified, provisioned human passes the organization membership gate.
// No consumer is installed by default and login never requires a refresh token.
type FederatedLoginConsumer interface {
	ConsumeFederatedLogin(context.Context, AuthorizedFederatedLogin) error
}

// FederatedOfflinePolicy is optional. Lookup is called only after resolving a
// provisioned human and verifying current organization membership. Return false
// for an existing durable grant or refusal under this exact configuration hash.
type FederatedOfflinePolicy interface {
	ShouldRequestFederatedOffline(context.Context, FederatedOfflineRequest) (bool, error)
}

type FederatedOfflineRequest struct {
	// Set only by the CSRF-protected, consumed same-human consent retry action.
	ExplicitRetry     bool
	Provider          *remotesessions.FederatedProvider
	OrganizationID    string
	UserID            string
	TrustedIssuerID   uuid.UUID
	TrustedClientID   uuid.UUID
	ConfigurationHash string
}

// AuthorizedFederatedLogin must not be stored in general user/session state.
// Credentials remain opaque; consumers must deliberately request their handoff.
type AuthorizedFederatedLogin struct {
	Provider            *remotesessions.FederatedProvider
	OrganizationID      string
	UserID              string
	UserSessionIssuerID uuid.UUID
	TrustedIssuerID     uuid.UUID
	TrustedClientID     uuid.UUID
	Identity            *remotesessions.FederatedIdentity
	ConfigurationHash   string
	OfflineRequested    bool
	// OptionalRefused is only an explicit access_denied on a bound optional step.
	// Identity is nil in that case; update suppression without replacing tokens.
	OptionalRefused bool
}

// SetFederatedLoginConsumer installs the optional credential handoff at startup,
// before serving requests. A failed consumer fails login without minting a session.
func (s *Service) SetFederatedLoginConsumer(consumer FederatedLoginConsumer) {
	s.federatedLoginConsumer = consumer
}

// Only the upstream AICP callback requires HTTPS. Downstream MCP clients retain
// their registered redirect policy, including native loopback redirects.
func federatedCallbackURL(callback string) (*url.URL, error) {
	target, err := url.Parse(callback)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return nil, remotesessions.ErrFederatedConfiguration
	}
	return target, nil
}

// Never retain arbitrary upstream error strings: they may contain credentials.
func federatedFailure(cause error) (oops.Code, error) {
	if errors.Is(cause, remotesessions.ErrFederatedSigning) {
		return oops.CodeUnexpected, remotesessions.ErrFederatedSigning
	}
	if errors.Is(cause, remotesessions.ErrFederatedUnavailable) {
		return oops.CodeUnavailable, remotesessions.ErrFederatedUnavailable
	}
	if errors.Is(cause, remotesessions.ErrFederatedConfiguration) {
		return oops.CodeFailedPrecondition, remotesessions.ErrFederatedConfiguration
	}
	if errors.Is(cause, remotesessions.ErrFederatedIdentity) {
		return oops.CodeUnauthorized, remotesessions.ErrFederatedIdentity
	}
	return oops.CodeUnavailable, errors.New("federated login dependency unavailable")
}

// Call only for a consumed callback (or a failed preparation whose state was
// removed), so retries and replays cannot inflate terminal flow counters.
func (s *Service) finishFederatedFailure(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState, stage mcpmetrics.OAuthFlowStage, code oops.Code, cause error, message string, declined bool) error {
	ctx := r.Context()
	oauthCode := "server_error"
	if declined || code == oops.CodeForbidden || code == oops.CodeUnauthorized {
		oauthCode = "access_denied"
	}
	if code == oops.CodeUnavailable {
		oauthCode = "temporarily_unavailable"
	}
	var responseErr error
	var redirect string
	if !state.FirstParty {
		issuer, err := endpoint.RootURL(state.mintOriginOr(s.serverURL.String()))
		if err == nil {
			redirect, err = buildClientRedirect(clientRedirectParams{RedirectURI: state.RedirectURI, Issuer: issuer, Code: "", State: state.State, ErrorCode: oauthCode, ErrorDescription: message})
		}
		if err != nil {
			responseErr = oops.E(oops.CodeUnexpected, nil, "Unable to return login error to client")
			declined = false
		}
	}
	if declined {
		s.metrics.RecordOAuthFlowDeclined(ctx, state.UserSessionIssuerID.String(), endpoint.Slug, stage)
	} else {
		s.metrics.RecordOAuthFlowFailed(ctx, state.UserSessionIssuerID.String(), endpoint.Slug, stage)
	}
	failure := oops.E(code, cause, "%s", message).LogError(ctx, s.logger)
	if responseErr != nil {
		return responseErr
	}
	if state.FirstParty {
		return failure
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, redirect, http.StatusFound)
	return nil
}

// NewFederatedDelegationConsumer installs the AIM-69 persistence and policy
// adapter. No changes are made to deployments that leave the consumer unset.
func NewFederatedDelegationConsumer(service *remotesessions.DelegationService) FederatedLoginConsumer {
	return &federatedDelegationConsumer{service: service}
}

type federatedDelegationConsumer struct {
	service *remotesessions.DelegationService
}

func (c *federatedDelegationConsumer) ShouldRequestFederatedOffline(ctx context.Context, request FederatedOfflineRequest) (bool, error) {
	if c.service == nil || request.Provider == nil || request.UserID == "" || request.ConfigurationHash == "" || request.ConfigurationHash != request.Provider.OfflineConfigurationHash() {
		return false, remotesessions.ErrFederatedConfiguration
	}
	status, err := c.service.OfflineStatus(ctx, request.Provider, request.UserID)
	if err != nil {
		return false, err
	}
	return !status.UsableRefresh && (!status.Refused || request.ExplicitRetry), nil
}

func (c *federatedDelegationConsumer) ConsumeFederatedLogin(ctx context.Context, login AuthorizedFederatedLogin) error {
	if c.service == nil || login.Provider == nil || login.UserID == "" || login.ConfigurationHash == "" || login.ConfigurationHash != login.Provider.OfflineConfigurationHash() {
		return remotesessions.ErrFederatedConfiguration
	}
	if login.OptionalRefused {
		if !login.OfflineRequested || login.Identity != nil {
			return remotesessions.ErrFederatedIdentity
		}
		return c.service.RecordOfflineRefusal(ctx, login.Provider, login.UserID)
	}
	return c.service.RetainVerifiedLogin(ctx, login.Provider, login.UserID, login.Identity, login.OfflineRequested)
}

// retryFederatedDelegation starts a fresh minimal login, not an offline prompt.
// The action endpoint has already checked method, endpoint, CSRF, and resolution.
// Retain the original TTL and bind the renewed login to the canonical human.
func (s *Service) retryFederatedDelegation(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) error {
	ctx := r.Context()
	if _, ok := s.federatedLoginConsumer.(FederatedOfflinePolicy); !ok {
		return oops.E(oops.CodeFailedPrecondition, nil, "Delegation retry is unavailable")
	}
	if state.Subject == nil || state.Subject.Kind != urn.SessionSubjectKindUser || state.AuthorizerUserID == "" || state.Subject.ID != state.AuthorizerUserID || state.Federation != nil || state.CreatedAt.IsZero() || time.Since(state.CreatedAt) > state.TTL() || state.CreatedAt.After(time.Now().Add(time.Minute)) {
		return oops.E(oops.CodeUnauthorized, nil, "Delegation retry requires a resolved human login")
	}
	if state.AuthorizerImpersonated == nil || *state.AuthorizerImpersonated {
		return oops.E(oops.CodeForbidden, nil, "Delegation retry requires a non-impersonated login")
	}
	if state.DelegationRetryUsed {
		return oops.E(oops.CodeFailedPrecondition, nil, "Delegation retry was already used. Restart login")
	}
	humanID := state.AuthorizerUserID
	member, err := s.identityResolver.IsOrganizationMember(ctx, endpoint.OrganizationID, humanID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, nil, "Organization verification is unavailable")
	}
	if !member {
		return oops.E(oops.CodeForbidden, nil, "Your account is not provisioned for this organization")
	}
	provider, issuerID, clientID, _, err := s.federatedProvider(ctx, endpoint)
	if err != nil || provider == nil {
		return oops.E(oops.CodeFailedPrecondition, nil, "Trusted login configuration is unavailable")
	}
	if !state.FederatedBinding.matches(issuerID, clientID) {
		return oops.E(oops.CodeFailedPrecondition, nil, "Trusted login binding changed. Restart login")
	}
	consumed, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+state.ID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, nil, "Delegation retry state was already consumed or expired")
	}
	if consumed.Subject == nil || consumed.Subject.Kind != urn.SessionSubjectKindUser || consumed.Subject.ID != humanID || consumed.AuthorizerUserID != humanID || consumed.CSRFToken != state.CSRFToken || consumed.Federation != nil || consumed.AuthorizerImpersonated == nil || *consumed.AuthorizerImpersonated || consumed.DelegationRetryUsed || !consumed.FederatedBinding.matches(issuerID, clientID) {
		return oops.E(oops.CodeUnauthorized, nil, "Delegation retry identity changed")
	}
	if err := endpoint.ValidateChallenge(ctx, consumed.Endpoint, consumed.UserSessionIssuerID); err != nil {
		return oauthAuthorityError(err)
	}
	consumed.ID = uuid.NewString()
	consumed.Subject = nil
	consumed.AuthorizerUserID = ""
	consumed.DelegationRetryUsed = true
	target, err := s.prepareBoundFederatedLogin(ctx, endpoint, &consumed, humanID)
	if err != nil || target == nil {
		return oops.E(oops.CodeFailedPrecondition, nil, "Trusted login configuration is unavailable")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
	return nil
}
