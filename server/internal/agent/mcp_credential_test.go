package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// enrollmentGrants is exactly what agent-identity onboarding gives a device's
// key. It never includes mcp:connect, so a lifted enrollment key reaches no MCP.
func enrollmentGrants(orgID string, projectID uuid.UUID) []authz.Grant {
	return []authz.Grant{
		authz.NewGrant(authz.ScopeOrgDeviceAgentSync, orgID),
		authz.NewGrant(authz.ScopeOrgHooksIngest, orgID),
		authz.NewGrant(authz.ScopeProjectRead, projectID.String()),
	}
}

func mcpSelector(resourceID string) authz.Selector {
	return authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: resourceID}
}

// grantLive gives principal a live RBAC grant, outside any key's policy.
func grantLive(t *testing.T, ctx context.Context, ti *testInstance, principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	t.Helper()
	encoded, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: ti.orgID, PrincipalUrn: principal, Scope: string(scope), Selectors: encoded,
	})
	require.NoError(t, err)
}

// humanPrincipal is the test user: the agent's owner and the enrollment
// key's authorizer.
func humanPrincipal(t *testing.T, ctx context.Context) urn.Principal {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
}

// enrollWithMCP mints an enrollment key whose agent and human may both
// connect to every MCP server.
func enrollWithMCP(t *testing.T, ctx context.Context, ti *testInstance, ttl time.Duration) realAgentKey {
	t.Helper()
	parent := mintAgentKeyExpiring(t, ctx, ti, enrollmentGrants(ti.orgID, ti.projectID), ttl)
	grantLive(t, ctx, ti, parent.actor, authz.ScopeMCPConnect, mcpSelector("*"))
	grantLive(t, ctx, ti, humanPrincipal(t, ctx), authz.ScopeMCPConnect, mcpSelector("*"))
	return parent
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

func credentialGrants(t *testing.T, ctx context.Context, ti *testInstance, id string) []runtimepolicy.DelegatedPolicyGrant {
	t.Helper()
	row, err := keysrepo.New(ti.conn).GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: uuid.MustParse(id), OrganizationID: ti.orgID})
	require.NoError(t, err)
	policy, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(row.DelegatedGrantsVersion.Int32), row.DelegatedGrants)
	require.NoError(t, err)
	return policy.Effective
}

func TestMintMcpCredential_EnrollmentKeyWithoutConnectMints(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 200*24*time.Hour)

	before := time.Now()
	res := mintMCPCredential(t, ti, parent.key)
	require.NotEmpty(t, res.Key)
	require.NotEqual(t, parent.key, res.Key)
	require.NotEmpty(t, res.KeyPrefix)

	expiresAt, err := time.Parse(time.RFC3339, res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(90*24*time.Hour), expiresAt, time.Minute)

	child, err := keysrepo.New(ti.conn).GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: uuid.MustParse(res.ID), OrganizationID: ti.orgID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: parent.id, Valid: true}, child.ParentApiKeyID)
	require.Equal(t, parent.actor.String(), child.SubjectUrn.String)
	require.Empty(t, child.Scopes)

	grants := credentialGrants(t, ctx, ti, res.ID)
	require.Len(t, grants, 1, "only MCP access, none of the enrollment key's grants")
	require.Equal(t, authz.ScopeMCPConnect, grants[0].Scope)
	require.Equal(t, "*", grants[0].Selector[authz.SelectorKeyResourceID])
}

func TestMintMcpCredential_CarriesBlockedConnectExclusions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	excluded := uuid.NewString()
	grantLive(t, ctx, ti, parent.actor, authz.ScopeMCPBlockedConnect, mcpSelector(excluded))

	res := mintMCPCredential(t, ti, parent.key)
	var blocked []string
	for _, grant := range credentialGrants(t, ctx, ti, res.ID) {
		if grant.Scope == authz.ScopeMCPBlockedConnect {
			blocked = append(blocked, grant.Selector[authz.SelectorKeyResourceID])
		}
	}
	require.Equal(t, []string{excluded}, blocked)
}

