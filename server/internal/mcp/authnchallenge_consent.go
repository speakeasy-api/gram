// Consent UI + POST handler for the issuer-gated authn-challenge flow.
// GET renders the consent template; POST persists the user_session_consents
// row, mints a UserSessionGrant, and 302s back to the MCP client.

package mcp

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

//go:embed consent_template.html
var consentTemplateHTML string

var consentTemplate = template.Must(template.New("consent").Parse(consentTemplateHTML))

// remoteSetHashEmpty is the SHA-256 of an empty remote-set, used by the
// consent record's remote_set_hash column when the issuer has no remote
// session clients (the only case today). The empty case is NOT skipped —
// every consent binds to a specific hash so a later non-empty set
// invalidates prior consents.
var remoteSetHashEmpty = func() string {
	h := sha256.Sum256([]byte("[]"))
	return base64.RawURLEncoding.EncodeToString(h[:])
}()

// consentTemplateData is the field set the consent template renders against.
type consentTemplateData struct {
	ClientName     string
	MCPSlug        string
	State          string
	SubjectDisplay string
	RedirectURI    string
}

// HandleConsent serves the GET (consent UI) and POST (Give Access /
// Cancel) for the issuer-gated authn-challenge flow. Mounted at
// `GET, POST /mcp/{mcpSlug}/connect`.
//
// On POST + Give Access:
//
//   - If the AuthnChallengeState's Subject is still empty (public-toolset
//     path that bypassed the IDP), mint a fresh anonymous URN here.
//   - Persist a user_session_consents row binding (principal, client,
//     remote_set_hash). Even the empty-remote-set case is bound to a
//     specific hash so consent can't be CSRF'd past on a future issuer
//     change.
//   - Mint a UserSessionGrant in Redis carrying everything HandleToken
//     needs to mint a JWT (sub, client_id, redirect_uri, code_challenge,
//     scope) and 302 the MCP client to its registered redirect_uri with
//     `?code={code}&state={original_state}`.
func (s *Service) HandleConsent(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		return s.handleConsentGet(w, r)
	case http.MethodPost:
		return s.handleConsentPost(w, r)
	default:
		return oops.E(oops.CodeBadRequest, nil, "method not allowed").Log(r.Context(), s.logger)
	}
}

func (s *Service) handleConsentGet(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	stateID := r.URL.Query().Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            challengeState.ClientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}

	subjectDisplay := "An anonymous session for this MCP server"
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		subjectDisplay = challengeState.Subject.String()
	}

	data := consentTemplateData{
		ClientName:     client.ClientName,
		MCPSlug:        mcpSlug,
		State:          stateID,
		SubjectDisplay: subjectDisplay,
		RedirectURI:    challengeState.RedirectURI,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := consentTemplate.Execute(w, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render consent template").Log(ctx, logger)
	}
	return nil
}

func (s *Service) handleConsentPost(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	// Cap form body to defend against memory exhaustion (gosec G120). The
	// consent form has a few short fields; 16 KiB is generous.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").Log(ctx, s.logger)
	}

	toolset, _, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	// Cancel: 302 the MCP client back to its redirect_uri with
	// access_denied per RFC 6749 §4.1.2.1, preserving the original state.
	if r.PostForm.Get("action") == "deny" {
		denyURL := buildClientRedirect(challengeState.RedirectURI, "", challengeState.State, "access_denied", "user denied consent")
		_ = s.authnChallengeCache.Delete(ctx, challengeState)
		http.Redirect(w, r, denyURL, http.StatusFound)
		return nil
	}

	// Late-bind the anonymous URN here on the public-toolset path. Private
	// toolsets had Subject stamped at /idp_callback time.
	var subject urn.SessionSubject
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		subject = *challengeState.Subject
	} else {
		subject = urn.NewAnonymousSubject(uuid.NewString())
	}

	// Resolve the user_session_clients row id for the consent FK.
	clientRow, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            challengeState.ClientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}

	// Persist the consent record. The unique index on
	// (principal_urn, user_session_client_id, remote_set_hash) makes this
	// idempotent on re-consent for the same set; we treat the duplicate-key
	// error as a no-op (consent already on file).
	if _, err := usersessions_repo.New(s.db).CreateUserSessionConsent(ctx, usersessions_repo.CreateUserSessionConsentParams{
		SubjectUrn:          subject,
		UserSessionClientID: clientRow.ID,
		RemoteSetHash:       remoteSetHashEmpty,
	}); err != nil && !isUniqueViolation(err) {
		return oops.E(oops.CodeUnexpected, err, "record consent").Log(ctx, logger)
	}

	code, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate authorization code").Log(ctx, logger)
	}

	grant := UserSessionGrant{
		Code:                code,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		UserSessionClientID: clientRow.ID,
		ClientID:            challengeState.ClientID,
		RedirectURI:         challengeState.RedirectURI,
		CodeChallenge:       challengeState.CodeChallenge,
		CodeChallengeMethod: challengeState.CodeChallengeMethod,
		Subject:             subject,
		CreatedAt:           time.Now(),
	}
	if err := s.userSessionGrantCache.Store(ctx, grant); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store user session grant").Log(ctx, logger)
	}

	// Authn challenge state has served its purpose; drop it so a replay
	// can't re-issue a code from the same flow.
	_ = s.authnChallengeCache.Delete(ctx, challengeState)

	clientRedirect := buildClientRedirect(challengeState.RedirectURI, code, challengeState.State, "", "")
	http.Redirect(w, r, clientRedirect, http.StatusFound)
	return nil
}

// buildClientRedirect produces the URL to redirect the MCP client to,
// preserving any prior query string on redirectURI and adding `code` (success)
// or `error` / `error_description` (failure) plus the original `state`.
func buildClientRedirect(redirectURI, code, originalState, errCode, errDescription string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		// Should never happen — redirect_uri was validated at HandleAuthorize
		// time. Fall back to a best-effort string concatenation.
		return redirectURI
	}
	q := u.Query()
	if code != "" {
		q.Set("code", code)
	}
	if errCode != "" {
		q.Set("error", errCode)
		if errDescription != "" {
			q.Set("error_description", errDescription)
		}
	}
	if originalState != "" {
		q.Set("state", originalState)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. Used to detect duplicate consent inserts (idempotent re-consent).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps PG errors as *pgconn.PgError with Code "23505".
	return strings.Contains(err.Error(), "23505")
}
