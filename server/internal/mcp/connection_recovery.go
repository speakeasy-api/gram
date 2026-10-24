package mcp

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// connectionRecoveryRoute serves the link a person gets to connect their
	// own account for a turn that acts for them.
	connectionRecoveryRoute = "/mcp/connection_recovery/{token}"
	connectionRecoveryPath  = "/mcp/connection_recovery/"

	// connectionRecoveryDonePath is where the upstream login returns once the
	// connection is stored.
	connectionRecoveryDonePath = "/mcp/connection_recovery_done"
)

// connectionRecovery is one link to connect a person's own account through an
// auth client, bound to that person, the organization and the client. The
// link entry is keyed by its token and used once to start a login; the login
// entry is keyed by the login's parent id and checked again when the login
// completes. Neither changes what the assistant chose.
type connectionRecovery struct {
	ID                  string    `json:"id"`
	OrganizationID      string    `json:"organization_id"`
	UserID              string    `json:"user_id"`
	ProjectID           uuid.UUID `json:"project_id"`
	UserSessionIssuerID uuid.UUID `json:"user_session_issuer_id"`
	McpServerID         uuid.UUID `json:"mcp_server_id"`
	McpSlug             string    `json:"mcp_slug"`
	ServerName          string    `json:"server_name"`
	AuthClientID        uuid.UUID `json:"auth_client_id"`
	RegistrationID      uuid.UUID `json:"registration_id"`
	CreatedAt           time.Time `json:"created_at"`
}

var _ cache.CacheableObject[connectionRecovery] = (*connectionRecovery)(nil)

func (c connectionRecovery) CacheKey() string   { return "connectionRecovery:" + c.ID }
func (c connectionRecovery) TTL() time.Duration { return 15 * time.Minute }

func (c connectionRecovery) expired() bool {
	return c.CreatedAt.IsZero() || time.Since(c.CreatedAt) > c.TTL() || c.CreatedAt.After(time.Now().Add(time.Minute))
}

var (
	errRecoveryNotSignedIn = errors.New("connection recovery requires a signed-in user")
	errRecoveryOtherUser   = errors.New("connection recovery link belongs to another user")
	errRecoveryNoAccess    = errors.New("connection recovery user may not use the client")
)

// connectionRecoveryError answers a turn acting for a user whose own
// connection for the chosen personal client is missing or expired, with a
// link only that user can complete. It reports false for any other caller or
// client, which keep the ordinary answer.
func (s *Service) connectionRecoveryError(ctx context.Context, authentication *issuerGateAuthentication) (error, bool) {
	res := authentication.authClient
	authenticated, ok := principalcredential.FromContext(ctx)
	if !ok || authenticated.Credential.AuthorizerUserID == "" || res == nil || res.outcome != authClientResolved ||
		res.clientType != authClientTypeRemoteSession || !authentication.chosenClient.Valid || !authentication.endpoint.McpServerID.Valid {
		return nil, false
	}
	endpoint := authentication.endpoint
	userID := authenticated.Credential.AuthorizerUserID
	statuses, err := s.remoteChallengeMgr.AuthClientRemoteSessionStatuses(ctx, urn.NewUserSubject(userID), endpoint.ProjectID, endpoint.OrganizationID, []uuid.UUID{authentication.chosenClient.UUID})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read connection status").LogError(ctx, s.logger), true
	}
	_, connected := statuses[authentication.chosenClient.UUID]

	token, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "mint connection link").LogError(ctx, s.logger), true
	}
	recovery := connectionRecovery{
		ID: token, OrganizationID: endpoint.OrganizationID, UserID: userID, ProjectID: endpoint.ProjectID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID, McpServerID: endpoint.McpServerID.UUID, McpSlug: endpoint.Slug,
		ServerName: res.serverName, AuthClientID: res.clientID, RegistrationID: authentication.chosenClient.UUID, CreatedAt: time.Now(),
	}
	if err := s.connectionRecoveryCache.Store(ctx, recovery); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store connection link").LogError(ctx, s.logger), true
	}
	link := s.platformBaseURL(ctx).JoinPath(connectionRecoveryPath, token).String()

	name := res.serverName
	if name == "" {
		name = "this MCP server"
	}
	heading, action := "Connection required", "connect"
	if connected {
		heading, action = "Reconnect required", "reconnect"
	}
	message := fmt.Sprintf("%s: the person this turn acts for needs to %s their own account for %s. "+
		"Send this link privately to that person only; it works only for them, signed in to Speakeasy, and expires in 15 minutes. "+
		"If you cannot reach them privately, do not post the link: ask them to open Speakeasy and %s %s instead.\n\n%s",
		heading, action, name, action, name, link)
	s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: represented user's connection "+action+" required", issuerGateReasonInvalidRemoteSession)
	return oops.E(oops.CodeFailedPrecondition, nil, "%s", message).LogWarn(ctx, s.logger, attr.SlogAuthClientID(res.clientID.String())), true
}

