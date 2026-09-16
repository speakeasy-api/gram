package identityproviders_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestListApplicationsReadsTwoPagesAndAssignmentCounts(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("app-one", "First app", "ACTIVE", "SAML_2_0", "https://apps.example.test/first", true),
		inventoryApplication("app-two", "Second app", "INACTIVE", "BOOKMARK", "https://apps.example.test/second", false),
	}, map[string][2]int{
		"app-one": {2, 3},
		"app-two": {0, 1},
	})
	fake.SetIndirectUserAssignments("app-one", 2)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Equal(t, 2, result.ApplicationCount)
	require.NotEmpty(t, result.ReadAt)
	require.Contains(t, result.Detail, "Assignment counts were read for every application.")
	require.Equal(t, []*gen.IdentityProviderApplication{
		{SourceApplicationID: "app-one", Label: "First app", ProviderStatus: new("ACTIVE"), SignOnURL: new("https://apps.example.test/first"), LogoURL: new("https://apps.example.test/logos/app-one.png"), GroupAssignmentCount: new(2), AssignedGroups: []*gen.IdentityProviderAssignedGroup{{SourceGroupID: "app-one-groups-0", Name: "Group app-one-groups-0"}, {SourceGroupID: "app-one-groups-1", Name: "Group app-one-groups-1"}}, AssignedGroupOverflow: new(0), UserAssignmentCount: new(3), Match: nil, Pickable: false, UnpickableReason: new("no_match")},
		{SourceApplicationID: "app-two", Label: "Second app", ProviderStatus: new("INACTIVE"), SignOnURL: new("https://apps.example.test/second"), LogoURL: nil, GroupAssignmentCount: new(0), AssignedGroups: []*gen.IdentityProviderAssignedGroup{}, AssignedGroupOverflow: new(0), UserAssignmentCount: new(1), Match: nil, Pickable: false, UnpickableReason: new("inactive")},
	}, result.Applications)
	require.Equal(t, 4, fake.AssignmentReads())
	require.Zero(t, fake.InventoryGroupReads())
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsOmitsAssignmentCountsAboveFifty(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	applications := make([]map[string]any, 51)
	for i := range applications {
		id := fmt.Sprintf("app-%02d", i)
		label := fmt.Sprintf("Application %02d", i)
		applications[i] = inventoryApplication(id, label, "ACTIVE", "SAML_2_0", "", false)
		ti.catalog.SetMatch(label, "provider", "catalog/"+id, label, "https://mcp.example.test/"+id)
	}
	ti.catalog.BlockFirstSearchesUntil(4)
	fake.SetApplicationInventory(applications, nil)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, 51, result.ApplicationCount)
	require.Contains(t, result.Detail, "Assignment counts were omitted because the tenant has more than 50 applications.")
	for _, application := range result.Applications {
		require.Nil(t, application.GroupAssignmentCount)
		require.Nil(t, application.AssignedGroups)
		require.Nil(t, application.AssignedGroupOverflow)
		require.Nil(t, application.UserAssignmentCount)
	}
	require.Zero(t, fake.AssignmentReads())
	searchCalls, inspectCalls, maxSearches := ti.catalog.Stats()
	require.Equal(t, 50, searchCalls)
	require.Equal(t, 50, inspectCalls)
	require.Equal(t, 4, maxSearches)
	require.Contains(t, result.Detail, "Speakeasy MCP server matching was capped at 50 applications.")
	for _, application := range result.Applications[:50] {
		require.True(t, application.Pickable)
		require.NotNil(t, application.Match)
	}
	require.False(t, result.Applications[50].Pickable)
	require.Equal(t, "no_match", *result.Applications[50].UnpickableReason)
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsMatchesCatalogueAndOrdersPickableFirst(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("unknown", "Zulu Unknown", "ACTIVE", "SAML_2_0", "", false),
		inventoryApplication("slack", "Slack App", "ACTIVE", "SAML_2_0", "", false),
		inventoryApplication("inactive", "Inactive Tool", "INACTIVE", "SAML_2_0", "", false),
		inventoryApplication("workspace", "Acme Workspace", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"unknown": {0, 0}, "slack": {0, 0}, "inactive": {0, 0}, "workspace": {0, 0}})
	ti.catalog.SetMatch("Slack App", "provider-slack", "catalog/slack", "Slack, Inc.", "https://mcp.example.test/slack")
	ti.catalog.SetMatch("Inactive Tool", "provider-inactive", "catalog/inactive", "Inactive Tool", "https://mcp.example.test/inactive")
	ti.catalog.SetMatch("Acme Workspace", "provider-acme", "catalog/acme", "Acme", "https://mcp.example.test/acme")
	ti.catalog.SetCandidates("Zulu Unknown", []identityproviders.ApplicationCatalogCandidate{
		{ProviderKey: "provider-one", CatalogRef: "catalog/one", Name: "Zulu"},
		{ProviderKey: "provider-two", CatalogRef: "catalog/two", Name: "Unknown"},
	})

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, []string{"Acme Workspace", "Slack App", "Inactive Tool", "Zulu Unknown"}, []string{
		result.Applications[0].Label,
		result.Applications[1].Label,
		result.Applications[2].Label,
		result.Applications[3].Label,
	})
	require.True(t, result.Applications[0].Pickable)
	require.Equal(t, "likely", result.Applications[0].Match.Confidence)
	require.Equal(t, "name", result.Applications[0].Match.Basis)
	require.Equal(t, "https://mcp.example.test/acme", result.Applications[0].Match.RemoteURL)
	require.True(t, result.Applications[1].Pickable)
	require.Equal(t, "exact", result.Applications[1].Match.Confidence)
	require.Equal(t, "Slack, Inc.", result.Applications[1].Match.Name)
	require.False(t, result.Applications[2].Pickable)
	require.NotNil(t, result.Applications[2].Match)
	require.Equal(t, "inactive", *result.Applications[2].UnpickableReason)
	require.False(t, result.Applications[3].Pickable)
	require.Nil(t, result.Applications[3].Match)
	require.Equal(t, "no_match", *result.Applications[3].UnpickableReason)
	searchCalls, inspectCalls, _ := ti.catalog.Stats()

	_, err = ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	cachedSearchCalls, cachedInspectCalls, _ := ti.catalog.Stats()
	require.Equal(t, searchCalls, cachedSearchCalls)
	require.Equal(t, inspectCalls, cachedInspectCalls)
}

