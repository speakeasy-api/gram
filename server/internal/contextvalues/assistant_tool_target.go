package contextvalues

import "context"

type assistantToolTargetKey struct{}

// SetAssistantToolTarget records an explicitly authorized tool target, not an
// authenticated assistant identity. Authorization must continue to use the
// caller's original actor, credentials, grants, and AssistantPrincipal.
func SetAssistantToolTarget(ctx context.Context, target AssistantPrincipal) context.Context {
	return context.WithValue(ctx, assistantToolTargetKey{}, target)
}

// GetAssistantToolTarget returns automatic assistant context when attached,
// otherwise the explicit tool target. Use this for tool targeting and target
// attribution only, never to classify or authorize the caller.
func GetAssistantToolTarget(ctx context.Context) (AssistantPrincipal, bool) {
	if principal, ok := GetAssistantPrincipal(ctx); ok {
		return principal, true
	}
	target, ok := ctx.Value(assistantToolTargetKey{}).(AssistantPrincipal)
	return target, ok
}
