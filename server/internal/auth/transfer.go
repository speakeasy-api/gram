package auth

// Cross-domain session transfer: moving a browser's session from one platform
// host to another without a second sign-in.
//
//	A = source host (holds the session); B = destination host
//	Every arrow is a browser navigation or redirect.
//
//	Browser with an A session
//	    |
//	    v
//	B: transferIn(source_host=A)                 [Step 1/3: start]
//	   Set a per-transfer, host-only nonce cookie on B
//	    |
//	    | redirect to A with target_host=B + nonce
//	    v
//	A: transferOut                               [Step 2/3: authorize]
//	   Authenticate the A session; check the organization's default host is B
//	   Store a short-lived opaque code bound to B and the nonce hash
//	    |
//	    | redirect to B with the code
//	    v
//	B: transferIn(code=...)                      [Step 3/3: complete]
//	   Check the code was issued for B and B holds the matching nonce cookie
//	   Re-check membership; atomically consume the code
//	   Create a B session and set the B session cookie
//	    |
//	    v
//	B: the requested dashboard page
//
//	Any failure -> B's (or A's) login page with a safe signin_error code;
//	a transfer never restarts itself.
//
// Why the browser makes this round trip:
//
//   - A cannot set B's session cookie: they are different registrable
//     domains, so only a response from B can.
//   - A one-time code alone does not stop login CSRF. An attacker could get a
//     code for their own account and send a victim the callback URL, signing
//     the victim into the attacker's account.
//   - Starting on B sets a browser-specific nonce cookie. A binds the code to
//     that nonce's hash, and B creates a session only for the browser holding
//     the matching cookie.
//   - So the start, authorize and complete steps are what bind the transfer
//     to one browser. They are not incidental routing; a simplification must
//     keep all three.
//   - transferOut is a GET, so a signed-in victim can be made to issue a code
//     bound to a nonce someone else chose. B therefore burns a code presented
//     with a missing or mismatched cookie, leaving nothing to redeem if its
//     URL leaks.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authsessions "github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// transferSigninError is a public signin_error code a failed transfer puts on
// the login page URL. The dashboard maps each to a message. Only these
// constants ever reach the URL, never error text or identifiers.
type transferSigninError string

const (
	// The source host has no usable session to transfer.
	signinTransferSessionExpired transferSigninError = "transfer_session_expired"
	// The transfer names a host it may not use, or its parameters are invalid.
	signinTransferWrongDestination transferSigninError = "transfer_wrong_destination"
	// Support and impersonation sessions stay on their own host.
	signinTransferNotTransferable transferSigninError = "transfer_not_transferable"
	// The code is unknown, expired or already used.
	signinTransferExpired transferSigninError = "transfer_expired"
	// The browser does not hold the transfer's nonce cookie.
	signinTransferBrowserMismatch transferSigninError = "transfer_browser_mismatch"
	// The user is no longer a member of the organization.
	signinTransferAccessChanged transferSigninError = "transfer_access_changed"
	// A backend or configuration failure; trying again may work.
	signinTransferTemporaryError transferSigninError = "transfer_temporary_error"
)

// transferFailure is why a transfer step sent the browser to sign in: the
// public code for the URL, and a finer internal reason and cause for logs.
type transferFailure struct {
	code   transferSigninError
	reason string
	err    error
}

func failTransfer(code transferSigninError, reason string, err error) *transferFailure {
	return &transferFailure{code: code, reason: reason, err: err}
}

// log records the failure with its internal reason and cause. Temporary
// errors are logged as errors; the rest are expected user-facing outcomes.
func (f *transferFailure) log(ctx context.Context, logger *slog.Logger) {
	attrs := []any{attr.SlogTransferFailureReason(f.reason), attr.SlogTransferSigninError(string(f.code))}
	if f.err != nil {
		attrs = append(attrs, attr.SlogError(f.err))
	}
	if f.code == signinTransferTemporaryError {
		logger.ErrorContext(ctx, "session transfer failed", attrs...)
		return
	}
	logger.WarnContext(ctx, "session transfer failed", attrs...)
}

