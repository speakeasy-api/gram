package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestRedactSessionTitle(t *testing.T) {
	t.Parallel()
	for _, orgID := range []string{"org_fixture", constants.DemoOrganizationID} {
		for _, session := range []string{"ordinary", "support", "workos"} {
			t.Run(orgID+"/"+session, func(t *testing.T) {
				t.Parallel()
				authCtx := &contextvalues.AuthContext{ActiveOrganizationID: orgID, IsAdmin: true, SupportOrganizationID: orgID}
				ctx := contextvalues.SetAuthContext(t.Context(), authCtx)
				switch session {
				case "support":
					ctx = contextvalues.WithValidatedSupportSession(ctx, authCtx)
				case "workos":
					ctx = contextvalues.WithValidatedGramSession(ctx, authCtx, true)
				}
				for _, id := range []*string{nil, new(""), new("short"), new("12345678-0000-0000-0000-000000000000")} {
					expected := "private prose"
					if session != "ordinary" && orgID != constants.DemoOrganizationID {
						expected = "Session"
						if id != nil && *id != "" {
							expected += " " + (*id)[:min(8, len(*id))]
						}
					}
					require.Equal(t, expected, RedactSessionTitle(ctx, "private prose", id))
				}
			})
		}
	}
}
