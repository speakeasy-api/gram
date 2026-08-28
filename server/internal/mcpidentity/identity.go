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

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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

// Identity is opaque validated provenance. Callers can inspect it but cannot
// construct or mutate it; only a ValidatorBoundary can stamp one after its
// owning authentication strategy accepts a credential.
type Identity struct {
	kind   Kind
	userID string
}

// Kind returns the validated credential class.
func (i Identity) Kind() Kind { return i.kind }

// UserID returns the concrete Gram user ID for KindUserSession and is empty
// for every other credential class.
func (i Identity) UserID() string { return i.userID }

// ValidatorBoundary is the capability held by the MCP credential validators.
// It deliberately exposes only bounded, strategy-specific stamps: there is no
// generic identity setter and no public constructor for user provenance.
type ValidatorBoundary struct {
	initialized bool
}

// NewValidatorBoundary creates a provenance capability for a credential
// validation owner. The zero value is inert.
func NewValidatorBoundary() *ValidatorBoundary {
	return &ValidatorBoundary{initialized: true}
}

type contextKey struct{}

func (b *ValidatorBoundary) withIdentity(ctx context.Context, kind Kind, userID string) context.Context {
	if b == nil || !b.initialized {
		return ctx
	}
	return context.WithValue(ctx, contextKey{}, Identity{kind: kind, userID: userID})
}

// StampValidatedSession records provenance from an opaque session proof returned
// by sessiontokens.Signer.ValidateBearer. Zero or malformed proofs leave the
// context unstamped.
func (b *ValidatorBoundary) StampValidatedSession(ctx context.Context, session sessiontokens.ValidatedSession) context.Context {
	if !session.Valid() {
		return ctx
	}
	subject := session.Subject()
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		if subject.ID == "" {
			return ctx
		}
		return b.withIdentity(ctx, KindUserSession, subject.ID)
	case urn.SessionSubjectKindAPIKey:
		return b.withIdentity(ctx, KindAPIKey, "")
	case urn.SessionSubjectKindAnonymous:
		return b.withIdentity(ctx, KindAnonymous, "")
	default:
		return ctx
	}
}

// StampAssistant records an accepted assistant-runtime credential.
func (b *ValidatorBoundary) StampAssistant(ctx context.Context) context.Context {
	return b.withIdentity(ctx, KindAssistant, "")
}

// StampAPIKey records an accepted Gram API key.
func (b *ValidatorBoundary) StampAPIKey(ctx context.Context) context.Context {
	return b.withIdentity(ctx, KindAPIKey, "")
}

// StampChatSession records an accepted embedded-chat session token.
func (b *ValidatorBoundary) StampChatSession(ctx context.Context) context.Context {
	return b.withIdentity(ctx, KindChatSession, "")
}

// FromContext returns the stamped provenance. A false result means the
// surface never established provenance; callers must treat the request as
// unattributed rather than assuming any identity.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	return identity, ok && identity.kind != ""
}
