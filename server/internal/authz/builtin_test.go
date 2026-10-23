package authz

import (
	"context"
	"github.com/google/uuid"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestRequireBuiltinConnect(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	exact := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: string(ResourceKindMCP), SelectorKeyResourceID: "builtin", SelectorKeyProjectID: "project"}}
	broad := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: string(ResourceKindMCP), SelectorKeyResourceID: "*"}}
	blocked := Grant{Scope: ScopeMCPBlockedConnect, Selector: exact.Selector}
	for _, tc := range []struct {
		name     string
		actor    urn.PrincipalType
		admitted bool
		agent    []Grant
		owner    []Grant
		allow    bool
	}{
		{"explicit agent", urn.PrincipalTypeAgent, true, []Grant{exact}, []Grant{broad}, true},
		{"explicit workload", urn.PrincipalTypeWorkload, true, []Grant{exact}, []Grant{broad}, true},
		{"human even with admission", urn.PrincipalTypeUser, true, []Grant{exact}, []Grant{broad}, false},
		{"broad agent role", urn.PrincipalTypeAgent, true, []Grant{broad}, []Grant{broad}, false},
		{"blocked agent", urn.PrincipalTypeAgent, true, []Grant{exact, blocked}, []Grant{broad}, false},
		{"owner ceiling", urn.PrincipalTypeAgent, true, []Grant{exact}, nil, false},
		{"legacy or override grants", urn.PrincipalTypeAgent, false, []Grant{exact}, []Grant{broad}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actor := urn.NewPrincipal(tc.actor, uuid.NewString())
			if tc.actor == urn.PrincipalTypeWorkload {
				actor = urn.NewWorkloadPrincipal(uuid.New(), "fixture-workload")
			}
			ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "owner"}, actor)
			_, canonical := contextvalues.AuthenticatedActor(ctx)
			require.True(t, canonical)
			ctx = GrantsToContext(ctx, []Grant{exact})
			if tc.admitted {
				ctx = admittedPoliciesToContext(ctx, RepresentedUserPolicy{}, []Grant{broad}, tc.agent, tc.owner)
			}
			err := engine.RequireBuiltinConnect(ctx, "builtin", "project")
			if tc.allow {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Error(t, engine.RequireBuiltinConnect(ctx, "other-builtin", "project"))
			require.Error(t, engine.RequireBuiltinConnect(ctx, "builtin", "other-project"))
		})
	}
}
