package authz

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// Synthetic fixture: enterpriseSessionCtx authenticates in org_123. Project
// P1 holds servers S1 and S2, project P2 holds S3. org_other holds SB in PB.
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

func schemaEngine(t *testing.T, active bool) *Engine {
	t.Helper()
	activation := func(_ context.Context, organizationID string) (bool, error) {
		return active && organizationID == fxOrg, nil
	}
	return NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{MCPServerSchemaActive: activation})
}

func requireNotForbidden(t *testing.T, err error, msgAndArgs ...any) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	var oopsErr *oops.ShareableError
	if errors.As(err, &oopsErr) {
		require.NotEqual(t, oops.CodeForbidden, oopsErr.Code, msgAndArgs...)
	}
}

func serverGrant(principal string, scope Scope, kv ...string) Grant {
	sel := Selector{SelectorKeyResourceKind: ResourceKindMCP}
	for i := 0; i+1 < len(kv); i += 2 {
		sel[kv[i]] = kv[i+1]
	}
	return Grant{PrincipalUrn: principal, Scope: scope, Selector: sel, Inferred: false}
}

func onServer(id string, kv ...string) []string {
	return append([]string{SelectorKeyResourceID, id}, kv...)
}

func sessionWithGrants(t *testing.T, grants ...Grant) context.Context {
	t.Helper()
	return withPolicyOrganization(GrantsToContext(enterpriseSessionCtx(t), grants))
}

type mcpServerCase struct {
	name    string
	grants  []Grant
	server  MCPServerRef
	want    MCPServerCapabilities
	legacy  bool   // a relationship on the allow path is a legacy broad grant
	because string // substring of the deny reason, when connect is denied
}

func TestMCPServerSchema_ConfigurationVersusUse(t *testing.T) {
	t.Parallel()

	cases := []mcpServerCase{
		{
			name:   "row 1: edit permission edits but does not use",
			grants: []Grant{serverGrant(fxUser, ScopeMCPWrite, onServer(fxS1)...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: true, Edit: true, Connect: false},
		},
		{
			name:   "row 1: read permission does not use",
			grants: []Grant{serverGrant(fxUser, ScopeMCPRead, onServer(fxS1)...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: true, Edit: false, Connect: false},
		},
		{
			name:   "row 2: connection grant uses its server",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "row 2: connection grant does not use another server",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...)},
			server: refS2,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: false},
		},
		{
			name: "row 3a: inherited block defeats a direct allow",
			grants: []Grant{
				serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...),
				serverGrant(fxRole, ScopeMCPBlockedConnect, onServer(fxS1)...),
			},
			server:  refS1,
			want:    MCPServerCapabilities{Read: false, Edit: false, Connect: false},
			because: "withdrawn",
		},
		{
			name: "row 3b: block on one server leaves the other",
			grants: []Grant{
				serverGrant(fxAll, ScopeMCPConnect, onServer(fxS1)...),
				serverGrant(fxAll, ScopeMCPConnect, onServer(fxS2)...),
				serverGrant(fxRole, ScopeMCPBlockedConnect, onServer(fxS1)...),
			},
			server: refS2,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "row 4: project-restricted grant allows its server in that project",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1, SelectorKeyProjectID, fxP1)...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:    "row 4: project restriction holds on a concrete server elsewhere",
			grants:  []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS3, SelectorKeyProjectID, fxP1)...)},
			server:  refS3,
			want:    MCPServerCapabilities{Read: false, Edit: false, Connect: false},
			because: "no relationship",
		},
		{
			name: "row 5: grants naming a foreign server do not reach it",
			grants: []Grant{
				serverGrant(fxUser, ScopeMCPConnect, onServer(fxSB)...),
				serverGrant(fxUser, ScopeMCPWrite, onServer(fxSB)...),
				NewGrant(ScopeRoot, WildcardResource),
			},
			server:  refSB,
			want:    MCPServerCapabilities{Read: false, Edit: false, Connect: false},
			because: "another organization",
		},
		{
			name:   "row 6: organization-wide connection grant is honored as legacy",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(WildcardResource)...)},
			server: refS3,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
			legacy: true,
		},
		{
			name:   "row 6: project-wide connection grant is honored as legacy in its project",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(WildcardResource, SelectorKeyProjectID, fxP1)...)},
			server: refS2,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
			legacy: true,
		},
		{
			name:    "row 6: project-wide connection grant does not reach another project",
			grants:  []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(WildcardResource, SelectorKeyProjectID, fxP1)...)},
			server:  refS3,
			want:    MCPServerCapabilities{Read: false, Edit: false, Connect: false},
			because: "no relationship",
		},
		{
			name:   "root administers and uses",
			grants: []Grant{NewGrant(ScopeRoot, WildcardResource)},
			server: refS1,
			want:   MCPServerCapabilities{Read: true, Edit: true, Connect: true},
		},
		{
			name: "a block withdraws root",
			grants: []Grant{
				NewGrant(ScopeRoot, WildcardResource),
				serverGrant(fxRole, ScopeMCPBlockedConnect, onServer(fxS1)...),
				serverGrant(fxRole, ScopeMCPBlockedWrite, onServer(WildcardResource, SelectorKeyProjectID, fxP1)...),
			},
			server:  refS1,
			want:    MCPServerCapabilities{Read: true, Edit: false, Connect: false},
			because: "withdrawn",
		},
		{
			name:   "tool-limited connection grant admits its server",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1, SelectorKeyTool, "search")...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "tool-limited block does not withdraw the server",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...), serverGrant(fxRole, ScopeMCPBlockedConnect, onServer(fxS1, SelectorKeyTool, "delete")...)},
			server: refS1,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
		{
			name:   "out-of-schema scopes are ignored",
			grants: []Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...), NewGrant(ScopeProjectRead, fxP1)},
			server: refS1,
			want:   MCPServerCapabilities{Read: false, Edit: false, Connect: true},
		},
	}

	engine := schemaEngine(t, true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := sessionWithGrants(t, tc.grants...)

			caps, err := engine.MCPServerCapabilities(ctx, tc.server)
			require.NoError(t, err)
			require.Equal(t, tc.want, caps)

			// The capability response and the enforced check agree.
			for permission, want := range map[MCPServerPermission]bool{MCPServerRead: tc.want.Read, MCPServerEdit: tc.want.Edit, MCPServerConnect: tc.want.Connect} {
				err := engine.RequireMCPServer(ctx, permission, tc.server)
				if want {
					require.NoError(t, err, permission)
				} else {
					require.Error(t, err, permission)
					requireForbidden(t, err)
				}
			}

			explanation, err := engine.ExplainMCPServer(ctx, MCPServerConnect, tc.server)
			require.NoError(t, err)
			require.True(t, explanation.Schema)
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

	ctx := sessionWithGrants(t, serverGrant(fxUser, ScopeMCPWrite, onServer(fxS1)...))
	caps, err := engine.MCPServerCapabilities(ctx, refS1)
	require.NoError(t, err)
	require.Equal(t, MCPServerCapabilities{Read: true, Edit: true, Connect: true}, caps)
	require.NoError(t, engine.RequireMCPServer(ctx, MCPServerConnect, refS1))

	ctx = sessionWithGrants(t,
		serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...),
		serverGrant(fxRole, ScopeMCPBlockedConnect, onServer(fxS1)...),
	)
	require.NoError(t, engine.RequireMCPServer(ctx, MCPServerConnect, refS1))
}

