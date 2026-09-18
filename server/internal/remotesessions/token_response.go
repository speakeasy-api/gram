package remotesessions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/oauth/jwtclaims"
)

// tokenResponse is the slice of the upstream /token reply we care about.
// RFC 6749 fields, draft-ietf-oauth-refresh-token-expiration fields, and two
// common non-standard refresh lifetime aliases.
type tokenResponse struct {
	AccessToken            string `json:"access_token"`
	RefreshToken           string `json:"refresh_token"`
	TokenType              string `json:"token_type"`
	ExpiresIn              int    `json:"expires_in"`
	RefreshTokenTimeout    *int64 `json:"refresh_token_timeout"`
	AuthorizationExpiresIn *int64 `json:"authorization_expires_in"`
	RefreshExpiresIn       int64  `json:"refresh_expires_in"`
	RefreshTokenExpiresIn  int64  `json:"refresh_token_expires_in"`
	Scope                  string `json:"scope"`
	scopePresent           bool

	// IDToken is verified and reduced to claims at the exchange; never persisted or logged.
	IDToken string `json:"id_token"`

	// raw is the response body the fields above were decoded from, kept so
	// the non-standard members a provider adds can be retained.
	raw []byte
}

func (t *tokenResponse) UnmarshalJSON(data []byte) error {
	type wire tokenResponse
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil {
		return fmt.Errorf("decode token response members: %w", err)
	}
	// A null scope is an unreported one; any other non-string is malformed.
	scope, scopePresent := members["scope"]
	if scopePresent {
		scope = bytes.TrimSpace(scope)
		switch {
		case string(scope) == "null":
			scopePresent = false
		case len(scope) == 0 || scope[0] != '"':
			return errors.New("token response scope must be a string")
		}
	}
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	*t = tokenResponse(decoded)
	t.scopePresent = scopePresent
	return nil
}

// tokenResponseRetainedMembers are the provider metadata members a token
// response may keep, matched by lower-cased name; anything else is dropped
// unread, since no name rule can prove an arbitrary member holds no secret.
var tokenResponseRetainedMembers = map[string]struct{}{
	// Notion
	"workspace_name": {}, "workspace_id": {}, "workspace_icon": {}, "bot_id": {}, "owner": {}, "duplicated_template_id": {},
	// Slack
	"team": {}, "enterprise": {}, "authed_user": {}, "bot_user_id": {}, "app_id": {}, "is_enterprise_install": {},
	// PostHog
	"posthog_region": {},
	// HubSpot
	"hub_id": {}, "hub_domain": {},
	// Salesforce
	"instance_url": {},
	// Tenant identifiers several providers share
	"account_id": {}, "tenant_id": {}, "organization_id": {}, "login": {},
}

// extras returns the allowlisted members with credential-shaped members
// stripped at every nesting level (Slack nests tokens under authed_user). Nil when empty.
func (t tokenResponse) extras() map[string]json.RawMessage {
	// UseNumber keeps large integers as the provider wrote them.
	dec := json.NewDecoder(bytes.NewReader(t.raw))
	dec.UseNumber()
	var members map[string]any
	if err := dec.Decode(&members); err != nil {
		return nil
	}
	for name := range members {
		if _, keep := tokenResponseRetainedMembers[strings.ToLower(name)]; !keep {
			delete(members, name)
		}
	}
	stripCredentialMembers(members)
	if len(members) == 0 {
		return nil
	}
	kept := make(map[string]json.RawMessage, len(members))
	for name, value := range members {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		kept[name] = raw
	}
	return kept
}

// stripCredentialMembers removes credential-shaped members from doc and from
// every object nested under it.
func stripCredentialMembers(doc map[string]any) {
	for name, value := range doc {
		if credentialMemberName(name) {
			delete(doc, name)
			continue
		}
		stripNestedCredentialMembers(value)
	}
}

func stripNestedCredentialMembers(value any) {
	switch v := value.(type) {
	case map[string]any:
		stripCredentialMembers(v)
	case []any:
		for _, item := range v {
			stripNestedCredentialMembers(item)
		}
	}
}

// credentialMemberWords mark a member as credential material anywhere in its
// name; benign matches (country_code) are dropped too.
var credentialMemberWords = []string{"token", "secret", "assertion", "code", "password", "passwd", "credential", "key", "jwt", "webhook", "authorization", "session", "cookie", "signature"}

// credentialMemberName reports whether a member name looks like it names a
// credential, regardless of case.
func credentialMemberName(name string) bool {
	lower := strings.ToLower(name)
	for _, word := range credentialMemberWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// Scopes splits the space-delimited scope value per RFC 6749 §3.3, tolerating
// repeated or leading/trailing whitespace: strings.Fields collapses whitespace
// runs and drops empties, so no blank scope is ever persisted.
func (t tokenResponse) Scopes() []string {
	return strings.Fields(t.Scope)
}

// ScopeReported distinguishes omission from Gram's compatibility exception: an explicitly empty token-response scope.
func (t tokenResponse) ScopeReported() bool {
	return t.scopePresent
}

// RefreshTokenTimeoutSeconds normalizes the standard sliding idle timeout and
// two common provider aliases. The boolean distinguishes an omitted value from
// an explicit zero-second timeout.
func (t tokenResponse) RefreshTokenTimeoutSeconds() (int64, bool) {
	if t.RefreshTokenTimeout != nil {
		return *t.RefreshTokenTimeout, true
	}
	if t.RefreshExpiresIn > 0 {
		return t.RefreshExpiresIn, true
	}
	if t.RefreshTokenExpiresIn > 0 {
		return t.RefreshTokenExpiresIn, true
	}
	return 0, false
}

// AccessExpiresAt is the access token's deadline, or nil when the provider
// reported none. expires_in (RFC 6749 §5.1) governs when present. When it is
// omitted and the access token is itself a JWT, the token's exp claim is the
// provider's own statement of the deadline: RFC 9068 §2.2 makes exp mandatory
// on JWT access tokens, and some providers rely on it instead of ever sending
// expires_in.
//
// A zero expires_in is treated as unreported. Read literally it would describe
// a token that expired as it was issued, turning every request into a refresh
// grant — or a reconnect prompt when no refresh token exists — for providers
// that send 0 to mean the token never expires.
//
// The JWT is decoded without signature verification. exp only schedules a
// refresh and never authorizes anything, so a forged value can at worst move
// one refresh attempt earlier or later.
func (t tokenResponse) AccessExpiresAt(now time.Time) *time.Time {
	if t.ExpiresIn > 0 {
		deadline := now.Add(time.Duration(t.ExpiresIn) * time.Second)
		return &deadline
	}

	exp, ok := jwtclaims.UnsafeExtractExpiry(t.AccessToken)
	if !ok {
		return nil
	}
	return &exp
}

// AuthorizationLifetimeSeconds is the remaining absolute lifetime of the
// upstream authorization, when the provider reports it.
func (t tokenResponse) AuthorizationLifetimeSeconds() (int64, bool) {
	if t.AuthorizationExpiresIn == nil {
		return 0, false
	}
	return *t.AuthorizationExpiresIn, true
}

func expirationDeadline(now time.Time, seconds int64, reported bool) *time.Time {
	if !reported {
		return nil
	}
	deadline := now.Add(time.Duration(seconds) * time.Second)
	return &deadline
}
