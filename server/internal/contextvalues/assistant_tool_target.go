package contextvalues

import (
	"context"

	"github.com/google/uuid"
)

type assistantToolTargetKey struct{}

// SetAssistantToolTarget records an explicitly authorized tool target, not an
// authenticated assistant identity. Authorization must continue to use the
// caller's original actor, credentials, grants, and AssistantPrincipal.
func SetAssistantToolTarget(ctx context.Context, target AssistantPrincipal) context.Context {
	return context.WithValue(ctx, assistantToolTargetKey{}, target)
}

// GetAssistantToolTarget returns automatic thread context when attached,
// otherwise the explicit tool target, otherwise an assistant-scoped principal
// without a thread. Use this for tool targeting and target attribution only,
// never to classify or authorize the caller.
func GetAssistantToolTarget(ctx context.Context) (AssistantPrincipal, bool) {
	principal, hasPrincipal := GetAssistantPrincipal(ctx)
	if hasPrincipal && principal.ThreadID != uuid.Nil {
		return principal, true
	}
	if target, ok := ctx.Value(assistantToolTargetKey{}).(AssistantPrincipal); ok {
		return target, true
	}
	return principal, hasPrincipal
}
