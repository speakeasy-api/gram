package feature

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// PersonTarget is a signed-in session's lowercased email; API keys are never a person.
// The dashboard buckets by raw email, so only percentage rollouts can differ by case.
func PersonTarget(ctx context.Context) (distinctID string, personProperties map[string]string, ok bool) {
	if !contextvalues.HasValidatedGramSession(ctx) {
		return "", nil, false
	}
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx.Email == nil {
		return "", nil, false
	}
	email := strings.ToLower(strings.TrimSpace(*authCtx.Email))
	if email == "" {
		return "", nil, false
	}
	return email, map[string]string{"email": email}, true
}
