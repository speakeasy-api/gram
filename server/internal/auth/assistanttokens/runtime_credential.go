package assistanttokens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	gramcache "github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// runtimeBindingSkewBuffer extends the Redis runtime-binding TTL past the
// minted credential's expiry so minor clock skew never orphans a live token.
const runtimeBindingSkewBuffer = time.Minute

// RuntimeBinding is the assistant thread a turn's principal credential was
// minted for. The credential itself names only its principal; the runtime
// routes look the binding up by credential ID.
type RuntimeBinding struct {
	OrganizationID  string    `json:"organization_id"`
	ProjectID       uuid.UUID `json:"project_id"`
	AssistantID     uuid.UUID `json:"assistant_id"`
	ThreadID        uuid.UUID `json:"thread_id"`
	ChatID          uuid.UUID `json:"chat_id"`
	TranscriptOwned bool      `json:"transcript_owned,omitempty"`
}

type runtimeChatKey struct{}
type runtimeTranscriptOwnedKey struct{}

// RuntimeChatID is the chat of the thread a runtime credential is bound to.
func RuntimeChatID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(runtimeChatKey{}).(uuid.UUID)
	return id, ok
}

// IsRuntimeCredential reports whether an Authorization header carries a
// principal credential rather than a legacy assistant runtime token.
func IsRuntimeCredential(header string) bool {
	return principalcredential.IsToken(BearerToken(header))
}

// BearerToken extracts the raw token from an Authorization header value,
// stripping an optional case-insensitive "Bearer " prefix.
func BearerToken(header string) string {
	header = strings.TrimSpace(header)
	if len(header) > len("bearer ") && strings.EqualFold(header[:len("bearer ")], "bearer ") {
		return strings.TrimSpace(header[len("bearer "):])
	}
	return header
}

func runtimeBindingKey(credentialID string) string {
	return "assistant-runtime-credential:" + credentialID
}

// MintTurnCredential mints the principal credential an assistant turn runs
// with and records the thread it belongs to for the runtime routes.
func (m *Manager) MintTurnCredential(ctx context.Context, credential principalcredential.Credential, binding RuntimeBinding) (string, error) {
	raw, _, err := m.MintTurnCredentialWithLifetime(ctx, credential, binding, principalcredential.Lifetime)
	return raw, err
}

// MintTurnCredentialWithLifetime mints a turn principal credential with an
// explicit lifetime and records its thread binding in cache for lifetime plus
// runtimeBindingSkewBuffer so the binding always covers the live credential.
func (m *Manager) MintTurnCredentialWithLifetime(ctx context.Context, credential principalcredential.Credential, binding RuntimeBinding, lifetime time.Duration) (string, time.Time, error) {
	if binding.OrganizationID != credential.OrganizationID || binding.ProjectID != credential.ProjectID {
		return "", time.Time{}, principalcredential.ErrInvalid
	}
	raw, id, expiresAt, err := m.credentials.MintWithLifetime(credential, lifetime)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("mint turn credential: %w", err)
	}
	if err := m.runtimeBindings.Set(ctx, runtimeBindingKey(id), binding, lifetime+runtimeBindingSkewBuffer); err != nil {
		return "", time.Time{}, fmt.Errorf("record turn credential binding: %w", err)
	}
	return raw, expiresAt, nil
}

// BindRuntime attaches the assistant thread and chat recorded for an
// already-authenticated principal credential, if one was minted as an assistant
// turn credential. A principal credential with no runtime binding is returned
// unchanged; a binding whose assistant or thread has been deleted or paused is
// rejected as unauthorized.
func (m *Manager) BindRuntime(ctx context.Context) (context.Context, error) {
	credential, ok := principalcredential.FromContext(ctx)
	if !ok || m == nil || m.runtimeBindings == nil || m.runtimeBindings == gramcache.NoopCache {
		return ctx, nil
	}
	binding, found, err := m.loadRuntimeBinding(ctx, credential, false)
	if err != nil || !found {
		return ctx, err
	}
	return attachRuntimeBinding(ctx, binding), nil
}

