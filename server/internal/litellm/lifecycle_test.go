package litellm

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"goa.design/goa/v3/security"

	keysgen "github.com/speakeasy-api/gram/server/gen/keys"
	gen "github.com/speakeasy-api/gram/server/gen/litellm"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestInstanceLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	first, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "production", FailurePosture: ""})
	require.NoError(t, err)
	require.NotEmpty(t, first.Key)
	require.True(t, strings.HasPrefix(first.Key, auth.APIKeyPrefix("local")))
	require.Equal(t, gen.LiteLLMFailurePosture("fail_closed"), first.Instance.FailurePosture)
	require.True(t, first.Instance.Active)
	require.Nil(t, first.Instance.LastUsedAt)
	require.Equal(t, authCtx.ProjectID.String(), first.Instance.Project.ID)
	oldHash, err := auth.GetAPIKeyHash(first.Key)
	require.NoError(t, err)
	managedKey, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, oldHash)
	require.NoError(t, err)
	instanceID, err := uuid.Parse(first.Instance.ID)
	require.NoError(t, err)
	encodedInstanceID, ok := auth.LiteLLMInstanceIDFromAPIKeyName(managedKey.Name)
	require.True(t, ok)
	require.Equal(t, instanceID, encodedInstanceID)
	genericList, err := ti.keys.ListKeys(ctx, &keysgen.ListKeysPayload{})
	require.NoError(t, err)
	require.Empty(t, genericList.Keys)
	err = ti.keys.RevokeKey(ctx, &keysgen.RevokeKeyPayload{ID: managedKey.ID.String()})
	requireOops(t, err, oops.CodeConflict)

	_, err = ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "unsupported", FailurePosture: "fail_open"})
	requireOops(t, err, oops.CodeBadRequest)
	second, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "staging", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	require.Equal(t, gen.LiteLLMFailurePosture("fail_closed"), second.Instance.FailurePosture)

	listed, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Instances, 2)

	_, err = ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "production", FailurePosture: "fail_closed"})
	requireOops(t, err, oops.CodeConflict)

	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, oldHash)
	require.NoError(t, err)

	rotated, err := ti.service.RotateInstanceKey(ctx, &gen.RotateInstanceKeyPayload{ID: first.Instance.ID})
	require.NoError(t, err)
	require.NotEqual(t, first.Key, rotated.Key)
	resolvedID, managed := ti.service.instances.Resolve(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), managedKey.ID.String())
	require.True(t, managed)
	require.Equal(t, instanceID, resolvedID)
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, oldHash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	newHash, err := auth.GetAPIKeyHash(rotated.Key)
	require.NoError(t, err)
	rotatedKey, err := keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, newHash)
	require.NoError(t, err)

	require.NoError(t, ti.service.RevokeInstance(ctx, &gen.RevokeInstancePayload{ID: first.Instance.ID}))
	resolvedID, managed = ti.service.instances.Resolve(ctx, authCtx.ActiveOrganizationID, authCtx.ProjectID.String(), rotatedKey.ID.String())
	require.True(t, managed)
	require.Equal(t, instanceID, resolvedID)
	_, err = keysrepo.New(ti.conn).GetAPIKeyByKeyHash(ctx, newHash)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	listed, err = ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Instances, 2)
	require.False(t, instanceByName(t, listed, "production").Active)
	require.True(t, instanceByName(t, listed, "staging").Active)

	requireAuditCount(t, ctx, ti, audit.ActionLiteLLMInstanceCreate, 2)
	requireAuditCount(t, ctx, ti, audit.ActionLiteLLMInstanceRotateKey, 1)
	requireAuditCount(t, ctx, ti, audit.ActionLiteLLMInstanceRevoke, 1)
	requireAuditCount(t, ctx, ti, audit.ActionKeyCreate, 3)
	requireAuditCount(t, ctx, ti, audit.ActionKeyRevoke, 2)
	latest, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionLiteLLMInstanceRotateKey)
	require.NoError(t, err)
	require.Empty(t, latest.Metadata)
	before, err := audittest.DecodeAuditData(latest.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(latest.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, first.Instance.KeyPrefix, before["key_prefix"])
	require.Equal(t, rotated.Instance.KeyPrefix, after["key_prefix"])
	require.Equal(t, "production", before["name"])
	require.Equal(t, "fail_closed", after["failure_posture"])
	require.NotContains(t, before, "key")
	require.NotContains(t, before, "key_hash")
	require.NotContains(t, after, "key")
	require.NotContains(t, after, "key_hash")
}