// transferRedirect returns the sanitized, same-origin path a transfer should
// land on, or "/" when none was given or it cannot be trusted.
func transferRedirect(raw *string) string {
	if raw != nil {
		if redirect := safeRedirectPath(*raw, ""); redirect != "" {
			return redirect
		}
	}
	return "/"
}

// dashboardSiteURL returns the dashboard base URL on the platform host whose
// server base URL is baseURL. Extra platform hosts serve the dashboard
// themselves; the configured server host uses the configured site.
func (s *Service) dashboardSiteURL(baseURL string) string {
	if strings.TrimRight(baseURL, "/") == strings.TrimRight(s.cfg.GramServerURL, "/") {
		return s.siteOrigin
	}
	return strings.TrimRight(baseURL, "/")
}

// dashboardLoginURL is the dashboard login page under siteURL that shows
// signinError and returns to redirect after signing in. Transfers land there
// on any failure, so a browser mid-navigation never stops on an error page.
func dashboardLoginURL(siteURL, redirect string, signinError transferSigninError) string {
	query := url.Values{}
	query.Set("redirect", redirect)
	query.Set("signin_error", string(signinError))
	return strings.TrimRight(siteURL, "/") + "/login?" + query.Encode()
}

// sessionFailure maps an error from authenticating the source session.
func sessionFailure(err error) *transferFailure {
	var shareable *oops.ShareableError
	if errors.As(err, &shareable) && shareable.Code == oops.CodeUnauthorized {
		return failTransfer(signinTransferSessionExpired, "session_unauthenticated", err)
	}
	if errors.As(err, &shareable) && shareable.Code == oops.CodeForbidden {
		return failTransfer(signinTransferAccessChanged, "session_forbidden", err)
	}
	return failTransfer(signinTransferTemporaryError, "session_authenticate_failed", err)
}

// codeFailure maps an error from looking up or consuming a transfer code.
func codeFailure(step string, err error) *transferFailure {
	switch {
	case errors.Is(err, authsessions.ErrTransferCodeNotFound):
		return failTransfer(signinTransferExpired, step+"_code_not_found", err)
	case errors.Is(err, authsessions.ErrTransferWrongHost):
		return failTransfer(signinTransferWrongDestination, step+"_code_wrong_host", err)
	default:
		return failTransfer(signinTransferTemporaryError, step+"_code_cache_failed", err)
	}
}

// TransferOut is Step 2/3 (authorize) of the session transfer; see the flow
// at the top of this file. It runs on the source host after Step 1/3,
// TransferIn's start mode, and redirects to Step 3/3, TransferIn's callback
// mode. It authenticates the caller's session, checks that the session's
// active organization lives on the target host, and stores a one-time code
// bound to the nonce from Step 1/3. On any failure the browser lands on a
// login page: the target host's when the target is a platform host, this
// host's otherwise.
func (s *Service) TransferOut(ctx context.Context, payload *gen.TransferOutPayload) (*gen.TransferOutResult, error) {
	logger := s.logger.With(attr.SlogGoaMethod("TransferOut"))
	redirect := transferRedirect(payload.Redirect)

	loginSiteURL := s.platformHostURL(ctx, s.siteOrigin)
	location, failure := s.transferOut(ctx, logger, payload, redirect, &loginSiteURL)
	if failure != nil {
		failure.log(ctx, logger)
		return &gen.TransferOutResult{Location: dashboardLoginURL(loginSiteURL, redirect, failure.code)}, nil
	}
	return &gen.TransferOutResult{Location: location}, nil
}

