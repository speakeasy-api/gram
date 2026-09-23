package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"goa.design/goa/v3/security"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

const staffAccessLifetime = time.Hour
const staffRefreshLifetime = 24 * time.Hour

// StaffOAuthTokens exchanges one-use codes and rotates refresh tokens. No HTTP
// handler in this package accepts an admin browser cookie as a token credential.
type StaffOAuthTokens struct {
	clients  staffClientStore
	store    staffGrantStore
	verifier adminSessionVerifier
	cipher   *encryption.Client
	signer   *sessiontokens.Signer
	issuer   string
	resource string
}

func NewStaffOAuthTokens(clients staffClientStore, store staffGrantStore, verifier adminSessionVerifier, cipher *encryption.Client, signer *sessiontokens.Signer, issuer, resource string) *StaffOAuthTokens {
	return &StaffOAuthTokens{clients: clients, store: store, verifier: verifier, cipher: cipher, signer: signer, issuer: issuer, resource: resource}
}

func (s *StaffOAuthTokens) TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if s == nil || s.clients == nil || s.store == nil || s.verifier == nil || s.cipher == nil || s.signer == nil || s.issuer == "" || s.resource == "" {
			staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "token endpoint unavailable")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			staffOAuthError(w, http.StatusBadRequest, "invalid_request", "could not parse token request")
			return
		}
		clientID, secret := staffClientCredentials(r)
		client, err := s.authenticateClient(r.Context(), clientID, secret)
		if err != nil {
			staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
			return
		}
		_, _, basicAuth := r.BasicAuth()
		if !basicAuth || r.PostForm.Has("client_id") || r.PostForm.Has("client_secret") {
			staffOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication method is invalid")
			return
		}
		switch r.PostForm.Get("grant_type") {
		case oauthwire.GrantTypeAuthorizationCode:
			request := usersessions.AuthCodeTokenRequestFromForm(r.PostForm)
			if err := request.Validate(); err != nil {
				staffRequestError(w, err)
				return
			}
			if err := oauthwire.ValidateResourceIndicators(request.Resources, s.resource); err != nil {
				staffRequestError(w, err)
				return
			}
			s.exchangeCode(w, r.Context(), client.ID, request)
		case oauthwire.GrantTypeRefreshToken:
			request := usersessions.RefreshTokenRequestFromForm(r.PostForm)
			if err := request.Validate(); err != nil {
				staffRequestError(w, err)
				return
			}
			if err := oauthwire.ValidateResourceIndicators(request.Resources, s.resource); err != nil {
				staffRequestError(w, err)
				return
			}
			s.refresh(w, r.Context(), client.ID, request)
		default:
			staffOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
		}
	})
}

func staffClientCredentials(r *http.Request) (string, string) {
	id, secret, _ := r.BasicAuth()
	return id, secret
}

func (s *StaffOAuthTokens) authenticateClient(ctx context.Context, clientID, secret string) (staffOAuthClient, error) {
	if clientID == "" {
		return staffOAuthClient{}, errStaffGrant
	}
	client, err := s.clients.GetClient(ctx, clientID)
	if err != nil || (client.SecretExpiresAt != nil && !time.Now().Before(*client.SecretExpiresAt)) {
		return staffOAuthClient{}, errStaffGrant
	}
	if client.SecretHash == "" || bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(secret)) != nil {
		return staffOAuthClient{}, errStaffGrant
	}
	return client, nil
}

func (s *StaffOAuthTokens) exchangeCode(w http.ResponseWriter, ctx context.Context, clientID string, request *usersessions.AuthCodeTokenRequest) {
	now := time.Now()
	codeHash := staffTokenHash(request.Code)
	connection, err := s.store.ValidateGrant(ctx, codeHash, clientID, request.RedirectURI, request.CodeVerifier, now)
	if err != nil {
		staffTokenError(w, err)
		return
	}
	if err := s.verifyConnection(ctx, connection); err != nil {
		staffTokenError(w, err)
		return
	}
	access, refresh, session, err := s.mintPair(connection, clientID, now)
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint staff token")
		return
	}
	if err := s.store.ExchangeGrant(ctx, codeHash, clientID, request.RedirectURI, request.CodeVerifier, session, now); err != nil {
		staffTokenError(w, err)
		return
	}
	staffJSON(w, http.StatusOK, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": int64(session.ExpiresAt.Sub(now).Seconds()), "scope": "admin:read"})
}

