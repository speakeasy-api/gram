package contextvalues

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

type assistantInvocationEventKey struct{}

// WithAssistantInvocationEvent pins the trusted originating event for bounded
// assistant-owned replies. It carries no user-selected target or reply content.
func WithAssistantInvocationEvent(ctx context.Context, event string) context.Context {
	return context.WithValue(ctx, assistantInvocationEventKey{}, event)
}
func AssistantInvocationEvent(ctx context.Context) (string, bool) {
	event, ok := ctx.Value(assistantInvocationEventKey{}).(string)
	return event, ok
}

// AssistantBusinessInvocation is a request-local credential-source pin, not a
// user authentication stamp. The private callback reloads execution authority
// after a blocking credential refresh.
type AssistantBusinessInvocation struct {
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	Resource       string
	revalidate     func(context.Context) (context.Context, error)
}
type assistantBusinessInvocationKey struct{}

func WithAssistantBusinessInvocation(ctx context.Context, org string, project uuid.UUID, user string, revalidate func(context.Context) (context.Context, error)) context.Context {
	return context.WithValue(ctx, assistantBusinessInvocationKey{}, AssistantBusinessInvocation{OrganizationID: org, ProjectID: project, UserID: user, Resource: "", revalidate: revalidate})
}

// WithAssistantBusinessResource pins the server-resolved upstream resource for
// this authorized MCP route. An unqualified route cannot select human tokens.
func WithAssistantBusinessResource(ctx context.Context, resource string) context.Context {
	invocation, ok := AssistantBusinessInvocationFromContext(ctx)
	if !ok {
		return ctx
	}
	invocation.Resource = resource
	return context.WithValue(ctx, assistantBusinessInvocationKey{}, invocation)
}

func AssistantBusinessInvocationFromContext(ctx context.Context) (AssistantBusinessInvocation, bool) {
	invocation, ok := ctx.Value(assistantBusinessInvocationKey{}).(AssistantBusinessInvocation)
	return invocation, ok
}
func (i AssistantBusinessInvocation) Revalidate(ctx context.Context) error {
	_, err := i.RevalidatedContext(ctx)
	return err
}

// RevalidatedContext preserves request metadata while replacing admitted policy.
func (i AssistantBusinessInvocation) RevalidatedContext(ctx context.Context) (context.Context, error) {
	if i.revalidate == nil {
		return ctx, fmt.Errorf("assistant business invocation unavailable")
	}
	fresh, err := i.revalidate(ctx)
	if err != nil {
		return ctx, err
	}
	return WithAssistantBusinessResource(fresh, i.Resource), nil
}
