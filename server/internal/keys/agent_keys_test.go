package keys_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestKeysService_AgentKeyLifecycle(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)
	payload := agentKeyPayload(agentID, projectID)
	createAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	revokeAuditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	before := time.Now().UTC()
	created, err := ti.service.CreateKey(ctx, payload)
	require.NoError(t, err)
	require.NotNil(t, created.Key)
	require.Contains(t, *created.Key, "gram_local_")
	require.Equal(t, "agent:"+agentID.String(), *created.SubjectUrn)
	require.Empty(t, created.Scopes)
	require.Nil(t, created.ProjectID)
	require.Equal(t, 1, *created.DelegatedGrantsVersion)
	require.Len(t, created.DelegatedGrants.Requested, 1)
	expiresAt, err := time.Parse(time.RFC3339Nano, *created.ExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, before.Add(90*24*time.Hour), expiresAt, 5*time.Second)

	authorized, err := ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	require.NoError(t, err)
	actor, ok := contextvalues.AuthenticatedActor(authorized)
	require.True(t, ok)
	require.Equal(t, urn.PrincipalTypeAgent, actor.Type)
	require.Equal(t, agentID.String(), actor.ID)
	authorizer, owner, ok := contextvalues.PrincipalCredentialProvenance(authorized)
	require.True(t, ok)
	require.Equal(t, testAuthContext(t, ctx).UserID, authorizer)
	require.Equal(t, testAuthContext(t, ctx).UserID, owner)

	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	require.Len(t, listed.Keys, 1)
	require.Nil(t, listed.Keys[0].Key, "secrets are returned only at issuance and rotation")

	legacyList, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{})
	require.NoError(t, err)
	require.Empty(t, legacyList.Keys, "legacy listing must not bypass agent-specific authorization")

	collisionPayload := agentKeyPayload(agentID, projectID)
	collisionPayload.Name = "rotation collision"
	collision, err := ti.service.CreateKey(ctx, collisionPayload)
	require.NoError(t, err)
	_, err = ti.service.RotateKey(ctx, &gen.RotateKeyPayload{
		ID: created.ID, Name: collision.Name, DelegatedGrantsVersion: 1, RequestedGrants: payload.RequestedGrants,
	})
	require.Error(t, err)
	_, err = ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	require.NoError(t, err, "a failed atomic rotation must leave the old credential active")

	rotated, err := ti.service.RotateKey(ctx, &gen.RotateKeyPayload{
		ID: created.ID, Name: created.Name, DelegatedGrantsVersion: 1, RequestedGrants: payload.RequestedGrants,
	})
	require.NoError(t, err)
	require.NotNil(t, rotated.Key)
	require.NotEqual(t, *created.Key, *rotated.Key)
	require.NotEqual(t, created.ID, rotated.ID)
	_, err = ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	require.Error(t, err, "rotation directly revokes the old row")
	_, err = ti.service.APIKeyAuth(t.Context(), *rotated.Key, agentAPIKeyScheme())
	require.NoError(t, err)

	ti.features.SetFlag(feature.FlagAgentCredentialsM2, testAuthContext(t, ctx).ActiveOrganizationID, false)
	require.NoError(t, ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: rotated.ID}), "rollout disablement must not strand active credentials")
	ti.features.SetFlag(feature.FlagAgentCredentialsM2, testAuthContext(t, ctx).ActiveOrganizationID, true)
	require.NoError(t, ti.service.RevokeKey(ctx, &gen.RevokeKeyPayload{ID: rotated.ID}), "direct revocation is idempotent")
	_, err = ti.service.APIKeyAuth(t.Context(), *rotated.Key, agentAPIKeyScheme())
	require.Error(t, err)

	createAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	require.Equal(t, createAuditBefore+3, createAuditAfter, "failed rotation audit must roll back")
	revokeAuditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, revokeAuditBefore+2, revokeAuditAfter, "rotation and direct revocation audit both key identities")
}