func (s *StaffOAuthTokens) refresh(w http.ResponseWriter, ctx context.Context, clientID string, request *usersessions.RefreshTokenRequest) {
	now := time.Now()
	hash := staffTokenHash(request.RefreshToken)
	connection, err := s.store.PrepareRefresh(ctx, hash, clientID, now)
	if err != nil {
		staffTokenError(w, err)
		return
	}
	if err := s.verifyConnection(ctx, connection); err != nil {
		staffTokenError(w, err)
		return
	}
	access, refresh, session, err := s.mintPair(connection, clientID, now)
	if err != nil {
		staffOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint staff token")
		return
	}
	if err := s.store.RotateRefresh(ctx, hash, clientID, connection, session, now); err != nil {
		staffTokenError(w, err)
		return
	}
	staffJSON(w, http.StatusOK, map[string]any{"access_token": access, "refresh_token": refresh, "token_type": "Bearer", "expires_in": int64(session.ExpiresAt.Sub(now).Seconds()), "scope": "admin:read"})
}

func (s *StaffOAuthTokens) verifyConnection(ctx context.Context, connection staffTokenConnection) error {
	if connection.ResourceURI != s.resource || connection.SessionEnc == "" || !hasReadScope(connection.Scopes) || !time.Now().Before(connection.AuthorizedTil) {
		return errStaffGrant
	}
	subject, err := urn.ParseSessionSubject(connection.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return errStaffGrant
	}
	sessionID, err := s.cipher.Decrypt(connection.SessionEnc)
	if err != nil || sessionID == "" {
		return errStaffGrant
	}
	verified, err := s.verifier.Authorize(ctx, sessionID, &security.APIKeyScheme{Name: constants.AdminAuthSecurityScheme}) //nolint:exhaustruct // Only name is used.
	if err != nil {
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
			return errStaffGrant
		}
		return ErrAuthUnavailable
	}
	staff, ok := contextvalues.GetAdminAuthContext(verified)
	if !ok || staff == nil || staff.SessionID != sessionID || staff.OIDCSubject != subject.ID || staff.Email == "" {
		return errStaffGrant
	}
	return nil
}

func (s *StaffOAuthTokens) mintPair(connection staffTokenConnection, clientID string, now time.Time) (string, string, staffIssuedSession, error) {
	subject, err := urn.ParseSessionSubject(connection.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		return "", "", staffIssuedSession{}, errStaffGrant
	}
	jti, err := staffOpaqueToken()
	if err != nil {
		return "", "", staffIssuedSession{}, err
	}
	refresh, err := staffOpaqueToken()
	if err != nil {
		return "", "", staffIssuedSession{}, err
	}
	accessExpiry := minStaffTime(now.Add(staffAccessLifetime), connection.AuthorizedTil)
	refreshExpiry := minStaffTime(now.Add(staffRefreshLifetime), connection.AuthorizedTil)
	access, _, err := s.signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: s.resource, Issuer: s.issuer, Lifetime: 0, ExpiresAt: &accessExpiry, ClientID: clientID, JTI: jti})
	if err != nil {
		return "", "", staffIssuedSession{}, fmt.Errorf("mint staff access token: %w", err)
	}
	return access, refresh, staffIssuedSession{ID: uuid.New(), JTI: jti, RefreshHash: staffTokenHash(refresh), ExpiresAt: accessExpiry, RefreshTil: refreshExpiry}, nil
}

func minStaffTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func staffTokenError(w http.ResponseWriter, err error) {
	if errors.Is(err, errStaffGrant) || errors.Is(err, errStaffRefreshReuse) {
		staffOAuthError(w, http.StatusBadRequest, "invalid_grant", "staff authorization is invalid or expired")
	} else {
		staffOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "staff authorization state is unavailable")
	}
}
