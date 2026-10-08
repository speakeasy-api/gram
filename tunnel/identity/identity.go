// Package identity defines the wire contract of the signed caller assertion
// Speakeasy sends through private tunnels: its header, token type, and the
// claims the tunnel agent relies on to deliver per-user upstream credentials.
// Speakeasy mints the assertion and the tunnel agent verifies it, so both
// sides share these definitions.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Header carries the assertion, without a Bearer prefix.
const Header = "X-Speakeasy-Identity"

// TokenType is the assertion's protected `typ` header.
const TokenType = "speakeasy-identity+jwt"

// Version is the assertion contract version. Claims added since version 1
// are additive, so it stays 1: verifiers that require version 1 keep working.
const Version = 1

// MaxLifetime bounds an assertion's validity window, `exp - iat`.
const MaxLifetime = time.Minute

// ClockSkew is the tolerance a verifier allows on assertion times.
const ClockSkew = 5 * time.Second

// Credential owners. A subject credential was granted by the assertion's
// subject through a per-user authorization; a self credential is the remote
// session client's own, obtained without the subject.
const (
	OwnerSubject = "subject"
	OwnerSelf    = "self"
)

// Claim names the tunnel agent reads.
const (
	ClaimMCPServerID        = "mcp_server_id"
	ClaimUpstreamCredential = "upstream_credential"
	ClaimAllowedMethods     = "allowed_methods"
	ClaimOrganizationID     = "organization_id"
	ClaimVersion            = "version"
)

// UpstreamCredential describes the upstream bearer forwarded in the
// request's Authorization header. Speakeasy derives it from the credential it
// resolved for the request, never from a configured header, and only when
// that credential is a remote session grant or a self client credential.
type UpstreamCredential struct {
	// Owner is OwnerSubject or OwnerSelf.
	Owner string `json:"owner"`

	// ClientID is the remote session client the credential belongs to.
	ClientID string `json:"client_id"`

	// GrantID is the remote session grant row the token came from. Present
	// only for subject credentials.
	GrantID string `json:"grant_id,omitempty"`

	// GrantGeneration advances when the subject authorizes the grant again,
	// for example to link another upstream account, and not on refresh.
	// Present, and at least 1, only for subject credentials.
	GrantGeneration int64 `json:"grant_generation,omitempty"`

	// TokenSHA256 is TokenSHA256 of the forwarded bearer token.
	TokenSHA256 string `json:"token_sha256"`

	// TokenExpiresAt is the token's expiry in Unix seconds. Omitted when the
	// upstream did not state one.
	TokenExpiresAt *int64 `json:"token_expires_at,omitempty"`
}

// TokenSHA256 returns the lowercase hex SHA-256 of a bearer token's raw bytes,
// excluding the "Bearer " scheme.
func TokenSHA256(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ReservedHeader also matches spellings with underscores for any dash: some
// servers fold underscores into dashes, which would let a forged alias through.
func ReservedHeader(name string) bool {
	return strings.EqualFold(strings.ReplaceAll(name, "_", "-"), Header)
}
