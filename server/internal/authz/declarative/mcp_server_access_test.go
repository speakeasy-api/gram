package declarative

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Synthetic fixture: the session authenticates in fxOrg. Project P1 holds
// servers S1 and S2, project P2 holds S3. fxOtherOrg holds SB in PB.
const (
	fxOrg      = "org_123"
	fxOtherOrg = "org_other"
	fxP1       = "proj_p1"
	fxP2       = "proj_p2"
	fxPB       = "proj_pb"
	fxS1       = "mcp_s1"
	fxS2       = "mcp_s2"
	fxS3       = "mcp_s3"
	fxSB       = "mcp_sb"
	fxUser     = "user:user_123"
	fxRole     = "role:fixture-role"
	fxAll      = "user:all"
)

var (
	refS1 = MCPServerRef{ID: fxS1, ProjectID: fxP1, OrganizationID: fxOrg}
	refS2 = MCPServerRef{ID: fxS2, ProjectID: fxP1, OrganizationID: fxOrg}
	refS3 = MCPServerRef{ID: fxS3, ProjectID: fxP2, OrganizationID: fxOrg}
	refSB = MCPServerRef{ID: fxSB, ProjectID: fxPB, OrganizationID: fxOtherOrg}
)

func schemaEngine(t *testing.T, active bool) *authz.Engine {
	t.Helper()
	activation := func(_ context.Context, organizationID string) (bool, error) {
		return active && organizationID == fxOrg, nil
	}
	return authz.NewEngine(testenv.NewLogger(t), nil, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient(), authz.EngineOpts{MCPServerSchemaActive: activation})
}

func sessionWithGrants(t *testing.T, grants ...authz.Grant) context.Context {
	t.Helper()
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  fxOrg,
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
	return authz.GrantsToContext(ctx, grants)
}

func requireForbidden(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, msgAndArgs...)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code, msgAndArgs...)
}

func requireNotForbidden(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	var oopsErr *oops.ShareableError
	if errors.As(err, &oopsErr) {
		require.NotEqual(t, oops.CodeForbidden, oopsErr.Code, msgAndArgs...)
	}
}

func serverGrant(principal string, scope authz.Scope, kv ...string) authz.Grant {
	sel := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP}
	for i := 0; i+1 < len(kv); i += 2 {
		sel[kv[i]] = kv[i+1]
	}
	return authz.Grant{PrincipalUrn: principal, Scope: scope, Selector: sel, Inferred: false}
}

func onServer(id string, kv ...string) []string {
	return append([]string{authz.SelectorKeyResourceID, id}, kv...)
}

type mcpServerCase struct {
	name    string
	grants  []authz.Grant
	server  MCPServerRef
	want    Capabilities
	legacy  bool   // a relationship on the allow path is a legacy broad grant
	because string // substring of the deny reason, when connect is denied
}

