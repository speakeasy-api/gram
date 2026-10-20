package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type fakeDirectoryReviewBackend struct {
	listed      *accessgen.ListDirectoryReviewItemsResult
	afterList   *accessgen.ListDirectoryReviewItemsResult
	resolved    *accessgen.ResolveDirectoryReviewItemResult
	resolveErr  error
	resolves    []*accessgen.ResolveDirectoryReviewItemPayload
	authContext *contextvalues.AuthContext
}

func (f *fakeDirectoryReviewBackend) ListDirectoryReviewItems(ctx context.Context, _ *accessgen.ListDirectoryReviewItemsPayload) (*accessgen.ListDirectoryReviewItemsResult, error) {
	f.authContext, _ = contextvalues.GetAuthContext(ctx)
	if len(f.resolves) > 0 && f.afterList != nil {
		return f.afterList, nil
	}
	return f.listed, nil
}

func (f *fakeDirectoryReviewBackend) ResolveDirectoryReviewItem(ctx context.Context, payload *accessgen.ResolveDirectoryReviewItemPayload) (*accessgen.ResolveDirectoryReviewItemResult, error) {
	f.authContext, _ = contextvalues.GetAuthContext(ctx)
	f.resolves = append(f.resolves, payload)
	return f.resolved, f.resolveErr
}

func refusalText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func testDirectoryReviewItem() *accessgen.DirectoryReviewItem {
	groupID := "00000000-0000-0000-0000-000000000010"
	reuse := "role:organization:00000000-0000-0000-0000-000000000020"
	return &accessgen.DirectoryReviewItem{
		ID: "00000000-0000-0000-0000-000000000001", Kind: "new_group", Occurrence: 1, CreatedAt: "2026-01-01T00:00:00Z",
		Item: &accessgen.DirectoryRoleSetupPreviewItem{
			Source:     &accessgen.DirectoryRoleSetupSource{SourceKind: "group", DirectoryGroupID: &groupID},
			SourceName: "Platform", DirectoryPeople: 12, ExistingRoleUrns: []string{},
			ProposedRoles: []*accessgen.DirectoryRoleSetupProposedRole{{RoleAction: "reuse", RoleUrn: &reuse, RoleName: "Platform"}},
			Version:       "v1",
		},
	}
}

func TestDirectoryReviewToolDescriptors(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerDirectoryReviewTools(registrar, nil)

	list := descriptorByName(t, registrar, operationListDirectoryReviewItems)
	require.True(t, list.Annotations.ReadOnlyHint)
	resolve := descriptorByName(t, registrar, operationResolveDirectoryReviewItem)
	require.False(t, resolve.Annotations.ReadOnlyHint)
	require.True(t, *resolve.Annotations.DestructiveHint, "approving a missing source removes mappings")
	require.Contains(t, resolve.Description, "confirmed: true")
	for _, descriptor := range []Descriptor{list, resolve} {
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeNone, descriptor.Meta.ProjectScope)
	}
	assistant := names(registrar.For(AudienceAssistant))
	require.NotContains(t, assistant, operationListDirectoryReviewItems)
	require.NotContains(t, assistant, operationResolveDirectoryReviewItem)
}

func TestDirectoryReviewItemsCarrySourcesNotPeople(t *testing.T) {
	t.Parallel()

	output := directoryReviewItems(&accessgen.ListDirectoryReviewItemsResult{Items: []*accessgen.DirectoryReviewItem{testDirectoryReviewItem()}})
	require.Len(t, output.Items, 1)
	item := output.Items[0]
	require.Equal(t, "Platform", item.Source.Name)
	require.Equal(t, "group", item.Source.Kind)
	require.EqualValues(t, 12, item.DirectoryPeople)
	require.Equal(t, "v1", item.Version)
	require.Equal(t, []DirectoryReviewProposedRole{{Action: "reuse", Name: "Platform", RoleURN: "role:organization:00000000-0000-0000-0000-000000000020"}}, item.ProposedRoles)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "email", "directory people are counted, never listed")
}

