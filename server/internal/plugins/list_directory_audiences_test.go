package plugins_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
)

func TestPluginsService_ListAudiences(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	now := time.Now().UTC()
	directoryRepo := directoryrepo.New(ti.conn)
	roleURN := createTestRolePrincipal(t, ctx, ti, "engineering")
	groupWorkOSID := uuid.NewString()
	groupID, err := directoryRepo.UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		WorkosDirectoryGroupID: groupWorkOSID,
		Name:                   "Engineering",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(now),
		WorkosLastEventID:      conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	emptyGroupID, err := directoryRepo.UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		WorkosDirectoryGroupID: uuid.NewString(),
		Name:                   "Empty",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(now),
		WorkosLastEventID:      conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	directoryUserWorkOSID := uuid.NewString()
	directoryUserID, err := directoryRepo.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserWorkOSID,
		Email:                 conv.ToPGText("member@example.com"),
		Attributes:            []byte(`{"department":"engineering","title":"developer"}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGTextEmpty(""),
		RestoreDeleted:        true,
	})
	require.NoError(t, err)
	directoryUserTwoWorkOSID := uuid.NewString()
	directoryUserTwoID, err := directoryRepo.UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        authCtx.ActiveOrganizationID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserTwoWorkOSID,
		Email:                 conv.ToPGText(" MEMBER@example.com "),
		Attributes:            []byte(`{"department":"engineering","unused":null}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGTextEmpty(""),
		RestoreDeleted:        true,
	})
	require.NoError(t, err)
	_, err = directoryRepo.OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserTwoID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  directoryUserTwoWorkOSID,
		WorkosDirectoryGroupID: groupWorkOSID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
	})
	require.NoError(t, err)
	_, err = directoryRepo.OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserID,
		DirectoryGroupID:       groupID,
		WorkosDirectoryUserID:  directoryUserWorkOSID,
		WorkosDirectoryGroupID: groupWorkOSID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(now),
	})
	require.NoError(t, err)

	result, err := ti.service.ListAudiences(ctx, &gen.ListAudiencesPayload{})
	require.NoError(t, err)
	zero := int64(0)
	one := int64(1)
	require.Equal(t, &gen.PluginAudience{Kind: "everyone", DisplayName: "Everyone", PrincipalUrn: "*"}, result.Audiences[0])
	require.Contains(t, result.Audiences, &gen.PluginAudience{Kind: "role", DisplayName: "engineering", MemberCount: &zero, PrincipalUrn: roleURN})
	require.ElementsMatch(t, []*gen.PluginAudience{
		{
			Kind:         "directory_group",
			DisplayName:  "Empty",
			MemberCount:  &zero,
			PrincipalUrn: directory.GroupPrincipal(emptyGroupID),
		},
		{
			Kind:         "directory_group",
			DisplayName:  "Engineering",
			MemberCount:  &one,
			PrincipalUrn: directory.GroupPrincipal(groupID),
		},
		{
			Kind:         "directory_attribute",
			DisplayName:  "department: engineering",
			MemberCount:  &one,
			PrincipalUrn: directory.AttributePrincipal("department", "engineering"),
		},
		{
			Kind:         "directory_attribute",
			DisplayName:  "title: developer",
			MemberCount:  &one,
			PrincipalUrn: directory.AttributePrincipal("title", "developer"),
		},
	}, result.Audiences[2:])
}
