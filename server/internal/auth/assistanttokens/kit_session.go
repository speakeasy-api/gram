package assistanttokens

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	tokenrepo "github.com/speakeasy-api/gram/server/internal/auth/assistanttokens/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// BindKitSession resolves Kit's existing provider attribution to the control
// plane's chat. The guest does not receive or persist a control-plane chat ID.
// The verified workload authorizes a runtime generation, not an individual invocation.
// Session attribution selects only a persisted binding inside that authority;
// it neither grants cross-runtime access nor asserts per-turn isolation.
func (m *Manager) BindKitSession(ctx context.Context, providerID uuid.UUID) (context.Context, error) {
	identity, ok := ctx.Value(inferenceContextKey{}).(assistantidentity.Identity)
	runtimeID, hasRuntime := ctx.Value(inferenceRuntimeContextKey{}).(uuid.UUID)
	workload, hasWorkload := ctx.Value(inferenceWorkloadContextKey{}).([]byte)
	if !hasWorkload || !ok || !hasRuntime || runtimeID == uuid.Nil || providerID == uuid.Nil {
		return ctx, oops.C(oops.CodeForbidden)
	}
	if err := new(assistantidentity.Service).Validate(ctx, m.db, identity); err != nil {
		return ctx, oops.C(oops.CodeUnauthorized)
	}
	bindings, err := m.tokens.ListKitInferenceSessionBindings(ctx, tokenrepo.ListKitInferenceSessionBindingsParams{
		WorkloadBinding: workload,
		RuntimeID:       uuid.NullUUID{UUID: runtimeID, Valid: true},
		ProjectID:       uuid.NullUUID{UUID: identity.ProjectID, Valid: true},
		AssistantID:     uuid.NullUUID{UUID: identity.AssistantID, Valid: true},
	})
	if err != nil {
		return ctx, fmt.Errorf("resolve Kit inference session: %w", err)
	}
	for _, binding := range bindings {
		if kitProviderSessionID(binding.AcpSessionID) != providerID {
			continue
		}
		ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: identity.AssistantID, ThreadID: binding.ThreadID})
		return context.WithValue(ctx, runtimeChatKey{}, binding.ChatID), nil
	}
	return ctx, oops.C(oops.CodeForbidden)
}

// Matches Kit's existing Speakeasy provider and chat.SessionIDToChatID. UUID
// session IDs pass through; opaque IDs use UUIDv5 with the DNS namespace.
func kitProviderSessionID(sessionID string) uuid.UUID {
	if id, err := uuid.Parse(sessionID); err == nil {
		return id
	}
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(sessionID))
}
