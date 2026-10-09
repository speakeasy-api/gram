package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// enrollmentGrants is what an agent identity's device key holds: device sync,
// hooks ingest, and connect to every MCP server minus one excluded server.
func enrollmentGrants(orgID string, excluded uuid.UUID) []authz.Grant {
	return []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, orgID),
		authz.NewGrant(authz.ScopeOrgHooksIngest, orgID),
		authz.NewGrantWithSelector(authz.ScopeMCPConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: "*"}),
		authz.NewGrantWithSelector(authz.ScopeMCPBlockedConnect, authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: excluded.String()}),
	}
}

func authorizeMethod(t *testing.T, ti *testInstance, key, method string) (context.Context, error) {
	t.Helper()
	ctx, err := ti.service.APIKeyAuth(context.WithValue(t.Context(), goa.MethodKey, method), key, getPluginsScheme)
	if err != nil {
		return ctx, err //nolint:wrapcheck // tests compare the shareable code
	}
	return ctx, nil
}

func mintMCPCredential(t *testing.T, ti *testInstance, parentKey string) *gen.MintMcpCredentialResult {
	t.Helper()
	admitted, err := authorizeMethod(t, ti, parentKey, "mintMcpCredential")
	require.NoError(t, err)
	res, err := ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{})
	require.NoError(t, err)
	return res
}

func TestMintMcpCredential_IssuesConnectOnlyChild(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	excluded := uuid.New()
	parent := mintAgentKeyExpiring(t, ctx, ti, enrollmentGrants(ti.orgID, excluded), 200*24*time.Hour)

	before := time.Now()
	res := mintMCPCredential(t, ti, parent.key)
	require.NotEmpty(t, res.Key)
	require.NotEqual(t, parent.key, res.Key)
	require.NotEmpty(t, res.KeyPrefix)

	expiresAt, err := time.Parse(time.RFC3339, res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(90*24*time.Hour), expiresAt, time.Minute)

	childID, err := uuid.Parse(res.ID)
	require.NoError(t, err)
	child, err := keysrepo.New(ti.conn).GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: childID, OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: parent.id, Valid: true}, child.ParentApiKeyID)
	require.Equal(t, parent.actor.String(), child.SubjectUrn.String)
	require.Empty(t, child.Scopes)

	policy, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(child.DelegatedGrantsVersion.Int32), child.DelegatedGrants)
	require.NoError(t, err)
	scopes := make([]authz.Scope, 0, len(policy.Effective))
	for _, grant := range policy.Effective {
		scopes = append(scopes, grant.Scope)
	}
	require.ElementsMatch(t, []authz.Scope{authz.ScopeMCPConnect, authz.ScopeMCPBlockedConnect}, scopes,
		"the child keeps the parent's MCP exclusions and nothing else")
}

func TestMintMcpCredential_ExpiryCappedAtParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKeyExpiring(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()), 24*time.Hour)

	res := mintMCPCredential(t, ti, parent.key)
	expiresAt, err := time.Parse(time.RFC3339, res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), expiresAt, time.Minute)
}

func TestMintMcpCredential_ChildIsRefusedOutsideMCP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))
	child := mintMCPCredential(t, ti, parent.key)

	_, err := authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeForbidden)
	_, err = authorizeMethod(t, ti, child.Key, "mintMcpCredential")
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_ChildCannotMint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	grants := enrollmentGrants(ti.orgID, uuid.New())
	parent := mintAgentKey(t, ctx, ti, grants)

	// A child carrying device sync would pass the route gate; the handler
	// must still refuse it.
	parentRow, err := keysrepo.New(ti.conn).GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: parent.id, OrganizationID: ti.orgID})
	require.NoError(t, err)
	key, keyHash, keyPrefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("local"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateChildAgentAPIKey(ctx, keysrepo.CreateChildAgentAPIKeyParams{
		OrganizationID: ti.orgID, CreatedByUserID: parentRow.CreatedByUserID, Name: "forged child",
		KeyPrefix: keyPrefix, KeyHash: keyHash, SubjectUrn: parentRow.SubjectUrn,
		DelegatedGrants: parentRow.DelegatedGrants, DelegatedGrantsVersion: parentRow.DelegatedGrantsVersion,
		ExpiresAt: parentRow.ExpiresAt, ParentApiKeyID: uuid.NullUUID{UUID: parent.id, Valid: true},
	})
	require.NoError(t, err)

	admitted, err := authorizeMethod(t, ti, key, "mintMcpCredential")
	require.NoError(t, err)
	_, err = ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_ReissueRevokesPrevious(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))

	first := mintMCPCredential(t, ti, parent.key)
	second := mintMCPCredential(t, ti, parent.key)
	require.NotEqual(t, first.Key, second.Key)

	// A live child fails the route gate on grants; a revoked one fails to authenticate.
	_, err := authorizeMethod(t, ti, first.Key, "getPlugins")
	requireCode(t, err, oops.CodeUnauthorized)
	_, err = authorizeMethod(t, ti, second.Key, "getPlugins")
	requireCode(t, err, oops.CodeForbidden)

	live, err := keysrepo.New(ti.conn).ListAgentAPIKeys(ctx, keysrepo.ListAgentAPIKeysParams{OrganizationID: ti.orgID, SubjectUrn: conv.ToPGText(parent.actor.String())})
	require.NoError(t, err)
	require.Len(t, live, 2, "the parent and one child remain")
}

func TestMintMcpCredential_ParentRevocationInvalidatesChild(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))
	child := mintMCPCredential(t, ti, parent.key)

	_, err := keysrepo.New(ti.conn).DeleteAgentAPIKey(ctx, keysrepo.DeleteAgentAPIKeyParams{ID: parent.id, OrganizationID: ti.orgID, SubjectUrn: conv.ToPGText(parent.actor.String())})
	require.NoError(t, err)

	_, err = authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_ParentExpiryInvalidatesChild(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))
	child := mintMCPCredential(t, ti, parent.key)

	require.NoError(t, testrepo.New(ti.conn).ExpireAPIKeyFixture(ctx, parent.id))

	_, err := authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_SuspendedAgentIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))
	child := mintMCPCredential(t, ti, parent.key)

	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: ti.orgID, ID: parent.agentID})
	require.NoError(t, err)

	_, err = authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.Error(t, err)
	_, err = authorizeMethod(t, ti, child.Key, "getPlugins")
	require.Error(t, err)
}

func TestMintMcpCredential_ParentWithoutConnectIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeOrgDeviceAgentSync, ti.orgID)})

	admitted, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.NoError(t, err)
	_, err = ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_NonAgentCallersAreRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// The test context is a human session.
	_, err := ti.service.MintMcpCredential(ctx, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)

	// A faked agent context without a principal key mode is still refused.
	_, err = ti.service.MintMcpCredential(t.Context(), &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_RejectsBadExpiry(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, uuid.New()))
	admitted, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.NoError(t, err)

	for _, raw := range []string{"tomorrow", time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Add(400 * 24 * time.Hour).Format(time.RFC3339)} {
		_, err := ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{ExpiresAt: &raw})
		requireCode(t, err, oops.CodeBadRequest)
	}
}