func TestManagedKeyLastAccessedOnlyAfterProjectAuthorization(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "access-time", FailurePosture: "fail_closed"})
	require.NoError(t, err)
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "last-access-other", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)

	keyScheme := &security.APIKeyScheme{Name: constants.KeySecurityScheme, Scopes: []string{}, RequiredScopes: []string{"hooks"}}
	projectScheme := &security.APIKeyScheme{Name: constants.ProjectSlugSecuritySchema, Scopes: []string{}, RequiredScopes: []string{}}
	keyCtx, err := ti.service.APIKeyAuth(t.Context(), created.Key, keyScheme)
	require.NoError(t, err)
	_, err = ti.service.APIKeyAuth(keyCtx, otherProject.Slug, projectScheme)
	requireOops(t, err, oops.CodeForbidden)
	listed, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Nil(t, instanceByName(t, listed, "access-time").LastUsedAt)

	keyCtx, err = ti.service.APIKeyAuth(t.Context(), created.Key, keyScheme)
	require.NoError(t, err)
	_, err = ti.service.APIKeyAuth(keyCtx, string(created.Instance.Project.Slug), projectScheme)
	require.NoError(t, err)
	listed, err = ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.NotNil(t, instanceByName(t, listed, "access-time").LastUsedAt)
}

func TestInstanceLifecycleRequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListInstances(ctx, &gen.ListInstancesPayload{})
	requireOops(t, err, oops.CodeForbidden)
}

func TestInstanceProjectAndOrganizationIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newRealTestService(t, nil)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	created, err := ti.service.CreateInstance(ctx, &gen.CreateInstancePayload{Name: "isolated", FailurePosture: "fail_closed"})
	require.NoError(t, err)

	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-project", OrganizationID: authCtx.ActiveOrganizationID})
	require.NoError(t, err)
	projectContext := *authCtx
	projectContext.ProjectID = &otherProject.ID
	ctxOtherProject := contextvalues.SetAuthContext(ctx, &projectContext)
	otherList, err := ti.service.ListInstances(ctxOtherProject, &gen.ListInstancesPayload{})
	require.NoError(t, err)
	require.Empty(t, otherList.Instances)
	_, err = ti.service.RotateInstanceKey(ctxOtherProject, &gen.RotateInstanceKeyPayload{ID: created.Instance.ID})
	requireOops(t, err, oops.CodeNotFound)

	otherOrgContext := *authCtx
	otherOrgContext.ActiveOrganizationID = "org_other"
	ctxOtherOrg := contextvalues.SetAuthContext(ctx, &otherOrgContext)
	_, err = ti.service.RotateInstanceKey(ctxOtherOrg, &gen.RotateInstanceKeyPayload{ID: created.Instance.ID})
	requireOops(t, err, oops.CodeNotFound)
}

func instanceByName(t *testing.T, result *gen.ListInstancesResult, name string) *gen.LiteLLMInstance {
	t.Helper()
	for _, instance := range result.Instances {
		if instance.Name == name {
			return instance
		}
	}
	require.FailNow(t, "instance not found", name)
	return nil
}

func requireAuditCount(t *testing.T, ctx context.Context, ti *realTestInstance, action audit.Action, expected int64) {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
	require.NoError(t, err)
	require.Equal(t, expected, count)
}

func requireOops(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
