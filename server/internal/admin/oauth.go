package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"slices"
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

// sanitizeReturnTo ensures the post-login redirect target is safe.
// Relative paths are allowed unconditionally. Absolute URLs are allowed only
// when their origin (scheme + "://" + host) appears in allowedOrigins, which
// prevents open-redirect abuse while still allowing the Registry admin SPA
// (a different domain) to be the post-auth landing page.
func sanitizeReturnTo(raw, fallback string, allowedOrigins []string) string {
	if raw == "" {
		return fallback
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	if u.Scheme == "" && u.Host == "" {
		// Relative path — allow as long as there is a path.
		if u.Path == "" {
			return fallback
		}
		return u.String()
	}
	// Absolute URL — only allow if the origin is explicitly permitted.
	origin := u.Scheme + "://" + u.Host
	if slices.Contains(allowedOrigins, origin) {
		return u.String()
	}
	return fallback
}