// HandleConnectionRecovery serves a connection link. The signed-in Speakeasy
// user must be the person it was issued for, still a member of the
// organization and still allowed to use the client; a person who may not
// sees "No access" and no way to connect. Only then is the link used up and
// the upstream login started for that person.
func (s *Service) HandleConnectionRecovery(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	if origin, ok := requestorigin.FromContext(ctx); ok && origin.Surface != requestorigin.SurfacePlatform {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := chi.URLParam(r, "token")
	if token == "" {
		return renderConnectionRecoveryPage(w, http.StatusNotFound, connectionRecoveryExpiredPage)
	}
	recovery, err := s.connectionRecoveryCache.Get(ctx, "connectionRecovery:"+token)
	if err != nil || recovery.expired() || recovery.ID != token {
		return renderConnectionRecoveryPage(w, http.StatusNotFound, connectionRecoveryExpiredPage)
	}
	if _, err := s.verifyConnectionRecoveryUser(ctx, r, recovery); err != nil {
		return s.answerConnectionRecoveryRefusal(w, r, recovery, err)
	}
	// Used once, and only by the person it names.
	recovery, err = s.connectionRecoveryCache.GetAndDelete(ctx, "connectionRecovery:"+token)
	if err != nil || recovery.expired() {
		return renderConnectionRecoveryPage(w, http.StatusNotFound, connectionRecoveryExpiredPage)
	}

	clients, err := s.remoteChallengeMgr.ListAuthClientRegistrations(ctx, recovery.ProjectID, recovery.OrganizationID, []uuid.UUID{recovery.RegistrationID})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load connection client").LogError(ctx, s.logger)
	}
	if len(clients) != 1 || clients[0].CredentialOwner == remotesessions.CredentialOwnerSelf {
		return renderConnectionRecoveryPage(w, http.StatusConflict, connectionRecoveryUnavailablePage)
	}
	login := recovery
	login.ID = uuid.NewString()
	login.CreatedAt = time.Now()
	if err := s.connectionRecoveryCache.Store(ctx, login); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store connection login").LogError(ctx, s.logger)
	}
	subject := urn.NewUserSubject(recovery.UserID)
	authorizationURL, err := s.remoteChallengeMgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
		ID:                  login.ID,
		ProjectID:           recovery.ProjectID,
		OrganizationID:      recovery.OrganizationID,
		UserSessionIssuerID: recovery.UserSessionIssuerID,
		Subject:             &subject,
		McpSlug:             recovery.McpSlug,
		RouteBase:           mcproute.MCP,
		McpServerID:         uuid.NullUUID{UUID: recovery.McpServerID, Valid: true},
		MetaMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ResourceMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		FinalRedirectURI:    s.platformBaseURL(ctx).JoinPath(connectionRecoveryDonePath).String(),
		ConsentURL:          "",
		ConsentServer:       "",
		Resource:            "",
		AutoRefresh:         nil,
		Authority:           networkingress.Authority{Surface: "", BaseURL: "", OrganizationID: "", NetworkIngressID: uuid.Nil, NamespaceKind: "", CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}},
		BrowserCookieID:     "",
		BrowserHash:         "",
	}, clients[0])
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "start connection login").LogError(ctx, s.logger)
	}
	http.Redirect(w, r, authorizationURL, http.StatusFound)
	return nil
}