func TestKeysService_AgentKeyAllowsExactAuthorizeGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	ownerID := "owner-" + uuid.NewString()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: ownerID, Email: ownerID + "@example.com", DisplayName: ownerID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: authCtx.ActiveOrganizationID, UserID: conv.ToPGText(ownerID),
	})
	require.NoError(t, err)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: ownerID, Name: "delegated-agent-" + uuid.NewString(),
	})
	require.NoError(t, err)
	require.NotNil(t, authCtx.ProjectID)
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), authz.ScopeProjectRead, authCtx.ProjectID.String())
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, ownerID), authz.ScopeProjectRead, authCtx.ProjectID.String())
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeAgentAuthorize, agent.ID.String())

	created, err := ti.service.CreateKey(ctx, agentKeyPayload(agent.ID, *authCtx.ProjectID))
	require.NoError(t, err)
	require.Equal(t, authCtx.UserID, created.CreatedByUserID, "caller remains the immutable authorizer")

	_, err = accessrepo.New(ti.conn).DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrn:   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
	})
	require.NoError(t, err)
	_, err = ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agent.ID.String())})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestKeysService_AgentKeyRequiresOrdinaryHumanSession(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)
	payload := agentKeyPayload(agentID, projectID)

	apiKeyAuth := *testAuthContext(t, ctx)
	apiKeyAuth.APIKeyID = uuid.NewString()
	apiKeyCtx := contextvalues.SetAuthContext(ctx, &apiKeyAuth)
	_, err := ti.service.CreateKey(apiKeyCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	assistantCtx := contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	_, err = ti.service.CreateKey(assistantCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	supportAuth := *testAuthContext(t, ctx)
	supportAuth.IsAdmin = true
	supportAuth.SupportOrganizationID = supportAuth.ActiveOrganizationID
	supportCtx := contextvalues.WithValidatedSupportSession(ctx, &supportAuth)
	_, err = ti.service.CreateKey(supportCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestKeysService_AgentKeyParentAdmission(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)
	created, err := ti.service.CreateKey(ctx, agentKeyPayload(agentID, projectID))
	require.NoError(t, err)

	_, err = agentsrepo.New(ti.conn).SuspendAgent(ctx, agentsrepo.SuspendAgentParams{
		OrganizationID: testAuthContext(t, ctx).ActiveOrganizationID, ID: agentID,
	})
	require.NoError(t, err)

	_, err = ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

//nolint:paralleltest,tparallel // Subtests share mutable feature-flag state.
func TestKeysService_AgentKeyValidation(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)
	base := agentKeyPayload(agentID, projectID)

	t.Run("rollout gate fails closed", func(t *testing.T) {
		ti.features.SetFlag(feature.FlagAgentCredentialsM2, testAuthContext(t, ctx).ActiveOrganizationID, false)
		_, err := ti.service.CreateKey(ctx, base)
		requireOopsCode(t, err, oops.CodeNotFound)
		ti.features.SetFlag(feature.FlagAgentCredentialsM2, testAuthContext(t, ctx).ActiveOrganizationID, true)
	})

	tests := []struct {
		name   string
		mutate func(*gen.CreateKeyPayload)
		code   oops.Code
	}{
		{name: "unsupported policy version", mutate: func(p *gen.CreateKeyPayload) { p.DelegatedGrantsVersion = new(2) }, code: oops.CodeBadRequest},
		{name: "deny effect", mutate: func(p *gen.CreateKeyPayload) { p.RequestedGrants[0].Effect = "deny" }, code: oops.CodeBadRequest},
		{name: "duplicate grant", mutate: func(p *gen.CreateKeyPayload) {
			p.RequestedGrants = append(p.RequestedGrants, cloneGrant(p.RequestedGrants[0]))
		}, code: oops.CodeBadRequest},
		{name: "overbroad policy", mutate: func(p *gen.CreateKeyPayload) { p.RequestedGrants[0].Scope = string(authz.ScopeProjectWrite) }, code: oops.CodeForbidden},
		{name: "expired", mutate: func(p *gen.CreateKeyPayload) {
			value := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			p.ExpiresAt = &value
		}, code: oops.CodeBadRequest},
		{name: "over maximum expiry", mutate: func(p *gen.CreateKeyPayload) {
			value := time.Now().Add(366 * 24 * time.Hour).UTC().Format(time.RFC3339)
			p.ExpiresAt = &value
		}, code: oops.CodeBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := agentKeyPayload(agentID, projectID)
			test.mutate(payload)
			_, err := ti.service.CreateKey(ctx, payload)
			requireOopsCode(t, err, test.code)
		})
	}
}

func createAgentKeyFixture(t *testing.T, ctx context.Context, ti *testInstance) (uuid.UUID, uuid.UUID) {
	t.Helper()
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID, Name: "agent-" + uuid.NewString(),
	})
	require.NoError(t, err)

	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), authz.ScopeProjectRead, authCtx.ProjectID.String())
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeProjectRead, authCtx.ProjectID.String())
	return agent.ID, *authCtx.ProjectID
}

func upsertGrant(t *testing.T, ctx context.Context, ti *testInstance, principal urn.Principal, scope authz.Scope, resourceID string) {
	t.Helper()
	selectors, err := authz.NewSelector(scope, resourceID).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: testAuthContext(t, ctx).ActiveOrganizationID, PrincipalUrn: principal, Scope: string(scope), Selectors: selectors,
	})
	require.NoError(t, err)
}

func agentKeyPayload(agentID, projectID uuid.UUID) *gen.CreateKeyPayload {
	return &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "agent key", DelegatedGrantsVersion: new(1),
		RequestedGrants: []*gen.AgentPolicyGrantForm{{
			Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: projectID.String()},
		}},
	}
}

func agentAPIKeyScheme() *security.APIKeyScheme {
	return &security.APIKeyScheme{Name: constants.KeySecurityScheme, RequiredScopes: []string{auth.APIKeyScopeConsumer.String()}}
}

func cloneGrant(grant *gen.AgentPolicyGrantForm) *gen.AgentPolicyGrantForm {
	cloned := *grant
	selector := *grant.Selector
	cloned.Selector = &selector
	return &cloned
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
