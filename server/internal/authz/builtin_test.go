package authz

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestBuiltinUsesNormalMCPAuthorization(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	exact := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: string(ResourceKindMCP), SelectorKeyResourceID: "builtin", SelectorKeyProjectID: "project"}}
	broad := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: string(ResourceKindMCP), SelectorKeyResourceID: "*", SelectorKeyProjectID: "project"}}
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
		{"human session", urn.PrincipalTypeUser, false, []Grant{exact}, []Grant{broad}, true},
		{"broad agent role", urn.PrincipalTypeAgent, true, []Grant{broad}, []Grant{broad}, true},
		{"blocked agent", urn.PrincipalTypeAgent, true, []Grant{exact, blocked}, []Grant{broad}, false},
		{"owner ceiling", urn.PrincipalTypeAgent, true, []Grant{exact}, nil, false},
		{"broad workload role", urn.PrincipalTypeWorkload, true, []Grant{broad}, []Grant{broad}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			actor := urn.NewPrincipal(tc.actor, uuid.NewString())
			if tc.actor == urn.PrincipalTypeWorkload {
				actor = urn.NewWorkloadPrincipal(uuid.New(), "fixture-workload")
			}
			sessionID := "fixture-session"
			ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org", UserID: "owner", SessionID: &sessionID}, actor)
			_, canonical := contextvalues.AuthenticatedActor(ctx)
			require.True(t, canonical)
			ctx = GrantsToContext(ctx, []Grant{exact})
			if tc.admitted {
				ctx = admittedPoliciesToContext(ctx, RepresentedUserPolicy{}, false, []Grant{broad}, tc.agent, tc.owner)
			}
			err := engine.Require(ctx, MCPCheck(ScopeMCPConnect, "builtin", "project"))
			if tc.allow {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if tc.name != "broad agent role" && tc.name != "broad workload role" {
				require.Error(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, "other-builtin", "project")))
			}
			require.Error(t, engine.Require(ctx, MCPCheck(ScopeMCPConnect, "builtin", "other-project")))
		})
	}
}

func TestBuiltinNormalChecksPreserveToolAndCredentialCeilings(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	broad := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "*", SelectorKeyProjectID: "project"}}
	readOnly := Grant{Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "builtin", SelectorKeyProjectID: "project", SelectorKeyTool: "read", SelectorKeyDisposition: "read_only"}}
	connect := MCPCheck(ScopeMCPConnect, "builtin", "project")
	read := MCPToolCallCheck("builtin", MCPToolCallDimensions{Tool: "read", Disposition: "read_only", ProjectID: "project"})
	write := MCPToolCallCheck("builtin", MCPToolCallDimensions{Tool: "write", Disposition: "write", ProjectID: "project"})
	for _, ceiling := range []string{"credential", "live agent", "owner"} {
		t.Run(ceiling, func(t *testing.T) {
			t.Parallel()
			policies := [][]Grant{{broad}, {broad}, {broad}}
			index := map[string]int{"credential": 0, "live agent": 1, "owner": 2}[ceiling]
			policies[index] = []Grant{readOnly}
			ctx := admittedPoliciesToContext(enterpriseSessionCtx(t), RepresentedUserPolicy{}, false, policies...)
			require.NoError(t, engine.Require(ctx, connect, read))
			require.Error(t, engine.Require(ctx, connect, write))
			policies[index] = nil
			ctx = admittedPoliciesToContext(enterpriseSessionCtx(t), RepresentedUserPolicy{}, false, policies...)
			require.Error(t, engine.Require(ctx, connect))
		})
	}
}