func TestListApplicationsKeepsInventoryWhenCatalogueReadFails(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("broken", "Broken Catalogue", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"broken": {0, 0}})
	ti.catalog.SetSearchError("Broken Catalogue", errors.New("catalogue unavailable"))

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Applications, 1)
	require.Nil(t, result.Applications[0].Match)
	require.False(t, result.Applications[0].Pickable)
	require.Equal(t, "no_match", *result.Applications[0].UnpickableReason)
	require.Contains(t, result.Detail, "Speakeasy MCP server matching is partial because one or more catalogue reads failed.")
}

func TestListApplicationsMatchesTrailingGenericCatalogueName(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("atlassian", "Atlassian Cloud", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"atlassian": {0, 0}})
	ti.catalog.SetMatch("Atlassian Cloud", "provider-atlassian", "catalog/atlassian", "Atlassian", "https://mcp.example.test/atlassian")
	ti.catalog.SetCandidates("Atlassian Cloud", []identityproviders.ApplicationCatalogCandidate{
		{ProviderKey: "provider-atlassian", CatalogRef: "catalog/atlassian", Name: "Atlassian"},
		{ProviderKey: "provider-other", CatalogRef: "catalog/other", Name: "Unrelated Cloud"},
	})

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Applications, 1)
	require.True(t, result.Applications[0].Pickable)
	require.Equal(t, "Atlassian", result.Applications[0].Match.Name)
	require.Equal(t, "likely", result.Applications[0].Match.Confidence)
}

func TestListApplicationsKeepsInventoryWhenAnAssignmentReadFails(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("app-one", "First app", "ACTIVE", "SAML_2_0", "", false),
		inventoryApplication("app-two", "Second app", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"app-one": {1, 1}, "app-two": {1, 1}})
	fake.SetAssignmentFailure("app-two", "users")

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, 2, result.ApplicationCount)
	require.Contains(t, result.Detail, "Assignment counts are partial")
	require.NotNil(t, result.Applications[0].GroupAssignmentCount)
	require.Equal(t, 1, *result.Applications[1].GroupAssignmentCount)
	require.Nil(t, result.Applications[1].UserAssignmentCount)
}

