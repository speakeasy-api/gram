package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/stretchr/testify/require"
)

type fakeRoleProvisioning struct {
	status            roleprovisioning.Status
	reads             int
	writes            []roleprovisioning.ConfigureInput
	configureErr      error
	readErrAfterWrite bool
	readErr           bool
}

func (f *fakeRoleProvisioning) Status(context.Context, string) (roleprovisioning.Status, error) {
	f.reads++
	if f.readErr || (f.readErrAfterWrite && len(f.writes) > 0) {
		return roleprovisioning.Status{}, errors.New("private provider diagnostic")
	}
	return f.status, nil
}
func (f *fakeRoleProvisioning) Configure(_ context.Context, in roleprovisioning.ConfigureInput) (int64, error) {
	if f.configureErr != nil {
		return 0, f.configureErr
	}
	f.writes = append(f.writes, in)
	f.status.Version++
	f.status.Enabled = in.Enabled
	return f.status.Version, nil
}
func provisioningTestContext(t *testing.T) (context.Context, Principal) {
	t.Helper()
	p := testPrincipal()
	return authz.GrantsToContext(contextWithPrincipal(t.Context(), p), []authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, p.OrganizationID)}), p
}
func provisioningTestInput(p Principal) ConfigureRoleProvisioningInput {
	return ConfigureRoleProvisioningInput{OrganizationID: p.OrganizationID, ProjectID: uuid.NewString(), ExpectedVersion: 0, Enabled: true, Confirmed: true, Roles: []ConfigureRoleProvisioningSelection{}}
}

func TestRoleProvisioningDiscoveryAndAudience(t *testing.T) {
	t.Parallel()
	reg := newRegistrar(newTestMCPServer())
	registerRoleProvisioningTool(reg, nil)
	d := reg.Descriptors()[0]
	p := testPrincipal()
	require.Equal(t, ExternalAuthorizationOrgAdmin, d.Meta.Authorization)
	// The outcome spans multiple explicitly selected project destinations; it is
	// organization-scoped, not an implicit assistant-project mutation.
	require.Equal(t, ProjectScopeNone, d.Meta.ProjectScope)
	require.Contains(t, names(reg.For(AudienceExternal)), d.Name)
	require.NotContains(t, names(reg.For(AudienceAssistant)), d.Name)
	require.False(t, d.Annotations.IdempotentHint)
	require.True(t, *d.Annotations.DestructiveHint)
	require.False(t, externalToolDiscoverable(nil, false, p, d.Meta))
	require.False(t, externalToolDiscoverable([]authz.Grant{authz.NewGrant(authz.ScopeOrgRead, p.OrganizationID)}, true, p, d.Meta))
	require.False(t, externalToolDiscoverable([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, "org_other")}, true, p, d.Meta))
	require.True(t, externalToolDiscoverable([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, p.OrganizationID)}, true, p, d.Meta))
	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(d.InputSchema, &schema))
	require.ElementsMatch(t, []string{"organization_id", "project_id", "expected_version", "enabled", "roles", "confirmed"}, schema.Required)
	ctx, _ := provisioningTestContext(t)
	body, err := json.Marshal(provisioningTestInput(p))
	require.NoError(t, err)
	_, err = d.Invoke(ctx, body)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, unavailableCode, refusal.Code)
}

