// Package mcpidentity carries trusted authentication provenance for MCP
// requests. An Identity records how the serving surface established the
// caller's identity, so downstream enforcement can distinguish an
// authoritative acting Gram user from credentials that merely prove an
// organization, a machine, or nothing at all.
//
// Only the code that validates a credential may stamp an Identity. It must
// never be inferred after the fact from fields on an AuthContext, an API-key
// creator, a credential owner, an email, an external user ID, or any
// caller-provided value.
package mcpidentity

import "context"

// Kind is the bounded provenance class of a validated MCP credential.
type Kind string

const (
	// KindUserSession marks a validated Gram user-session token whose subject
	// is a concrete user. This is the only kind that identifies an
	// authoritative acting user.
	KindUserSession Kind = "user_session"

	// KindAnonymous marks a validated session whose subject is anonymous.
	KindAnonymous Kind = "anonymous"

	// KindAPIKey marks a validated session whose subject is an API key. The
	// key's creator or owner is not an acting user.
	KindAPIKey Kind = "api_key"

	// KindAssistant marks a validated assistant-runtime token. Its user claim
	// attributes the assistant's work but is not an authoritative acting-user
	// proof for enforcement.
	KindAssistant Kind = "assistant"

	// KindChatSession marks a validated chat-session token minted for an
	// embedded chat surface. Its claims may name a Gram user or an external
	// end-user for attribution, but the credential proves only the session,
	// so it is never an authoritative acting user.
	KindChatSession Kind = "chat_session"
)

// Identity is the provenance stamped by a serving surface after credential
// validation.
type Identity struct {
	// Kind is the provenance class of the validated credential.
	Kind Kind

	// UserID is the concrete Gram user ID and is set only for
	// KindUserSession.
	UserID string
}

// AuthenticatedUser builds the provenance for a validated user-session
// subject with a concrete user ID.
func AuthenticatedUser(userID string) Identity {
	return Identity{Kind: KindUserSession, UserID: userID}
}

type contextKey struct{}

// WithIdentity returns a context carrying the validated provenance.
func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, identity)
}

// FromContext returns the stamped provenance. A false result means the
// surface never established provenance; callers must treat the request as
// unattributed rather than assuming any identity.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok
}
