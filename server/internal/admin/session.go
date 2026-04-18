package admin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

// SessionMiddleware forwards the opaque value of the gram_admin
// cookie into the request context so the admin Goa authorizer can find it
// (Goa does not natively bind APIKey security schemes to cookies). It does
// not validate the session; that happens inside the Verifier.
func SessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(constants.AdminSessionCookie)
		if err == nil && cookie.Value != "" {
			ctx := contextvalues.SetAdminSessionTokenInContext(r.Context(), cookie.Value)
			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// adminSessionTTL is how long an admin session record is kept in the cache
// before it is evicted. The record is refreshed (cache write) on every
// request so an active admin member's session stays alive; an inactive
// session is eventually reclaimed.
const adminSessionTTL = 24 * time.Hour

// Session is the server-side record for an OIDC-authenticated admin user.
// The access and refresh tokens are stored encrypted so the cache contents are
// not directly usable if exposed.
type Session struct {
	SessionID            string    `json:"session_id"`
	Email                string    `json:"email"`
	Name                 string    `json:"name"`
	OIDCSubject          string    `json:"oidc_sub"`
	HD                   string    `json:"hd"`
	AccessTokenEnc       string    `json:"access_token_enc"`
	RefreshTokenEnc      string    `json:"refresh_token_enc"`
	AccessTokenExpiresAt time.Time `json:"access_token_expires_at"`
	CreatedAt            time.Time `json:"created_at"`
}

var _ cache.CacheableObject[Session] = (*Session)(nil)

func AdminSessionCacheKey(sessionID string) string {
	return "adminSession:" + sessionID
}

func (s Session) CacheKey() string {
	return AdminSessionCacheKey(s.SessionID)
}

func (s Session) AdditionalCacheKeys() []string {
	return []string{}
}

func (s Session) TTL() time.Duration {
	return adminSessionTTL
}

// newSessionID returns a URL-safe random identifier used as the opaque value
// of the admin session cookie.
func newSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SessionStore wraps the typed cache and the encryption client used to
// protect OIDC tokens at rest.
type SessionStore struct {
	cache cache.TypedCacheObject[Session]
	enc   *encryption.Client
}

func NewSessionStore(typed cache.TypedCacheObject[Session], enc *encryption.Client) *SessionStore {
	return &SessionStore{cache: typed, enc: enc}
}

type StoreParams struct {
	Email        string
	Name         string
	OIDCSubject  string
	HD           string
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// Store creates a new admin session, encrypts the oauth tokens, and writes
// the record to the cache. It returns the opaque session ID which should be
// written to the admin session cookie.
func (s *SessionStore) Store(ctx context.Context, p StoreParams) (string, error) {
	sessionID, err := newSessionID()
	if err != nil {
		return "", err
	}

	accessEnc, err := s.enc.Encrypt([]byte(p.AccessToken))
	if err != nil {
		return "", fmt.Errorf("encrypt access token: %w", err)
	}
	var refreshEnc string
	if p.RefreshToken != "" {
		refreshEnc, err = s.enc.Encrypt([]byte(p.RefreshToken))
		if err != nil {
			return "", fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	session := Session{
		SessionID:            sessionID,
		Email:                p.Email,
		Name:                 p.Name,
		OIDCSubject:          p.OIDCSubject,
		HD:                   p.HD,
		AccessTokenEnc:       accessEnc,
		RefreshTokenEnc:      refreshEnc,
		AccessTokenExpiresAt: p.ExpiresAt,
		CreatedAt:            time.Now().UTC(),
	}

	if err := s.cache.Store(ctx, session); err != nil {
		return "", fmt.Errorf("store admin session: %w", err)
	}
	return sessionID, nil
}

func (s *SessionStore) Get(ctx context.Context, sessionID string) (Session, error) {
	session, err := s.cache.Get(ctx, AdminSessionCacheKey(sessionID))
	if err != nil {
		return session, fmt.Errorf("load admin session: %w", err)
	}
	return session, nil
}

func (s *SessionStore) Delete(ctx context.Context, sessionID string) error {
	if err := s.cache.DeleteByKey(ctx, AdminSessionCacheKey(sessionID)); err != nil {
		return fmt.Errorf("delete admin session: %w", err)
	}
	return nil
}

// UpdateAccessToken re-encrypts and persists a freshly refreshed OAuth
// access token onto an existing session record.
func (s *SessionStore) UpdateAccessToken(ctx context.Context, session Session, accessToken string, expiresAt time.Time) (Session, error) {
	enc, err := s.enc.Encrypt([]byte(accessToken))
	if err != nil {
		return session, fmt.Errorf("encrypt access token: %w", err)
	}
	session.AccessTokenEnc = enc
	session.AccessTokenExpiresAt = expiresAt
	if err := s.cache.Store(ctx, session); err != nil {
		return session, fmt.Errorf("store admin session: %w", err)
	}
	return session, nil
}

// DecryptAccessToken returns the plaintext OAuth access token for the given
// session.
func (s *SessionStore) DecryptAccessToken(session Session) (string, error) {
	tok, err := s.enc.Decrypt(session.AccessTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt access token: %w", err)
	}
	return tok, nil
}

// DecryptRefreshToken returns the plaintext OAuth refresh token for the
// given session, or an empty string if no refresh token was captured.
func (s *SessionStore) DecryptRefreshToken(session Session) (string, error) {
	if session.RefreshTokenEnc == "" {
		return "", nil
	}
	tok, err := s.enc.Decrypt(session.RefreshTokenEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh token: %w", err)
	}
	return tok, nil
}