func TestMintMcpCredential_BoundedByAuthorizingHuman(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, ti.projectID))
	allowed := uuid.NewString()
	grantLive(t, ctx, ti, parent.actor, authz.ScopeMCPConnect, mcpSelector("*"))
	grantLive(t, ctx, ti, humanPrincipal(t, ctx), authz.ScopeMCPConnect, mcpSelector(allowed))

	res := mintMCPCredential(t, ti, parent.key)
	grants := credentialGrants(t, ctx, ti, res.ID)
	require.Len(t, grants, 1)
	require.Equal(t, authz.ScopeMCPConnect, grants[0].Scope)
	require.Equal(t, allowed, grants[0].Selector[authz.SelectorKeyResourceID],
		"the agent's wildcard narrows to what the human can delegate")
}

func TestMintMcpCredential_AgentWithoutMCPAccessIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, ti.projectID))
	grantLive(t, ctx, ti, humanPrincipal(t, ctx), authz.ScopeMCPConnect, mcpSelector("*"))

	admitted, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.NoError(t, err)
	_, err = ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_HumanWithoutMCPAccessIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, enrollmentGrants(ti.orgID, ti.projectID))
	grantLive(t, ctx, ti, parent.actor, authz.ScopeMCPConnect, mcpSelector("*"))

	admitted, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.NoError(t, err)
	_, err = ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_NonEnrollmentKeyIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := mintAgentKey(t, ctx, ti, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, ti.projectID.String())})
	grantLive(t, ctx, ti, parent.actor, authz.ScopeMCPConnect, mcpSelector("*"))
	grantLive(t, ctx, ti, humanPrincipal(t, ctx), authz.ScopeMCPConnect, mcpSelector("*"))

	_, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_ExpiryCappedAtParent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)

	res := mintMCPCredential(t, ti, parent.key)
	expiresAt, err := time.Parse(time.RFC3339, res.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), expiresAt, time.Minute)
}

func TestMintMcpCredential_ChildIsRefusedOutsideMCP(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	child := mintMCPCredential(t, ti, parent.key)

	_, err := authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeForbidden)
	_, err = authorizeMethod(t, ti, child.Key, "mintMcpCredential")
	requireCode(t, err, oops.CodeForbidden)
}

func TestMintMcpCredential_ChildCannotMint(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)

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
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)

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
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	child := mintMCPCredential(t, ti, parent.key)

	_, err := keysrepo.New(ti.conn).DeleteAgentAPIKey(ctx, keysrepo.DeleteAgentAPIKeyParams{ID: parent.id, OrganizationID: ti.orgID, SubjectUrn: conv.ToPGText(parent.actor.String())})
	require.NoError(t, err)

	_, err = authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_ParentExpiryInvalidatesChild(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	child := mintMCPCredential(t, ti, parent.key)

	require.NoError(t, testrepo.New(ti.conn).ExpireAPIKeyFixture(ctx, parent.id))

	_, err := authorizeMethod(t, ti, child.Key, "getPlugins")
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_SuspendedAgentIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	child := mintMCPCredential(t, ti, parent.key)

	_, err := agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{OrganizationID: ti.orgID, ID: parent.agentID})
	require.NoError(t, err)

	_, err = authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.Error(t, err)
	_, err = authorizeMethod(t, ti, child.Key, "getPlugins")
	require.Error(t, err)
}

func TestMintMcpCredential_NonAgentCallersAreRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// The test context is a human session.
	_, err := ti.service.MintMcpCredential(ctx, &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeForbidden)

	// No credential at all.
	_, err = ti.service.MintMcpCredential(t.Context(), &gen.MintMcpCredentialPayload{})
	requireCode(t, err, oops.CodeUnauthorized)
}

func TestMintMcpCredential_RejectsBadExpiry(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	parent := enrollWithMCP(t, ctx, ti, 24*time.Hour)
	admitted, err := authorizeMethod(t, ti, parent.key, "mintMcpCredential")
	require.NoError(t, err)

	for _, raw := range []string{"tomorrow", time.Now().Add(-time.Hour).Format(time.RFC3339), time.Now().Add(400 * 24 * time.Hour).Format(time.RFC3339)} {
		_, err := ti.service.MintMcpCredential(admitted, &gen.MintMcpCredentialPayload{ExpiresAt: &raw})
		requireCode(t, err, oops.CodeBadRequest)
	}
}
