package mcpservers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func renameServer(ctx context.Context, ti *testInstance, server remoteServerFixture, name string) (*types.McpServer, error) {
	updated, err := ti.service.UpdateMcpServer(ctx, &gen.UpdateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		ID:                    server.server.ID,
		Name:                  &name,
		EnvironmentID:         nil,
		RemoteMcpServerID:     &server.remoteMcpServerID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		UnproxiedMcpServerID:  nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("private"),
	})
	return updated, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

func mcpGrant(scope authz.Scope, kv ...string) authz.Grant {
	sel := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP}
	for i := 0; i+1 < len(kv); i += 2 {
		sel[kv[i]] = kv[i+1]
	}
	return authz.NewGrantWithSelector(scope, sel)
}

// The configuration side of slice 1: editing a server and the permissions its
// responses report come from the same evaluation, and editing never reports
// or confers use.
func TestMCPServerSchema_Configure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		grants   func(s1, s2 string, projectID string) []authz.Grant
		canGet   bool
		canEdit  bool
		wantCaps types.McpServerPermissions
	}{
		{
			name: "row 1: edit permission edits without use",
			grants: func(s1, _ string, _ string) []authz.Grant {
				return []authz.Grant{mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1)}
			},
			canGet: true, canEdit: true,
			wantCaps: types.McpServerPermissions{Read: true, Edit: true, Connect: false},
		},
		{
			name: "row 1: edit and connection grants together report both",
			grants: func(s1, _ string, _ string) []authz.Grant {
				return []authz.Grant{
					mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1),
					mcpGrant(authz.ScopeMCPConnect, authz.SelectorKeyResourceID, s1),
				}
			},
			canGet: true, canEdit: true,
			wantCaps: types.McpServerPermissions{Read: true, Edit: true, Connect: true},
		},
		{
			name: "row 2: connection grant neither reads nor edits",
			grants: func(s1, _ string, _ string) []authz.Grant {
				return []authz.Grant{mcpGrant(authz.ScopeMCPConnect, authz.SelectorKeyResourceID, s1)}
			},
			canGet: false, canEdit: false,
		},
		{
			name: "row 2: edit permission on another server does not edit this one",
			grants: func(_, s2 string, _ string) []authz.Grant {
				return []authz.Grant{mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s2)}
			},
			canGet: false, canEdit: false,
		},
		{
			name: "row 3: a block on use leaves editing and reports no use",
			grants: func(s1, _ string, _ string) []authz.Grant {
				return []authz.Grant{
					mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1),
					mcpGrant(authz.ScopeMCPConnect, authz.SelectorKeyResourceID, s1),
					mcpGrant(authz.ScopeMCPBlockedConnect, authz.SelectorKeyResourceID, s1),
				}
			},
			canGet: true, canEdit: true,
			wantCaps: types.McpServerPermissions{Read: true, Edit: true, Connect: false},
		},
		{
			name: "row 3: a block on editing defeats an edit grant",
			grants: func(s1, _ string, projectID string) []authz.Grant {
				return []authz.Grant{
					mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1),
					mcpGrant(authz.ScopeMCPBlockedWrite, authz.SelectorKeyResourceID, "*", authz.SelectorKeyProjectID, projectID),
				}
			},
			canGet: true, canEdit: false,
		},
		{
			name: "row 4: project-restricted edit grant in another project does not edit",
			grants: func(s1, _ string, _ string) []authz.Grant {
				return []authz.Grant{mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1, authz.SelectorKeyProjectID, "00000000-0000-0000-0000-000000000000")}
			},
			canGet: false, canEdit: false,
		},
		{
			name: "root administers and uses",
			grants: func(_, _ string, _ string) []authz.Grant {
				return []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)}
			},
			canGet: true, canEdit: true,
			wantCaps: types.McpServerPermissions{Read: true, Edit: true, Connect: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			ti.mcpServerSchema.Store(true)
			s1 := createRemoteServerFixture(t, ctx, ti, "schema s1")
			s2 := createRemoteServerFixture(t, ctx, ti, "schema s2")
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)

			scoped := withExactAuthzGrants(t, ctx, ti.conn, tc.grants(s1.server.ID, s2.server.ID, authCtx.ProjectID.String())...)

			got, err := getMcpServerByID(scoped, ti, s1.server.ID)
			if tc.canGet {
				require.NoError(t, err)
				require.NotNil(t, got.Permissions)
				require.Equal(t, tc.canEdit, got.Permissions.Edit, "the reported edit permission agrees with the update check")
			} else {
				requireOopsCode(t, err, oops.CodeForbidden)
			}

			updated, err := renameServer(scoped, ti, s1, "schema s1 renamed")
			if !tc.canEdit {
				requireOopsCode(t, err, oops.CodeForbidden)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, updated.Permissions)
			require.Equal(t, tc.wantCaps, *updated.Permissions)
			require.Equal(t, tc.wantCaps, *got.Permissions, "get and update report the same permissions")
		})
	}
}

// Until an organization's schema is active, edit permission still implies
// use, and the response says so.
func TestMCPServerSchema_Configure_InactiveKeepsLegacyRules(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	s1 := createRemoteServerFixture(t, ctx, ti, "legacy s1")
	scoped := withExactAuthzGrants(t, ctx, ti.conn, mcpGrant(authz.ScopeMCPWrite, authz.SelectorKeyResourceID, s1.server.ID))

	updated, err := renameServer(scoped, ti, s1, "legacy s1 renamed")
	require.NoError(t, err)
	require.Equal(t, types.McpServerPermissions{Read: true, Edit: true, Connect: true}, *updated.Permissions)
}
