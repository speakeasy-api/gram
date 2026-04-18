package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	"github.com/speakeasy-api/gram/server/internal/cache"
)

// loginStateTTL bounds the time an admin login flow is permitted to take.
const loginStateTTL = 10 * time.Minute

// LoginState is the short-lived record persisted while an admin login is in
// flight. It ties the random `state` query param to the PKCE verifier and
// the origin of the request.
type LoginState struct {
	State        string    `json:"state"`
	CodeVerifier string    `json:"code_verifier"`
	ReturnTo     string    `json:"return_to"`
	CreatedAt    time.Time `json:"created_at"`
}

var _ cache.CacheableObject[LoginState] = (*LoginState)(nil)

func LoginStateCacheKey(state string) string {
	return "adminLoginState:" + state
}

func (s LoginState) CacheKey() string              { return LoginStateCacheKey(s.State) }
func (s LoginState) AdditionalCacheKeys() []string { return []string{} }
func (s LoginState) TTL() time.Duration            { return loginStateTTL }

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// sanitizeReturnTo ensures the post-login redirect target stays on the
// current origin so the login flow cannot be used as an open redirect.
func sanitizeReturnTo(raw, fallback string) string {
	if raw == "" {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	// Only allow same-origin relative paths.
	if u.Scheme != "" || u.Host != "" {
		return fallback
	}
	if u.Path == "" {
		return fallback
	}
	return u.String()
}