func requireRoleProvisioningContextPrivacy(t *testing.T, surface ActingSurface, scope authz.Scope, visible bool) {
	t.Helper()
	p := testPrincipal()
	p.Surface = surface
	ctx := authz.GrantsToContext(contextWithPrincipal(t.Context(), p), []authz.Grant{authz.NewGrant(scope, p.OrganizationID)})
	backend := &fakeRoleProvisioning{status: roleprovisioning.Status{Version: 7, Enabled: true}}
	reg := newRegistrar(newTestMCPServer())
	registerGetPlatformContextTool(reg, backend)
	out, err := reg.Descriptors()[0].Invoke(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	result, ok := out.(PlatformContext)
	require.True(t, ok)
	require.Empty(t, result.RoleProvisioningUnavailable)
	if visible {
		require.NotNil(t, result.RoleProvisioning)
		require.EqualValues(t, 7, result.RoleProvisioning.Version)
		require.Equal(t, 1, backend.reads)
	} else {
		require.Nil(t, result.RoleProvisioning)
		require.Zero(t, backend.reads)
	}
}

func requireContextWithUnavailableProvisioning(t *testing.T, backend roleProvisioningBackend) {
	t.Helper()
	ctx, principal := provisioningTestContext(t)
	reg := newRegistrar(newTestMCPServer())
	registerGetPlatformContextTool(reg, backend)
	out, err := reg.Descriptors()[0].Invoke(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	result, ok := out.(PlatformContext)
	require.True(t, ok)
	require.Equal(t, principal.OrganizationID, result.OrganizationID)
	require.Equal(t, principal.ConnectionID, result.ConnectionID)
	require.Equal(t, platformOverview, result.Overview)
	available, requestable := platformWorkflowCategories([]authz.Grant{authz.NewGrant(authz.ScopeOrgAdmin, principal.OrganizationID)}, principal.OrganizationID)
	require.Equal(t, available, result.AvailableWorkflows)
	require.Equal(t, requestable, result.RequestableWorkflows)
	require.Nil(t, result.RoleProvisioning)
	require.Contains(t, result.RoleProvisioningUnavailable, "temporarily unavailable")
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(body), `"role_provisioning_unavailable"`)
	require.NotContains(t, string(body), `"role_provisioning":`)
	require.NotContains(t, string(body), "private provider diagnostic")
}

func TestRoleProvisioningContextPreservesCoreOnStatusFailure(t *testing.T) {
	t.Parallel()
	backend := &fakeRoleProvisioning{readErr: true}
	requireContextWithUnavailableProvisioning(t, backend)
	require.Equal(t, 1, backend.reads)
}

func TestRoleProvisioningContextPreservesCoreWithoutBackend(t *testing.T) {
	t.Parallel()
	requireContextWithUnavailableProvisioning(t, nil)
}

func TestRoleProvisioningContextExternalAdmin(t *testing.T) {
	t.Parallel()
	requireRoleProvisioningContextPrivacy(t, SurfacePlatformMCP, authz.ScopeOrgAdmin, true)
}

func TestRoleProvisioningContextExternalMember(t *testing.T) {
	t.Parallel()
	requireRoleProvisioningContextPrivacy(t, SurfacePlatformMCP, authz.ScopeOrgRead, false)
}

func TestRoleProvisioningContextManagedAssistantWithAdminGrants(t *testing.T) {
	t.Parallel()
	requireRoleProvisioningContextPrivacy(t, SurfaceProjectAssistant, authz.ScopeOrgAdmin, false)
}

func requireRoleProvisioningMutationRefused(t *testing.T, ctx context.Context, p Principal, in ConfigureRoleProvisioningInput) {
	t.Helper()
	f := &fakeRoleProvisioning{}
	_, err := configureRoleProvisioning(ctx, p, f, in)
	require.Error(t, err)
	require.Empty(t, f.writes)
}

func TestRoleProvisioningMutationRejectsWithoutConfirmation(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.Confirmed = false
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsOtherTenant(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.OrganizationID = "org_other"
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsWithoutDestination(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.ProjectID = ""
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsNegativeVersion(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.ExpectedVersion = -1
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsStaleVersion(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.ExpectedVersion = 1
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsOversizedRoles(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.Roles = make([]ConfigureRoleProvisioningSelection, 101)
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsInvalidRoleDestination(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.Roles = []ConfigureRoleProvisioningSelection{{ProjectID: "invalid"}}
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsManagedAssistant(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	p.Surface = SurfaceProjectAssistant
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningMutationRejectsWithoutGrants(t *testing.T) {
	t.Parallel()
	_, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	ctx := contextWithPrincipal(t.Context(), p)
	requireRoleProvisioningMutationRefused(t, ctx, p, in)
}

func TestRoleProvisioningConfigureAndRetry(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	in := provisioningTestInput(p)
	in.ProjectID = uuid.Nil.String()
	in.Roles = []ConfigureRoleProvisioningSelection{{RoleURN: "role-test", Enabled: false, ProjectID: uuid.Nil.String()}}
	f := &fakeRoleProvisioning{}
	out, err := configureRoleProvisioning(ctx, p, f, in)
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Version)
	require.EqualValues(t, 1, out.Status.Version)
	require.True(t, out.Status.Enabled)
	require.Len(t, f.writes, 1)
	require.Equal(t, p.OrganizationID, f.writes[0].OrganizationID)
	require.Equal(t, p.UserID, f.writes[0].Actor.PublicationUserID)
	require.Equal(t, uuid.Nil, *f.writes[0].ProjectID)
	require.Equal(t, uuid.Nil, *f.writes[0].Roles[0].ProjectID)
	require.Equal(t, 2, f.reads)
	_, err = configureRoleProvisioning(ctx, p, f, in)
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "version_conflict", refusal.Code)
	require.Len(t, f.writes, 1)
}

func TestRoleProvisioningCommittedButReadUnavailable(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	f := &fakeRoleProvisioning{readErrAfterWrite: true}
	out, err := configureRoleProvisioning(ctx, p, f, provisioningTestInput(p))
	require.NoError(t, err)
	require.EqualValues(t, 1, out.Version)
	require.Nil(t, out.Status)
	require.Contains(t, out.NextAction, "committed")
	require.NotContains(t, out.NextAction, "private provider")
}

func TestRoleProvisioningRefusalsSanitizeBackendErrors(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		err  error
		code string
	}{{roleprovisioning.ErrConflict, "version_conflict"}, {roleprovisioning.ErrInvalid, "invalid_configuration"}, {errors.New("private provider diagnostic"), unavailableCode}} {
		ctx, p := provisioningTestContext(t)
		f := &fakeRoleProvisioning{configureErr: test.err}
		_, err := configureRoleProvisioning(ctx, p, f, provisioningTestInput(p))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		require.Equal(t, test.code, refusal.Code)
		require.NotContains(t, refusal.Payload, "private provider")
	}
}

func TestRoleProvisioningContextBoundedAndLive(t *testing.T) {
	t.Parallel()
	id := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	status := roleprovisioning.Status{Roles: make([]roleprovisioning.RoleStatus, 101), Projects: make([]roleprovisioning.ProjectOption, 101)}
	status.Roles[0] = roleprovisioning.RoleStatus{Enabled: false, ProjectID: id, AppliedProjectID: id, PluginID: id, OriginAudience: "assigned", PublicationStatus: "published_before", PendingReason: "private provider diagnostic"}
	out := provisioningContext(status)
	require.True(t, out.Truncated)
	require.Len(t, out.Roles, 100)
	require.Len(t, out.Projects, 100)
	require.False(t, out.Roles[0].Enabled)
	require.Equal(t, "assigned", out.Roles[0].OriginAudience)
	require.Equal(t, "published_before", out.Roles[0].PublicationStatus)
	require.Equal(t, "reconciliation_pending", out.Roles[0].PendingReason)
	require.Equal(t, id.UUID.String(), out.Roles[0].PluginID)
}

func TestRoleProvisioningTruncatedPreviewCannotConfirm(t *testing.T) {
	t.Parallel()
	ctx, p := provisioningTestContext(t)
	f := &fakeRoleProvisioning{status: roleprovisioning.Status{Roles: make([]roleprovisioning.RoleStatus, 101)}}
	_, err := configureRoleProvisioning(ctx, p, f, provisioningTestInput(p))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, "configuration_too_large", refusal.Code)
	require.Empty(t, f.writes)
}