func TestMCPServerSchema_ConfigurationVersusUse(t *testing.T) {
	t.Parallel()

	cases := []mcpServerCase{
		{
			name:   "row 1: edit permission edits but does not use",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPWrite, onServer(fxS1)...)},
			server: refS1,
			want:   Capabilities{Read: true, Edit: true, Connect: false},
		},
		{
			name:   "row 1: read permission does not use",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPRead, onServer(fxS1)...)},
			server: refS1,
			want:   Capabilities{Read: true, Edit: false, Connect: false},
		},
		{
			name:   "row 2: connection grant uses its server",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...)},
			server: refS1,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "row 2: connection grant does not use another server",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...)},
			server: refS2,
			want:   Capabilities{Read: false, Edit: false, Connect: false},
		},
		{
			name: "row 3a: inherited block defeats a direct allow",
			grants: []authz.Grant{
				serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...),
				serverGrant(fxRole, authz.ScopeMCPBlockedConnect, onServer(fxS1)...),
			},
			server:  refS1,
			want:    Capabilities{Read: false, Edit: false, Connect: false},
			because: "withdrawn",
		},
		{
			name: "row 3b: block on one server leaves the other",
			grants: []authz.Grant{
				serverGrant(fxAll, authz.ScopeMCPConnect, onServer(fxS1)...),
				serverGrant(fxAll, authz.ScopeMCPConnect, onServer(fxS2)...),
				serverGrant(fxRole, authz.ScopeMCPBlockedConnect, onServer(fxS1)...),
			},
			server: refS2,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "row 4: project-restricted grant allows its server in that project",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1, authz.SelectorKeyProjectID, fxP1)...)},
			server: refS1,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:    "row 4: project restriction holds on a concrete server elsewhere",
			grants:  []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS3, authz.SelectorKeyProjectID, fxP1)...)},
			server:  refS3,
			want:    Capabilities{Read: false, Edit: false, Connect: false},
			because: "no relationship",
		},
		{
			name: "row 5: grants naming a foreign server do not reach it",
			grants: []authz.Grant{
				serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxSB)...),
				serverGrant(fxUser, authz.ScopeMCPWrite, onServer(fxSB)...),
				authz.NewGrant(authz.ScopeRoot, authz.WildcardResource),
			},
			server:  refSB,
			want:    Capabilities{Read: false, Edit: false, Connect: false},
			because: "another organization",
		},
		{
			name:   "row 6: organization-wide connection grant is honored as legacy",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(authz.WildcardResource)...)},
			server: refS3,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
			legacy: true,
		},
		{
			name:   "row 6: project-wide connection grant is honored as legacy in its project",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(authz.WildcardResource, authz.SelectorKeyProjectID, fxP1)...)},
			server: refS2,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
			legacy: true,
		},
		{
			name:    "row 6: project-wide connection grant does not reach another project",
			grants:  []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(authz.WildcardResource, authz.SelectorKeyProjectID, fxP1)...)},
			server:  refS3,
			want:    Capabilities{Read: false, Edit: false, Connect: false},
			because: "no relationship",
		},
		{
			name:   "root administers and uses",
			grants: []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)},
			server: refS1,
			want:   Capabilities{Read: true, Edit: true, Connect: true},
		},
		{
			name: "a block withdraws root",
			grants: []authz.Grant{
				authz.NewGrant(authz.ScopeRoot, authz.WildcardResource),
				serverGrant(fxRole, authz.ScopeMCPBlockedConnect, onServer(fxS1)...),
				serverGrant(fxRole, authz.ScopeMCPBlockedWrite, onServer(authz.WildcardResource, authz.SelectorKeyProjectID, fxP1)...),
			},
			server:  refS1,
			want:    Capabilities{Read: true, Edit: false, Connect: false},
			because: "withdrawn",
		},
		{
			name:   "tool-limited connection grant admits its server",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1, authz.SelectorKeyTool, "search")...)},
			server: refS1,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "tool-limited block does not withdraw the server",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...), serverGrant(fxRole, authz.ScopeMCPBlockedConnect, onServer(fxS1, authz.SelectorKeyTool, "delete")...)},
			server: refS1,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "out-of-schema scopes are ignored",
			grants: []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...), authz.NewGrant(authz.ScopeProjectRead, fxP1)},
			server: refS1,
			want:   Capabilities{Read: false, Edit: false, Connect: true},
		},
	}

	engine := schemaEngine(t, true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := sessionWithGrants(t, tc.grants...)

			caps, err := MCPServerCapabilities(ctx, engine, tc.server)
			require.NoError(t, err)
			require.Equal(t, tc.want, caps)

			// The capability response and the enforced check agree.
			for permission, want := range map[MCPServerPermission]bool{MCPServerRead: tc.want.Read, MCPServerEdit: tc.want.Edit, MCPServerConnect: tc.want.Connect} {
				err := RequireMCPServer(ctx, engine, permission, tc.server)
				if want {
					require.NoError(t, err, permission)
				} else {
					requireForbidden(t, err, permission)
				}
			}

			explanation, err := ExplainMCPServer(ctx, engine, MCPServerConnect, tc.server)
			require.NoError(t, err)
			require.Equal(t, tc.want.Connect, explanation.Allowed)
			require.Len(t, explanation.Policies, 1)
			trace := explanation.trace()
			require.Equal(t, tc.legacy, strings.Contains(trace, "legacy broad grant"), trace)
			if tc.because != "" {
				require.Contains(t, explanation.Policies[0].Reason, tc.because, trace)
			}
		})
	}
}

