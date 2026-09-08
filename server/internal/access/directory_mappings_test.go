package access

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_ListDirectoryMappings_emptyCatalog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	result, err := ti.service.ListDirectoryMappings(ctx, &gen.ListDirectoryMappingsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.Mappings)
	require.Empty(t, result.Groups)
	require.Empty(t, result.Attributes)
}

func TestService_DirectoryMappings_attributeAndGroupRoundTrip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	groupID := seedAccessDirectoryGroup(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, "Engineering")
	seedAccessDirectoryAttributes(t, ti, authCtx.ActiveOrganizationID, authCtx.UserID, `{"department":"Engineering","job_title":"I.T Admins"}`)

	beforeUpsert, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessDirectoryMappingUpsert)
	require.NoError(t, err)

	attributeURN := directory.AttributePrincipal("department", "Engineering")
	created, err := ti.service.UpsertDirectoryMapping(ctx, &gen.UpsertDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: attributeURN,
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeMCPConnect),
			Selectors: nil,
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "attribute", created.Kind)
	require.Equal(t, attributeURN, created.PrincipalUrn)
	require.NotNil(t, created.AttributeKey)
	require.Equal(t, "department", *created.AttributeKey)
	require.NotNil(t, created.AttributeValue)
	require.Equal(t, "Engineering", *created.AttributeValue)
	require.Len(t, created.Grants, 1)
	require.Equal(t, string(authz.ScopeMCPConnect), created.Grants[0].Scope)

	groupURN := directory.GroupPrincipal(groupID)
	_, err = ti.service.UpsertDirectoryMapping(ctx, &gen.UpsertDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: groupURN,
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeOrgAdmin),
			Selectors: nil,
		}},
	})
	require.NoError(t, err)

	afterUpsert, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessDirectoryMappingUpsert)
	require.NoError(t, err)
	require.Equal(t, beforeUpsert+2, afterUpsert)

	listed, err := ti.service.ListDirectoryMappings(ctx, &gen.ListDirectoryMappingsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Mappings, 2)
	require.Len(t, listed.Groups, 1)
	require.Equal(t, "Engineering", listed.Groups[0].Name)
	require.GreaterOrEqual(t, len(listed.Attributes), 1)

	beforeDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessDirectoryMappingDelete)
	require.NoError(t, err)

	err = ti.service.DeleteDirectoryMapping(ctx, &gen.DeleteDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: attributeURN,
	})
	require.NoError(t, err)

	afterDelete, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessDirectoryMappingDelete)
	require.NoError(t, err)
	require.Equal(t, beforeDelete+1, afterDelete)

	listed, err = ti.service.ListDirectoryMappings(ctx, &gen.ListDirectoryMappingsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Mappings, 1)
	require.Equal(t, groupURN, listed.Mappings[0].PrincipalUrn)
}

func TestService_UpsertDirectoryMapping_requiresGrantAndDirectoryPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)

	_, err := ti.service.UpsertDirectoryMapping(ctx, &gen.UpsertDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, "user_other").String(),
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeMCPConnect),
			Selectors: nil,
		}},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)

	_, err = ti.service.UpsertDirectoryMapping(ctx, &gen.UpsertDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: directory.AttributePrincipal("department", "Engineering"),
		Grants:       nil,
	})
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UpsertDirectoryMapping_forbiddenWithoutAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.UpsertDirectoryMapping(ctx, &gen.UpsertDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: directory.AttributePrincipal("department", "Engineering"),
		Grants: []*gen.RoleGrant{{
			Scope:     string(authz.ScopeMCPConnect),
			Selectors: nil,
		}},
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_ListDirectoryMappings_forbiddenWithoutRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	ctx = withRBACGrants(t, ctx)

	_, err := ti.service.ListDirectoryMappings(ctx, &gen.ListDirectoryMappingsPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestService_DeleteDirectoryMapping_notFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	err := ti.service.DeleteDirectoryMapping(ctx, &gen.DeleteDirectoryMappingPayload{
		ApikeyToken:  nil,
		SessionToken: nil,
		PrincipalUrn: directory.AttributePrincipal("department", "Missing"),
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func seedAccessDirectoryAttributes(t *testing.T, ti *testInstance, organizationID, userID, attributes string) {
	t.Helper()

	syncedAt := time.Now().UTC()
	_, err := directoryrepo.New(ti.conn).UpsertDirectoryUser(t.Context(), directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: "directory_" + userID,
		Email:                 conv.ToPGText(userID + "@example.com"),
		Attributes:            []byte(attributes),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:     conv.ToPGText("event_" + userID),
	})
	require.NoError(t, err)
}

func seedAccessDirectoryGroup(t *testing.T, ti *testInstance, organizationID, userID, groupName string) uuid.UUID {
	t.Helper()

	seedAccessDirectoryAttributes(t, ti, organizationID, userID, `{}`)
	syncedAt := time.Now().UTC()
	externalGroupID := "directory_group_" + groupName
	_, err := directoryrepo.New(ti.conn).UpsertDirectoryGroup(t.Context(), directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         organizationID,
		WorkosDirectoryGroupID: externalGroupID,
		Name:                   groupName,
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(syncedAt),
		WorkosLastEventID:      conv.ToPGText("event_" + externalGroupID),
	})
	require.NoError(t, err)

	group, err := directoryrepo.New(ti.conn).GetDirectoryGroupForMembershipByWorkOSID(t.Context(), externalGroupID)
	require.NoError(t, err)

	user, err := directoryrepo.New(ti.conn).GetDirectoryUserByWorkOSID(t.Context(), "directory_"+userID)
	require.NoError(t, err)

	_, err = directoryrepo.New(ti.conn).OpenDirectoryUserGroupMembership(t.Context(), directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        user.ID,
		DirectoryGroupID:       group.ID,
		WorkosDirectoryUserID:  user.WorkosDirectoryUserID,
		WorkosDirectoryGroupID: externalGroupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(syncedAt),
	})
	require.NoError(t, err)

	return group.ID
}