// AuthorizeRuntime authorizes runner calls that serve the assistant itself:
// bootstrap, compaction persistence, MCP OAuth flow creation, model inference
// and the platform toolset. A principal credential is authenticated as its
// principal and bound to the thread it was minted for; any other token goes
// through Authorize.
func (m *Manager) AuthorizeRuntime(ctx context.Context, header string) (context.Context, *Claims, error) {
	if !IsRuntimeCredential(header) {
		return m.Authorize(ctx, header)
	}
	raw := BearerToken(header)
	authed, err := m.credentials.Authenticate(ctx, raw)
	if errors.Is(err, principalcredential.ErrInvalid) || errors.Is(err, principalcredential.ErrNotCredential) {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid runtime credential")
	}
	if err != nil {
		return ctx, nil, fmt.Errorf("authenticate runtime credential: %w", err)
	}
	credential, _ := principalcredential.FromContext(authed)
	binding, _, err := m.loadRuntimeBinding(authed, credential, true)
	if err != nil {
		return ctx, nil, err
	}
	authed, err = m.authz.PrepareContext(authed)
	if err != nil {
		if denied, ok := errors.AsType[*oops.ShareableError](err); ok && (denied.Code == oops.CodeUnauthorized || denied.Code == oops.CodeForbidden) {
			return ctx, nil, oops.E(denied.Code, err, "runtime credential is no longer admitted")
		}
		return ctx, nil, oops.E(oops.CodeUnexpected, err, "admit runtime credential")
	}
	authed = attachRuntimeBinding(authed, binding)
	userID := credential.Credential.AuthorizerUserID
	var claims Claims
	claims.OrgID = binding.OrganizationID
	claims.ProjectID = binding.ProjectID.String()
	claims.AssistantID = binding.AssistantID.String()
	claims.ThreadID = binding.ThreadID.String()
	claims.UserID = userID
	return authed, &claims, nil
}

func (m *Manager) loadRuntimeBinding(ctx context.Context, credential principalcredential.Authenticated, required bool) (RuntimeBinding, bool, error) {
	var binding RuntimeBinding
	if m.runtimeBindings == nil {
		if required {
			return binding, false, oops.E(oops.CodeUnauthorized, nil, "runtime credential is not bound to an assistant thread")
		}
		return binding, false, nil
	}
	if err := m.runtimeBindings.Get(ctx, runtimeBindingKey(credential.ID), &binding); err != nil {
		if !required && errors.Is(err, cache.ErrCacheMiss) {
			return binding, false, nil
		}
		if required {
			return binding, false, oops.E(oops.CodeUnauthorized, err, "runtime credential is not bound to an assistant thread")
		}
		return binding, false, fmt.Errorf("read runtime credential binding: %w", err)
	}
	if binding.OrganizationID != credential.Credential.OrganizationID || binding.ProjectID != credential.Credential.ProjectID {
		return binding, false, oops.C(oops.CodeUnauthorized)
	}
	lifecycle, err := m.tokens.GetAssistantTokenRevocation(ctx, tokenrepo.GetAssistantTokenRevocationParams{ProjectID: binding.ProjectID, AssistantID: binding.AssistantID, ThreadID: binding.ThreadID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return binding, false, oops.E(oops.CodeUnauthorized, err, "runtime credential thread unavailable")
		}
		return binding, false, fmt.Errorf("check runtime credential thread revocation: %w", err)
	}
	if lifecycle.ThreadDeleted || lifecycle.AssistantDeleted || lifecycle.AssistantStatus != "active" {
		return binding, false, oops.C(oops.CodeUnauthorized)
	}
	return binding, true, nil
}

func attachRuntimeBinding(ctx context.Context, binding RuntimeBinding) context.Context {
	ctx = context.WithValue(ctx, runtimeChatKey{}, binding.ChatID)
	if binding.TranscriptOwned {
		ctx = context.WithValue(ctx, runtimeTranscriptOwnedKey{}, true)
	}
	return contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: binding.AssistantID, ThreadID: binding.ThreadID})
}