// verifyConnectionRecoveryCompletion runs when the upstream login of a
// connection link returns, before its connection is stored: the browser must
// still be signed in as the person the link named, and that person must still
// be allowed to use the client. It reports false for a login that is not a
// connection link's.
func (s *Service) verifyConnectionRecoveryCompletion(r *http.Request, remote remotesessions.RemoteLoginState) (bool, error) {
	if !strings.HasSuffix(remote.FinalRedirectURI, connectionRecoveryDonePath) {
		return false, nil
	}
	ctx := r.Context()
	login, err := s.connectionRecoveryCache.GetAndDelete(ctx, "connectionRecovery:"+remote.ParentChallengeID)
	if err != nil || login.expired() || login.ID != remote.ParentChallengeID {
		return true, errors.New("connection login expired")
	}
	if remote.Subject == nil || remote.Subject.Kind != urn.SessionSubjectKindUser || remote.Subject.ID != login.UserID ||
		remote.RemoteSessionClientID != login.RegistrationID || remote.OrganizationID != login.OrganizationID {
		return true, errors.New("connection login does not match its link")
	}
	if _, err := s.verifyConnectionRecoveryUser(ctx, r, login); err != nil {
		return true, err
	}
	return true, nil
}

// verifyConnectionRecoveryUser checks the request's Speakeasy session is the
// recovery's person, acting as themselves, in its organization and still
// allowed to use its client.
func (s *Service) verifyConnectionRecoveryUser(ctx context.Context, r *http.Request, recovery connectionRecovery) (context.Context, error) {
	cookie, err := r.Cookie(constants.SessionCookie)
	if err != nil || cookie.Value == "" {
		return ctx, errRecoveryNotSignedIn
	}
	authenticate := s.sessions.Authenticate
	if s.browserSession != nil {
		authenticate = s.browserSession
	}
	authed, err := authenticate(ctx, cookie.Value)
	if err != nil {
		return ctx, errRecoveryNotSignedIn
	}
	authCtx, ok := contextvalues.GetAuthContext(authed)
	if !ok || authCtx == nil || authCtx.UserID == "" {
		return ctx, errRecoveryNotSignedIn
	}
	if authCtx.UserID != recovery.UserID || contextvalues.IsSupportSession(authed) || contextvalues.IsLegacyImpersonatedSession(authed) {
		return ctx, errRecoveryOtherUser
	}
	active, err := orgrepo.New(s.db).HasActiveOrganizationUser(ctx, orgrepo.HasActiveOrganizationUserParams{UserID: recovery.UserID, OrganizationID: recovery.OrganizationID})
	if err != nil {
		return ctx, fmt.Errorf("check connection user membership: %w", err)
	}
	if !active {
		return ctx, errRecoveryNoAccess
	}
	scoped := *authCtx
	scoped.ActiveOrganizationID = recovery.OrganizationID
	scoped.ProjectID = &recovery.ProjectID
	authed = contextvalues.SetAuthContext(authed, &scoped)
	permitter, err := s.authClientPermitFor(authed, recovery.OrganizationID)
	if err != nil {
		return ctx, fmt.Errorf("load connection permitter: %w", err)
	}
	allowed, err := permitter.Permitted(authed, authClientPermitRequest{
		OrganizationID: recovery.OrganizationID, ProjectID: recovery.ProjectID, ServerID: recovery.McpServerID,
		ClientID: recovery.AuthClientID, ClientType: authClientTypeRemoteSession, RegistrationID: uuid.NullUUID{UUID: recovery.RegistrationID, Valid: true},
		CredentialKind: "", CredentialID: uuid.Nil, Subject: urn.NewUserSubject(recovery.UserID),
	})
	if err != nil {
		return ctx, fmt.Errorf("check connection permission: %w", err)
	}
	if !allowed {
		return ctx, errRecoveryNoAccess
	}
	return authed, nil
}

