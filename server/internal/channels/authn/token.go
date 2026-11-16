package authn

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/channels/authn/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) token(w http.ResponseWriter, r *http.Request) {
	s.cleanup(r.Context())
	if !parseForm(w, r) {
		return
	}
	client, ok := s.authenticateClient(w, r)
	if !ok {
		return
	}
	target, targetErr := s.target(r.Context(), r.PostForm.Get("resource"))
	if targetErr != nil || oauthwire.ValidateResourceIndicators(oauthwire.ResourceIndicatorsFrom(r.PostForm), target.Resource) != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	kind := ""
	raw := ""
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		kind = "code"
		raw = r.PostForm.Get("code")
	case "refresh_token":
		kind = "refresh"
		raw = r.PostForm.Get("refresh_token")
	default:
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	if raw == "" || (r.PostForm.Get("scope") != "" && r.PostForm.Get("scope") != Scope) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	ctx := r.Context()
	tx, err := s.config.DB.Begin(ctx)
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := repo.New(tx)
	credential, err := q.LockCredential(ctx, repo.LockCredentialParams{TokenHash: digest(raw), OrganizationID: target.OrganizationID, ProjectID: target.ProjectID})
	if err != nil || credential.ClientID != client.ClientID || credential.Kind != kind || credential.Resource != target.Resource {
		oauthError(w, 400, "invalid_grant")
		return
	}
	if kind == "code" && (r.PostForm.Get("redirect_uri") != credential.RedirectUri || !validVerifier(r.PostForm.Get("code_verifier"), credential.CodeChallenge)) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	// Retain consumed refresh credentials for the full family lifetime. Replaying
	// one atomically revokes the entire family, including still-live access JWTs.
	if credential.UsedAt.Valid {
		if err = q.RevokeGrant(ctx, repo.RevokeGrantParams{ID: credential.GrantID, OrganizationID: target.OrganizationID, ProjectID: target.ProjectID}); err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		if err = tx.Commit(ctx); err != nil {
			oauthError(w, 500, "server_error")
			return
		}
		oauthError(w, 400, "invalid_grant")
		return
	}
	now := time.Now()
	if credential.RevokedAt.Valid || !credential.ExpiresAt.Time.After(now) || !credential.GrantExpiresAt.Time.After(now) {
		oauthError(w, 400, "invalid_grant")
		return
	}
	identity, err := parseIdentity(credential.OrganizationID, credential.Principal)
	identity.ProjectID = credential.ProjectID
	if err == nil {
		_, err = s.config.Admit(ctx, identity)
	}
	if err != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	expiry := now.Add(accessLifetime)
	if expiry.After(credential.GrantExpiresAt.Time) {
		expiry = credential.GrantExpiresAt.Time
	}
	access, jti, err := s.config.Signer.Mint(sessiontokens.MintParams{Subject: urn.NewUserSubject(identity.Principal.ID), Audience: credential.Resource, Issuer: s.issuer, ExpiresAt: &expiry, ClientID: client.ClientID, Lifetime: 0, JTI: ""})
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	refresh := randomToken()
	if err = q.UseCredential(ctx, repo.UseCredentialParams{TokenHash: credential.TokenHash, OrganizationID: target.OrganizationID, ProjectID: target.ProjectID}); err == nil {
		err = q.CreateCredential(ctx, repo.CreateCredentialParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, TokenHash: digest(refresh), Kind: "refresh", GrantID: credential.GrantID, RedirectUri: "", CodeChallenge: "", ExpiresAt: credential.GrantExpiresAt})
	}
	if err == nil {
		err = q.CreateAccessToken(ctx, repo.CreateAccessTokenParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, Jti: jti, GrantID: credential.GrantID, ExpiresAt: timestamp(expiry)})
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": oauthwire.TokenTypeBearer, "expires_in": int(time.Until(expiry).Seconds()), "refresh_token": refresh, "scope": Scope})
}
func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	if !parseForm(w, r) {
		return
	}
	client, ok := s.authenticateClient(w, r)
	if !ok {
		return
	}
	token := r.PostForm.Get("token")
	if token == "" {
		oauthError(w, 400, "invalid_request")
		return
	}
	target, targetErr := s.target(r.Context(), r.PostForm.Get("resource"))
	if targetErr != nil {
		oauthError(w, 400, "invalid_target")
		return
	}
	q := repo.New(s.config.DB)
	// Refresh/code hashes live only in Channels. A JWT must additionally carry
	// this server's issuer, exact audience, and authenticating client identifier.
	err := q.RevokeCredential(r.Context(), repo.RevokeCredentialParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, ClientID: client.ClientID, TokenHash: digest(token)})
	if err == nil {
		claims, e := s.config.Signer.ValidateExactAudience(token, target.Resource)
		if e == nil && claims.Issuer == s.issuer && claims.ClientID == client.ClientID {
			err = q.RevokeAccess(r.Context(), repo.RevokeAccessParams{OrganizationID: target.OrganizationID, ProjectID: target.ProjectID, ClientID: client.ClientID, Jti: claims.ID})
		}
	}
	if err != nil {
		oauthError(w, 500, "server_error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Cleanup bounds each sweep. Credentials are removed before empty grants;
// consumed refresh evidence remains until the absolute family expiry.
func (s *Service) Cleanup(ctx context.Context) error {
	q := repo.New(s.config.DB)
	if err := q.CleanupChallenges(ctx); err != nil {
		return fmt.Errorf("cleanup Channels OAuth records: %w", err)
	}
	if err := q.CleanupAccessTokens(ctx); err != nil {
		return fmt.Errorf("cleanup Channels OAuth records: %w", err)
	}
	if err := q.CleanupCredentials(ctx); err != nil {
		return fmt.Errorf("cleanup Channels OAuth records: %w", err)
	}
	if err := q.CleanupGrants(ctx); err != nil {
		return fmt.Errorf("cleanup Channels OAuth records: %w", err)
	}
	if err := q.CleanupClients(ctx); err != nil {
		return fmt.Errorf("cleanup Channels clients: %w", err)
	}
	return nil
}