// transferOut does TransferOut's work. Once the target is known to be a
// platform host it points loginSiteURL at that host's dashboard, so failures
// after that land on the target's login page.
func (s *Service) transferOut(ctx context.Context, logger *slog.Logger, payload *gen.TransferOutPayload, redirect string, loginSiteURL *string) (string, *transferFailure) {
	if s.cfg.OrgHosts == nil {
		return "", failTransfer(signinTransferTemporaryError, "platform_hosts_not_configured", nil)
	}
	targetBaseURL, ok := s.cfg.OrgHosts.IsPlatformHost(conv.PtrValOr(payload.TargetHost, ""))
	if !ok {
		return "", failTransfer(signinTransferWrongDestination, "target_not_platform_host", nil)
	}
	*loginSiteURL = s.dashboardSiteURL(targetBaseURL)

	sourceURL, ok := currentPlatformURL(ctx)
	if !ok {
		return "", failTransfer(signinTransferWrongDestination, "source_not_platform_host", nil)
	}
	nonce := conv.PtrValOr(payload.Nonce, "")
	if nonce == "" {
		return "", failTransfer(signinTransferWrongDestination, "nonce_missing", nil)
	}

	// The session comes from the header, or the session cookie when there is
	// none, the same way the session security scheme reads it. Support and
	// impersonation sessions are refused before authenticating, so they get
	// their own message whatever state they are in.
	sessionID := conv.PtrValOr(payload.SessionToken, "")
	if sessionID == "" {
		sessionID, _ = contextvalues.GetSessionTokenFromContext(ctx)
	}
	if sessionID == "" {
		return "", failTransfer(signinTransferSessionExpired, "session_missing", nil)
	}
	session, err := s.sessions.GetSession(ctx, sessionID)
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss):
		return "", failTransfer(signinTransferSessionExpired, "session_not_found", nil)
	case err != nil:
		return "", failTransfer(signinTransferTemporaryError, "session_load_failed", err)
	}
	if session.ImpersonatorEmail != "" || session.SupportOrganizationID != "" {
		return "", failTransfer(signinTransferNotTransferable, "session_not_transferable", nil)
	}
	// Authenticate also checks membership, a disabled organization, and
	// refreshes the session, as any authenticated request would.
	ctx, err = s.sessions.Authenticate(ctx, sessionID)
	if err != nil {
		return "", sessionFailure(err)
	}

	targetURL, err := url.Parse(targetBaseURL)
	if err != nil {
		return "", failTransfer(signinTransferTemporaryError, "target_url_invalid", err)
	}

	// Only a session whose organization would move to the target host by the
	// same rule the login callback and dashboard use transfers there: a stored,
	// non-NULL default host that is a configured platform host other than this
	// one, reached without downgrading to http. NULL means the legacy host,
	// which never transfers, and neither do organization-less or demo
	// sessions.
	if session.ActiveOrganizationID == "" {
		return "", failTransfer(signinTransferWrongDestination, "no_active_organization", nil)
	}
	orgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, session.ActiveOrganizationID)
	if err != nil {
		return "", failTransfer(signinTransferTemporaryError, "organization_load_failed", err)
	}
	move, ok := s.organizationHostMove(ctx, orgMetadata.DefaultHost)
	if !ok || !sameHost(move.serverURL.Host, targetURL.Host) {
		return "", failTransfer(signinTransferWrongDestination, "target_not_organization_host", nil)
	}

	code, err := s.transferManager.Create(ctx, session, nonce, sourceURL.Host, targetURL.Host)
	switch {
	case errors.Is(err, authsessions.ErrSessionNotTransferable):
		return "", failTransfer(signinTransferNotTransferable, "session_not_transferable", err)
	case err != nil:
		return "", failTransfer(signinTransferTemporaryError, "code_create_failed", err)
	}

	query := url.Values{}
	query.Set("code", code)
	query.Set("redirect", redirect)
	targetURL.Path = strings.TrimRight(targetURL.Path, "/") + transferInPath
	targetURL.RawQuery = query.Encode()

	logger.InfoContext(ctx, "initiating session transfer",
		attr.SlogSourceHost(sourceURL.Host),
		attr.SlogTargetHost(targetURL.Host),
		attr.SlogUserID(session.UserID),
	)
	return targetURL.String(), nil
}