// The legacy rules stay in force until an organization's schema is active:
// edit permission still implies use and a direct grant outranks an inherited
// block.
func TestMCPServerSchema_InactiveKeepsLegacyRules(t *testing.T) {
	t.Parallel()

	engine := schemaEngine(t, false)

	ctx := sessionWithGrants(t, serverGrant(fxUser, authz.ScopeMCPWrite, onServer(fxS1)...))
	caps, err := MCPServerCapabilities(ctx, engine, refS1)
	require.NoError(t, err)
	require.Equal(t, Capabilities{Read: true, Edit: true, Connect: true}, caps)
	require.NoError(t, RequireMCPServer(ctx, engine, MCPServerConnect, refS1))

	ctx = sessionWithGrants(t,
		serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...),
		serverGrant(fxRole, authz.ScopeMCPBlockedConnect, onServer(fxS1)...),
	)
	require.NoError(t, RequireMCPServer(ctx, engine, MCPServerConnect, refS1))
}

func TestMCPServerSchema_UnsupportedGrantShapesFail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		grant authz.Grant
	}{
		{"tool key on an edit grant", serverGrant(fxUser, authz.ScopeMCPWrite, onServer(fxS1, authz.SelectorKeyTool, "search")...)},
		{"unknown selector key", serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1, authz.SelectorKeyServerURL, "https://example.test")...)},
		{"represented user marker", serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1, authz.SelectorKeyTool, "$represented_user")...)},
		{"missing resource id", authz.Grant{PrincipalUrn: fxUser, Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP}, Inferred: false}},
		{"wrong resource kind", authz.Grant{PrincipalUrn: fxUser, Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindProject, authz.SelectorKeyResourceID: fxS1}, Inferred: false}},
	}

	engine := schemaEngine(t, true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := sessionWithGrants(t, tc.grant)

			_, err := ExplainMCPServer(ctx, engine, MCPServerConnect, refS1)
			require.ErrorContains(t, err, "decode grant")

			err = RequireMCPServer(ctx, engine, MCPServerConnect, refS1)
			requireNotForbidden(t, err, "an undecodable grant is an error, not a silent deny")
		})
	}
}

func TestMCPServerSchema_UnboundPoliciesAreUndetermined(t *testing.T) {
	t.Parallel()

	set := authz.PolicySet{Enforced: true, OrganizationID: "", Policies: [][]authz.Grant{{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}}}
	explanation, err := explainMCPServer(set, MCPServerConnect, refS1)
	require.NoError(t, err)
	require.False(t, explanation.Allowed)
	require.Equal(t, "undetermined", explanation.Policies[0].Outcome)
}

func TestMCPServerSchema_EveryPolicyMustAllow(t *testing.T) {
	t.Parallel()

	set := authz.PolicySet{Enforced: true, OrganizationID: fxOrg, Policies: [][]authz.Grant{
		{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS1)...)},
		{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(fxS2)...)},
	}}
	explanation, err := explainMCPServer(set, MCPServerConnect, refS1)
	require.NoError(t, err)
	require.False(t, explanation.Allowed)

	set.Policies[1] = []authz.Grant{serverGrant(fxUser, authz.ScopeMCPConnect, onServer(authz.WildcardResource)...)}
	explanation, err = explainMCPServer(set, MCPServerConnect, refS1)
	require.NoError(t, err)
	require.True(t, explanation.Allowed)

	set.Policies = nil
	explanation, err = explainMCPServer(set, MCPServerConnect, refS1)
	require.NoError(t, err)
	require.False(t, explanation.Allowed, "no policy allows nothing")
}

func TestMCPServerSchema_IncompleteReferenceFails(t *testing.T) {
	t.Parallel()

	engine := schemaEngine(t, true)
	ctx := sessionWithGrants(t, authz.NewGrant(authz.ScopeRoot, authz.WildcardResource))
	for _, ref := range []MCPServerRef{
		{ID: fxS1, ProjectID: "", OrganizationID: fxOrg},
		{ID: fxS1, ProjectID: fxP1, OrganizationID: ""},
		{ID: authz.WildcardResource, ProjectID: fxP1, OrganizationID: fxOrg},
	} {
		requireNotForbidden(t, RequireMCPServer(ctx, engine, MCPServerEdit, ref))
	}
}