// answerConnectionRecoveryRefusal answers a link its browser may not use. A
// signed-out browser signs in and returns; anyone else learns nothing about
// the link, and the person it names keeps it.
func (s *Service) answerConnectionRecoveryRefusal(w http.ResponseWriter, r *http.Request, recovery connectionRecovery, err error) error {
	switch {
	case errors.Is(err, errRecoveryNotSignedIn):
		login := s.platformBaseURL(r.Context()).JoinPath("login")
		login.RawQuery = url.Values{"redirect": {r.URL.Path}}.Encode()
		http.Redirect(w, r, login.String(), http.StatusFound)
		return nil
	case errors.Is(err, errRecoveryOtherUser):
		return renderConnectionRecoveryPage(w, http.StatusForbidden, connectionRecoveryOtherUserPage)
	case errors.Is(err, errRecoveryNoAccess):
		s.logger.InfoContext(r.Context(), "connection link refused: no access", attr.SlogAuthClientID(recovery.AuthClientID.String()))
		return renderConnectionRecoveryPage(w, http.StatusForbidden, connectionRecoveryNoAccessPage)
	default:
		return oops.E(oops.CodeUnexpected, err, "check connection link").LogError(r.Context(), s.logger)
	}
}

// HandleConnectionRecoveryDone is where a connection link's login returns.
func (s *Service) HandleConnectionRecoveryDone(w http.ResponseWriter, _ *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	return renderConnectionRecoveryPage(w, http.StatusOK, connectionRecoveryDonePage)
}

type connectionRecoveryPage struct {
	Title   string
	Message string
}

var (
	connectionRecoveryDonePage = connectionRecoveryPage{
		Title:   "Account connected",
		Message: "You can close this page, go back to your conversation and ask the assistant to try again.",
	}
	connectionRecoveryExpiredPage = connectionRecoveryPage{
		Title:   "This link has expired",
		Message: "Ask the assistant again to get a new link.",
	}
	connectionRecoveryOtherUserPage = connectionRecoveryPage{
		Title:   "This link is for someone else",
		Message: "Only the person it was sent to can use it. Sign in to Speakeasy as that person, or ask the assistant for a new link.",
	}
	connectionRecoveryNoAccessPage = connectionRecoveryPage{
		Title:   "No access",
		Message: "You don't have access to this connection. Ask an organization administrator to grant access.",
	}
	connectionRecoveryUnavailablePage = connectionRecoveryPage{
		Title:   "This connection is no longer available",
		Message: "Ask an organization administrator to check the server's upstream credentials.",
	}
)

var connectionRecoveryTemplate = template.Must(template.New("connection_recovery").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · Speakeasy</title>
<style>
body{margin:0;font-family:system-ui,-apple-system,sans-serif;background:#fafafa;color:#111;display:flex;min-height:100vh;align-items:center;justify-content:center;padding:16px}
main{max-width:420px;background:#fff;border:1px solid #e5e5e5;padding:32px}
h1{font-size:20px;margin:0 0 12px}
p{margin:0;line-height:1.5;color:#444}
@media (prefers-color-scheme:dark){body{background:#0a0a0a;color:#eee}main{background:#141414;border-color:#2a2a2a}p{color:#bbb}}
</style>
</head>
<body><main><h1>{{.Title}}</h1><p>{{.Message}}</p></main></body>
</html>`))

func renderConnectionRecoveryPage(w http.ResponseWriter, status int, page connectionRecoveryPage) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := connectionRecoveryTemplate.Execute(w, page); err != nil {
		return fmt.Errorf("render connection page: %w", err)
	}
	return nil
}