func TestResolveDirectoryReviewItemRequiresConfirmation(t *testing.T) {
	t.Parallel()

	backend := &fakeDirectoryReviewBackend{}
	_, err := resolveDirectoryReviewItem(t.Context(), backend, ResolveDirectoryReviewItemInput{ItemID: "item", Decision: "dismiss"})
	result, ok := directoryReviewToolResult(err)
	require.True(t, ok)
	require.Contains(t, refusalText(t, result), "confirmation_required")
	require.Empty(t, backend.resolves, "nothing is decided before confirmation")
}

func TestResolveDirectoryReviewItemUsesTheServicePathAndRereads(t *testing.T) {
	t.Parallel()

	backend := &fakeDirectoryReviewBackend{
		resolved: &accessgen.ResolveDirectoryReviewItemResult{
			State:        "approved",
			CreatedRoles: []*accessgen.Role{{PrincipalUrn: "role:new", Name: "Platform"}},
			Mappings:     []*accessgen.DirectoryRoleMapping{{RoleUrn: "role:new"}},
			SideEffects:  []*accessgen.DirectoryRoleSetupSideEffect{{Kind: "workos_role_sync", RoleID: "r", RequestStatus: "requested"}},
		},
		afterList: &accessgen.ListDirectoryReviewItemsResult{Items: []*accessgen.DirectoryReviewItem{}},
	}
	principal := Principal{OrganizationID: "org-1", UserID: "user-1"}
	ctx := directoryReviewContext(t.Context(), principal)

	output, err := resolveDirectoryReviewItem(ctx, backend, ResolveDirectoryReviewItemInput{
		ItemID: " item ", Decision: "approve", Version: "v1", Confirmed: true,
		Roles: []DirectoryReviewRoleChoice{{CreateName: "Platform"}},
	})
	require.NoError(t, err)
	require.Len(t, backend.resolves, 1)
	payload := backend.resolves[0]
	require.Equal(t, "item", payload.ID)
	require.Equal(t, "approve", payload.Decision)
	require.Equal(t, "v1", *payload.Version)
	require.Len(t, payload.Roles, 1)
	require.Equal(t, "Platform", *payload.Roles[0].CreateName)
	require.Nil(t, payload.Roles[0].RoleUrn)
	require.Empty(t, payload.GrantSet, "created roles get no grants through MCP")

	require.Equal(t, "org-1", backend.authContext.ActiveOrganizationID)
	require.Equal(t, "user-1", backend.authContext.UserID)
	require.Equal(t, "approved", output.State)
	require.Equal(t, []DirectoryReviewCreatedRole{{RoleURN: "role:new", Name: "Platform"}}, output.CreatedRoles)
	require.Equal(t, []string{"workos_role_sync"}, output.RequestedWork)
	require.Zero(t, output.PendingItems)
	require.Contains(t, output.NextSafeActions, "grant nothing")
}

func TestDirectoryReviewRefusals(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		code oops.Code
		want string
	}{
		{oops.CodeConflict, "conflict"},
		{oops.CodeNotFound, "not_found"},
		{oops.CodeForbidden, "permission_denied"},
		{oops.CodeBadRequest, "invalid_request"},
	} {
		backend := &fakeDirectoryReviewBackend{resolveErr: oops.E(tc.code, nil, "refused")}
		_, err := resolveDirectoryReviewItem(t.Context(), backend, ResolveDirectoryReviewItemInput{ItemID: "item", Decision: "dismiss", Confirmed: true})
		result, ok := directoryReviewToolResult(err)
		require.True(t, ok, tc.want)
		require.True(t, result.IsError)
		var refusal directoryReviewRefusal
		require.NoError(t, json.Unmarshal([]byte(refusalText(t, result)), &refusal))
		require.Equal(t, tc.want, refusal.Code)
	}

	result, ok := directoryReviewToolResult(directoryReviewUnavailable())
	require.True(t, ok)
	require.Contains(t, refusalText(t, result), unavailableCode)
}