// TransferIn is Steps 1/3 and 3/3 of the session transfer; see the flow at
// the top of this file. Its mode is chosen by its parameters:
//
//   - Step 1/3, start (source_host, no code), sets a per-transfer nonce cookie
//     on this host and redirects to Step 2/3, TransferOut on the source host,
//     so the code TransferOut issues is bound to this browser.
//   - Step 3/3, callback (code, no source_host), redeems that code: it must
//     have been issued for this host, the browser must hold the matching
//     nonce cookie, and the user must still be a member of the organization.
//     Only then is the code consumed and a new session minted here.
//
// The nonce exists because a code alone is a bearer credential for its
// account: anyone holding one could send it to someone else and sign them
// into the sender's account (login CSRF). A request with both or neither
// parameter, and every failed check, lands on this host's login page with a
// signin_error code. A failed callback never falls back to start mode, so a
// broken transfer cannot restart itself.
func (s *Service) TransferIn(ctx context.Context, payload *gen.TransferInPayload) (*gen.TransferInResult, error) {
	logger := s.logger.With(attr.SlogGoaMethod("TransferIn"))
	redirect := transferRedirect(payload.Redirect)
	siteURL := s.platformHostURL(ctx, s.siteOrigin)

	sourceHost := conv.PtrValOr(payload.SourceHost, "")
	code := conv.PtrValOr(payload.Code, "")
	var (
		result  *gen.TransferInResult
		failure *transferFailure
	)
	switch {
	case sourceHost != "" && code != "":
		failure = failTransfer(signinTransferWrongDestination, "both_modes", nil)
	case sourceHost != "":
		result, failure = s.transferInStart(ctx, sourceHost, redirect)
	case code != "":
		result, failure = s.transferInCallback(ctx, logger, code, redirect, siteURL)
	default:
		failure = failTransfer(signinTransferWrongDestination, "no_mode", nil)
	}
	if failure != nil {
		failure.log(ctx, logger)
		return &gen.TransferInResult{
			Location:      dashboardLoginURL(siteURL, redirect, failure.code),
			SessionToken:  nil,
			SessionCookie: nil,
		}, nil
	}
	return result, nil
}

// transferSourceBaseURL resolves the source_host of TransferIn's start mode
// to the server base URL that holds its session. Besides platform hosts, it
// accepts the configured dashboard host, whose sessions live on the server
// host when the two differ.
func (s *Service) transferSourceBaseURL(sourceHost string) (string, bool) {
	if baseURL, ok := s.cfg.OrgHosts.IsPlatformHost(sourceHost); ok {
		return baseURL, true
	}
	site, err := url.Parse(s.siteOrigin)
	if err != nil || site.Host == "" || sourceHost == "" || !sameHost(sourceHost, site.Host) {
		return "", false
	}
	return s.cfg.GramServerURL, s.cfg.GramServerURL != ""
}

// transferInStart is Step 1/3, TransferIn's start mode.
func (s *Service) transferInStart(ctx context.Context, sourceHost, redirect string) (*gen.TransferInResult, *transferFailure) {
	if s.cfg.OrgHosts == nil {
		return nil, failTransfer(signinTransferTemporaryError, "platform_hosts_not_configured", nil)
	}
	currentURL, ok := currentPlatformURL(ctx)
	if !ok {
		return nil, failTransfer(signinTransferWrongDestination, "target_not_platform_host", nil)
	}
	sourceBaseURL, ok := s.transferSourceBaseURL(sourceHost)
	if !ok {
		return nil, failTransfer(signinTransferWrongDestination, "source_not_platform_host", nil)
	}
	sourceURL, err := url.Parse(sourceBaseURL)
	if err != nil {
		return nil, failTransfer(signinTransferTemporaryError, "source_url_invalid", err)
	}
	if sameHost(currentURL.Host, sourceURL.Host) {
		return nil, failTransfer(signinTransferWrongDestination, "source_is_target", nil)
	}
	jar, ok := transferCookieJarFromContext(ctx)
	if !ok {
		return nil, failTransfer(signinTransferTemporaryError, "cookie_jar_missing", nil)
	}
	nonce, err := authsessions.NewSessionID()
	if err != nil {
		return nil, failTransfer(signinTransferTemporaryError, "nonce_generate_failed", err)
	}

	query := url.Values{}
	query.Set("target_host", currentURL.Host)
	query.Set("nonce", nonce)
	query.Set("redirect", redirect)
	sourceURL.Path = strings.TrimRight(sourceURL.Path, "/") + "/rpc/auth.transferOut"
	sourceURL.RawQuery = query.Encode()

	jar.Set(transferNonceCookieName(authsessions.TransferNonceHash(nonce)), nonce)
	return &gen.TransferInResult{Location: sourceURL.String(), SessionToken: nil, SessionCookie: nil}, nil
}