func TestListApplicationsLimitsSlowApplicationAssignmentReads(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("slow", "Slow app", "ACTIVE", "SAML_2_0", "", false),
		inventoryApplication("fast", "Fast app", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"slow": {1, 1}, "fast": {1, 1}})
	fake.SetAssignmentStall("slow", "groups")

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Contains(t, result.Detail, "Assignment counts are partial")
	require.Equal(t, "slow/groups", <-fake.assignmentCanceled)
	require.Equal(t, "Fast app", result.Applications[0].Label)
	require.Equal(t, 1, *result.Applications[0].GroupAssignmentCount)
	require.Equal(t, 1, *result.Applications[0].UserAssignmentCount)
	require.Equal(t, "Slow app", result.Applications[1].Label)
	require.Nil(t, result.Applications[1].GroupAssignmentCount)
	require.Nil(t, result.Applications[1].UserAssignmentCount)
}

func TestListApplicationsCapsInventoryAtFiveHundred(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	applications := make([]map[string]any, 501)
	for i := range applications {
		id := fmt.Sprintf("app-%03d", i)
		applications[i] = inventoryApplication(id, fmt.Sprintf("Application %03d", i), "ACTIVE", "SAML_2_0", "", false)
	}
	fake.SetApplicationInventory(applications, nil)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Equal(t, 500, result.ApplicationCount)
	require.Len(t, result.Applications, 500)
	require.Contains(t, result.Detail, "capped at 500 applications")
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsDoesNotTruncateExactlyFiveHundred(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	applications := make([]map[string]any, 500)
	for i := range applications {
		id := fmt.Sprintf("app-%03d", i)
		applications[i] = inventoryApplication(id, fmt.Sprintf("Application %03d", i), "ACTIVE", "SAML_2_0", "", false)
	}
	fake.SetApplicationInventory(applications, nil)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Equal(t, 500, result.ApplicationCount)
	require.Len(t, result.Applications, 500)
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsReadsPaginatedAssignments(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("app-one", "First app", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"app-one": {201, 201}})
	fake.SetIndirectUserAssignments("app-one", 5)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Equal(t, 201, *result.Applications[0].GroupAssignmentCount)
	require.Len(t, result.Applications[0].AssignedGroups, 10)
	require.Equal(t, 191, *result.Applications[0].AssignedGroupOverflow)
	require.Equal(t, 201, *result.Applications[0].UserAssignmentCount)
	require.Equal(t, 4, fake.AssignmentReads())
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsFallsBackToIndividualGroupReads(t *testing.T) {
	t.Parallel()

	fake := newFakeOktaServer(t, fakeOktaPassed)
	ctx, ti := newTestServiceWithOktaEndpoint(t, fake.server.URL)
	prepareActiveConnection(t, ctx, ti, fake)
	fake.SetApplicationInventory([]map[string]any{
		inventoryApplication("app-one", "First app", "ACTIVE", "SAML_2_0", "", false),
	}, map[string][2]int{"app-one": {12, 0}})
	fake.SetEmbeddedInventoryGroups(false)

	result, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Len(t, result.Applications, 1)
	require.Equal(t, 12, *result.Applications[0].GroupAssignmentCount)
	require.Equal(t, 2, *result.Applications[0].AssignedGroupOverflow)
	require.Len(t, result.Applications[0].AssignedGroups, 10)
	for i, assignedGroup := range result.Applications[0].AssignedGroups {
		require.Equal(t, fmt.Sprintf("app-one-groups-%d", i), assignedGroup.SourceGroupID)
		require.Equal(t, fmt.Sprintf("Group app-one-groups-%d", i), assignedGroup.Name)
	}
	require.Equal(t, 10, fake.InventoryGroupReads())
	require.NoError(t, fake.ValidationError())
}

func TestListApplicationsRequiresOrganizationRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.ListApplications(ctx, &gen.ListApplicationsPayload{SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func inventoryApplication(id, label, status, signOnMode, signOnURL string, appLink bool) map[string]any {
	application := map[string]any{
		"id": id, "label": label, "status": status, "signOnMode": signOnMode,
		"settings": map[string]any{"app": map[string]any{"url": signOnURL}},
	}
	if appLink {
		application["_links"] = map[string]any{
			"appLinks": []map[string]any{{"href": signOnURL}},
			"logo": []map[string]any{
				{"name": "large", "href": "https://apps.example.test/logos/large.png", "type": "image/png"},
				{"name": "medium", "href": "https://apps.example.test/logos/" + id + ".png", "type": "image/png"},
			},
		}
	}
	if strings.TrimSpace(signOnURL) == "" {
		application["settings"] = map[string]any{"app": map[string]any{}}
	}
	return application
}
