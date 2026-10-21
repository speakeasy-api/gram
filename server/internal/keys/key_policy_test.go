package keys_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func keyGrant(scope, kind, resourceID string, projectID *string) *gen.APIKeyPolicyGrant {
	return &gen.APIKeyPolicyGrant{Scope: scope, Selector: &gen.APIKeyPolicySelector{ResourceKind: kind, ResourceID: resourceID, ProjectID: projectID, ClientType: nil}}
}

func requireCode(t *testing.T, err error, code oops.Code, msgAndArgs ...any) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr, msgAndArgs...)
	require.Equal(t, code, oopsErr.Code, msgAndArgs...)
}

func keyProject(t *testing.T, ctx context.Context) string {
	t.Helper()
	return testAuthContext(t, ctx).ProjectID.String()
}

var clientA = uuid.NewString()

func TestKeysService_KeyPolicy(t *testing.T) {
	t.Parallel()

	t.Run("create stores, lists and revoke removes the key's own policy", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		policy := []*gen.APIKeyPolicyGrant{
			keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project),
			keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, clientA, nil),
		}

		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "policy" + randstr(6), Scopes: []string{"consumer"}, PolicyGrants: policy})
		require.NoError(t, err)
		require.Len(t, key.PolicyGrants, 2)

		listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
		require.NoError(t, err)
		var found *gen.Key
		for _, candidate := range listed.Keys {
			if candidate.ID == key.ID {
				found = candidate
			}
		}
		require.NotNil(t, found)
		require.ElementsMatch(t, key.PolicyGrants, found.PolicyGrants)

		keyID := uuid.MustParse(key.ID)
		grants, err := authz.LoadGrants(ctx, ti.conn, key.OrganizationID, []urn.Principal{urn.NewAPIKeyPrincipal(keyID)})
		require.NoError(t, err)
		require.Len(t, grants, 2)

		require.NoError(t, ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: key.ID}))
		grants, err = authz.LoadGrants(ctx, ti.conn, key.OrganizationID, []urn.Principal{urn.NewAPIKeyPrincipal(keyID)})
		require.NoError(t, err)
		require.Empty(t, grants, "revoking a key removes its policy")
	})

	t.Run("a key without policy still issues", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "nopolicy" + randstr(6), Scopes: []string{"consumer"}})
		require.NoError(t, err)
		require.Empty(t, key.PolicyGrants)
	})

	t.Run("invalid key policies are rejected", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		foreign := uuid.NewString()
		ctx = authztest.WithExactGrants(t, ctx,
			authz.Grant{PrincipalUrn: "", Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, testAuthContext(t, ctx).ActiveOrganizationID)},
			authz.Grant{PrincipalUrn: "", Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, project)},
		)
		for name, grant := range map[string]*gen.APIKeyPolicyGrant{
			"auth on every client":          keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, "*", nil),
			"wrong resource kind":           keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCP, clientA, nil),
			"every server with no project":  keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", nil),
			"a project outside the org":     keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &foreign),
			"a block on every server, no p": keyGrant(string(authz.ScopeMCPBlockedConnect), authz.ResourceKindMCP, "*", nil),
		} {
			_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "bad" + randstr(6), Scopes: []string{"consumer"}, PolicyGrants: []*gen.APIKeyPolicyGrant{grant}})
			requireCode(t, err, oops.CodeBadRequest, name)
		}

		_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "bound" + randstr(6), Scopes: []string{"consumer"}, ProjectID: &project,
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPAuth), authz.ResourceKindMCPAuthClient, clientA, nil)}})
		requireCode(t, err, oops.CodeBadRequest, "a project-bound key's policy must name its project")

		_, err = ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "bound-ok" + randstr(6), Scopes: []string{"consumer"}, ProjectID: &project,
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		require.NoError(t, err)
	})

	t.Run("agent keys refuse policy grants", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		agentID := uuid.NewString()
		version := 4
		_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "agent" + randstr(6), AgentID: &agentID, DelegatedGrantsVersion: &version,
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		requireCode(t, err, oops.CodeBadRequest)
	})

	t.Run("a policy needs org admin even on a self-serve hooks key", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		ctx = authztest.WithExactGrants(t, ctx)
		_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "hooks-self-serve" + randstr(6), Scopes: []string{"hooks"}})
		require.NoError(t, err)

		_, err = ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "hooks-policy" + randstr(6), Scopes: []string{"hooks"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		requireCode(t, err, oops.CodeForbidden)
	})

	t.Run("ids are stored canonically and duplicates once", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		upper := strings.ToUpper(project)
		key, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "canon" + randstr(6), Scopes: []string{"consumer"}, PolicyGrants: []*gen.APIKeyPolicyGrant{
			keyGrant(string(authz.ScopeMCPBlockedConnect), authz.ResourceKindMCP, "*", &upper),
			keyGrant(string(authz.ScopeMCPBlockedConnect), authz.ResourceKindMCP, "*", &project),
		}})
		require.NoError(t, err)
		require.Len(t, key.PolicyGrants, 1)
		require.Equal(t, project, *key.PolicyGrants[0].Selector.ProjectID)

		listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
		require.NoError(t, err)
		var stored *gen.Key
		for _, candidate := range listed.Keys {
			if candidate.ID == key.ID {
				stored = candidate
			}
		}
		require.NotNil(t, stored)
		require.Len(t, stored.PolicyGrants, 1)
		require.Equal(t, string(authz.ScopeMCPBlockedConnect), stored.PolicyGrants[0].Scope)
		require.Equal(t, project, *stored.PolicyGrants[0].Selector.ProjectID)
	})

	t.Run("a policy needs a transport scope that reaches MCP", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		project := keyProject(t, ctx)
		_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "hooks" + randstr(6), Scopes: []string{"hooks"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &project)}})
		requireCode(t, err, oops.CodeBadRequest)
	})

	t.Run("a wildcard project is refused", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestKeysService(t)
		wildcard := "*"
		_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{Name: "wild" + randstr(6), Scopes: []string{"consumer"},
			PolicyGrants: []*gen.APIKeyPolicyGrant{keyGrant(string(authz.ScopeMCPConnect), authz.ResourceKindMCP, "*", &wildcard)}})
		requireCode(t, err, oops.CodeBadRequest)
	})
}
