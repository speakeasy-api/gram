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
	revalidate     func() error
}
type assistantBusinessInvocationKey struct{}

func WithAssistantBusinessInvocation(ctx context.Context, org string, project uuid.UUID, user string, revalidate func() error) context.Context {
	return context.WithValue(ctx, assistantBusinessInvocationKey{}, AssistantBusinessInvocation{OrganizationID: org, ProjectID: project, UserID: user, revalidate: revalidate})
}
func AssistantBusinessInvocationFromContext(ctx context.Context) (AssistantBusinessInvocation, bool) {
	invocation, ok := ctx.Value(assistantBusinessInvocationKey{}).(AssistantBusinessInvocation)
	return invocation, ok
}
func (i AssistantBusinessInvocation) Revalidate() error {
	if i.revalidate == nil {
		return fmt.Errorf("assistant business invocation unavailable")
	}
	return i.revalidate()
}