func TestMCPServerSchema_UnsupportedGrantShapesFail(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		grant Grant
	}{
		{"tool key on an edit grant", serverGrant(fxUser, ScopeMCPWrite, onServer(fxS1, SelectorKeyTool, "search")...)},
		{"unknown selector key", serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1, SelectorKeyServerURL, "https://example.test")...)},
		{"represented user marker", serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1, SelectorKeyTool, "$represented_user")...)},
		{"missing resource id", Grant{PrincipalUrn: fxUser, Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: ResourceKindMCP}, Inferred: false}},
		{"wrong resource kind", Grant{PrincipalUrn: fxUser, Scope: ScopeMCPConnect, Selector: Selector{SelectorKeyResourceKind: ResourceKindProject, SelectorKeyResourceID: fxS1}, Inferred: false}},
	}

	engine := schemaEngine(t, true)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := sessionWithGrants(t, tc.grant)

			_, err := engine.ExplainMCPServer(ctx, MCPServerConnect, refS1)
			require.Error(t, err)
			require.Contains(t, err.Error(), "decode grant")

			err = engine.RequireMCPServer(ctx, MCPServerConnect, refS1)
			requireNotForbidden(t, err, "an undecodable grant is an error, not a silent deny")
		})
	}
}

func TestMCPServerSchema_PoliciesBoundToAnotherOrganizationDeny(t *testing.T) {
	t.Parallel()

	engine := schemaEngine(t, true)
	// Policies were loaded in another organization, then the request switched
	// to fxOrg without reloading them.
	loaded := withPolicyOrganization(GrantsToContext(enterpriseSessionCtxWithOrg(t, fxOtherOrg), []Grant{NewGrant(ScopeRoot, WildcardResource)}))
	authCtx, ok := contextvalues.GetAuthContext(loaded)
	require.True(t, ok)
	switched := *authCtx
	switched.ActiveOrganizationID = fxOrg
	ctx := contextvalues.SetAuthContext(loaded, &switched)

	requireForbidden(t, engine.RequireMCPServer(ctx, MCPServerConnect, refS1))
	explanation, err := engine.ExplainMCPServer(ctx, MCPServerConnect, refS1)
	require.NoError(t, err)
	require.Equal(t, "undetermined", explanation.Policies[0].Outcome)
}

func TestMCPServerSchema_EveryPolicyMustAllow(t *testing.T) {
	t.Parallel()

	engine := schemaEngine(t, true)
	ctx := admittedPoliciesToContext(enterpriseSessionCtx(t), RepresentedUserPolicy{UserID: "", Policy: nil}, false,
		[]Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...)},
		[]Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS2)...)},
	)
	requireForbidden(t, engine.RequireMCPServer(ctx, MCPServerConnect, refS1))

	ctx = admittedPoliciesToContext(enterpriseSessionCtx(t), RepresentedUserPolicy{UserID: "", Policy: nil}, false,
		[]Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(fxS1)...)},
		[]Grant{serverGrant(fxUser, ScopeMCPConnect, onServer(WildcardResource)...)},
	)
	require.NoError(t, engine.RequireMCPServer(ctx, MCPServerConnect, refS1))
}

func TestMCPServerSchema_IncompleteReferenceFails(t *testing.T) {
	t.Parallel()

	engine := schemaEngine(t, true)
	ctx := sessionWithGrants(t, NewGrant(ScopeRoot, WildcardResource))
	for _, ref := range []MCPServerRef{
		{ID: fxS1, ProjectID: "", OrganizationID: fxOrg},
		{ID: fxS1, ProjectID: fxP1, OrganizationID: ""},
		{ID: WildcardResource, ProjectID: fxP1, OrganizationID: fxOrg},
	} {
		err := engine.RequireMCPServer(ctx, MCPServerEdit, ref)
		requireNotForbidden(t, err)
	}
}
