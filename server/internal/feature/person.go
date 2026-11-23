package feature

import (
	"context"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// FlagTarget evaluates a signed-in session as its raw email (the dashboard's distinct ID) with a lowercased
// "email"; anyone else, API keys included, is the organization with an empty "email" that never matches.
func FlagTarget(ctx context.Context, organizationID string) (distinctID string, personProperties map[string]string, person bool) {
	if contextvalues.HasValidatedGramSession(ctx) {
		authCtx, _ := contextvalues.GetAuthContext(ctx)
		if authCtx.Email != nil {
			if email := strings.ToLower(strings.TrimSpace(*authCtx.Email)); email != "" {
				return *authCtx.Email, map[string]string{"email": email}, true
			}
		}
	}
	return organizationID, map[string]string{"email": ""}, false
}
