package assistanttokens

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RuntimeBinding is the assistant thread a turn's principal credential was
// minted for. The credential itself names only its principal; the runtime
// routes look the binding up by credential ID.
type RuntimeBinding struct {
	OrganizationID string    `json:"organization_id"`
	ProjectID      uuid.UUID `json:"project_id"`
	AssistantID    uuid.UUID `json:"assistant_id"`
	ThreadID       uuid.UUID `json:"thread_id"`
	ChatID         uuid.UUID `json:"chat_id"`
}

type runtimeChatKey struct{}

// RuntimeChatID is the chat of the thread a runtime credential is bound to.
func RuntimeChatID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(runtimeChatKey{}).(uuid.UUID)
	return id, ok
}

// IsRuntimeCredential reports whether an Authorization header carries a
// principal credential rather than a legacy assistant runtime token.
func IsRuntimeCredential(header string) bool {
	return principalcredential.IsToken(bearerToken(header))
}

func bearerToken(header string) string {
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
	if binding.OrganizationID != credential.OrganizationID || binding.ProjectID != credential.ProjectID {
		return "", principalcredential.ErrInvalid
	}
	raw, id, err := m.credentials.Mint(credential)
	if err != nil {
		return "", fmt.Errorf("mint turn credential: %w", err)
	}
	if err := m.runtimeBindings.Set(ctx, runtimeBindingKey(id), binding, principalcredential.Lifetime); err != nil {
		return "", fmt.Errorf("record turn credential binding: %w", err)
	}
	return raw, nil
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
	raw := bearerToken(header)
	authed, err := m.credentials.Authenticate(ctx, raw)
	if errors.Is(err, principalcredential.ErrInvalid) || errors.Is(err, principalcredential.ErrNotCredential) {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "invalid runtime credential")
	}
	if err != nil {
		return ctx, nil, fmt.Errorf("authenticate runtime credential: %w", err)
	}
	credential, _ := principalcredential.FromContext(authed)
	var binding RuntimeBinding
	if err := m.runtimeBindings.Get(ctx, runtimeBindingKey(credential.ID), &binding); err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "runtime credential is not bound to an assistant thread")
	}
	if binding.OrganizationID != credential.Credential.OrganizationID || binding.ProjectID != credential.Credential.ProjectID {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	lifecycle, err := m.tokens.GetAssistantTokenRevocation(ctx, tokenrepo.GetAssistantTokenRevocationParams{ProjectID: binding.ProjectID, AssistantID: binding.AssistantID, ThreadID: binding.ThreadID})
	if err != nil {
		return ctx, nil, oops.E(oops.CodeUnauthorized, err, "runtime credential thread unavailable")
	}
	if lifecycle.ThreadDeleted || lifecycle.AssistantDeleted || lifecycle.AssistantStatus != "active" {
		return ctx, nil, oops.C(oops.CodeUnauthorized)
	}
	authed = context.WithValue(authed, runtimeChatKey{}, binding.ChatID)
	authed = contextvalues.SetAssistantPrincipal(authed, contextvalues.AssistantPrincipal{AssistantID: binding.AssistantID, ThreadID: binding.ThreadID})
	userID := credential.Credential.AuthorizerUserID
	if credential.Credential.Principal.Type == urn.PrincipalTypeAgent {
		authed = contextvalues.WithAssistantInvoker(authed, userID)
	}
	var claims Claims
	claims.OrgID = binding.OrganizationID
	claims.ProjectID = binding.ProjectID.String()
	claims.AssistantID = binding.AssistantID.String()
	claims.ThreadID = binding.ThreadID.String()
	claims.UserID = userID
	return authed, &claims, nil
}