// transferInCallback is Step 3/3, TransferIn's callback mode.
func (s *Service) transferInCallback(ctx context.Context, logger *slog.Logger, code, redirect, siteURL string) (*gen.TransferInResult, *transferFailure) {
	currentURL, ok := currentPlatformURL(ctx)
	if !ok {
		return nil, failTransfer(signinTransferWrongDestination, "target_not_platform_host", nil)
	}

	record, err := s.transferManager.Lookup(ctx, code, currentURL.Host)
	if err != nil {
		return nil, codeFailure("lookup", err)
	}

	// The cookie is single-use, so it is cleared whatever the outcome.
	jar, ok := transferCookieJarFromContext(ctx)
	if !ok {
		return nil, failTransfer(signinTransferTemporaryError, "cookie_jar_missing", nil)
	}
	cookieName := transferNonceCookieName(record.NonceHash)
	nonce := jar.Get(cookieName)
	jar.Clear(cookieName)
	if !record.BoundTo(nonce) {
		// Burn the code: see the flow notes at the top of this file. A
		// browser without the cookie could never redeem it anyway.
		if err := s.transferManager.Consume(ctx, code); err != nil {
			logger.WarnContext(ctx, "failed to burn transfer code", attr.SlogError(err))
		}
		reason := "nonce_mismatch"
		if nonce == "" {
			reason = "nonce_cookie_missing"
		}
		return nil, failTransfer(signinTransferBrowserMismatch, reason, nil)
	}

	isMember, err := s.identity.IsOrganizationMember(ctx, record.ActiveOrganizationID, record.UserID)
	if err != nil {
		return nil, failTransfer(signinTransferTemporaryError, "membership_lookup_failed", err)
	}
	if !isMember {
		return nil, failTransfer(signinTransferAccessChanged, "not_a_member", nil)
	}

	// Consume only after every check passes, so a transient failure above
	// leaves the code redeemable. Consume is atomic: a concurrent second use
	// fails here.
	if err := s.transferManager.Consume(ctx, code); err != nil {
		return nil, codeFailure("consume", err)
	}

	newSessionID, err := authsessions.NewSessionID()
	if err != nil {
		return nil, failTransfer(signinTransferTemporaryError, "session_id_generate_failed", err)
	}
	newSession := authsessions.Session{
		SessionID:             newSessionID,
		ActiveOrganizationID:  record.ActiveOrganizationID,
		UserID:                record.UserID,
		WorkOSSessionID:       record.WorkOSSessionID,
		ImpersonatorEmail:     "", // TransferOut refuses impersonation sessions.
		SupportOrganizationID: "", // TransferOut refuses support sessions.
		SupportExpiresAt:      time.Time{},
	}
	if err := s.sessions.StoreSession(ctx, newSession); err != nil {
		return nil, failTransfer(signinTransferTemporaryError, "session_store_failed", fmt.Errorf("store session: %w", err))
	}

	logger.InfoContext(ctx, "completed session transfer",
		attr.SlogSourceHost(record.SourceHost),
		attr.SlogTargetHost(currentURL.Host),
		attr.SlogUserID(record.UserID),
	)

	return &gen.TransferInResult{
		Location:      strings.TrimRight(siteURL, "/") + redirect,
		SessionToken:  &newSessionID,
		SessionCookie: &newSessionID,
	}, nil
}
