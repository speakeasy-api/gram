package remotemcp_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestServerContext_TrackingUsesServerOwner(t *testing.T) {
	t.Parallel()
	for _, caller := range []string{"anonymous", "other organization", "other project"} {
		t.Run(caller, func(t *testing.T) {
			t.Parallel()
			owner := remotemcp.ServerContext{
				OrganizationID: "org-owner", OrganizationSlug: "owner",
				ProjectID: uuid.New(), ProjectSlug: "owner-project", AccountType: string(billing.TierBase),
			}
			ctx := t.Context()
			if caller != "anonymous" {
				organizationID := "org-caller"
				if caller == "other project" {
					organizationID = owner.OrganizationID
				}
				ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
					ActiveOrganizationID: organizationID, OrganizationSlug: "caller",
					ProjectID: new(uuid.New()), ProjectSlug: new("caller-project"), AccountType: string(billing.TierPro),
				})
			}
			ctx = remotemcp.WithServerContext(ctx, owner)
			toolTracker := newFakeBillingTracker()
			toolInterceptor := remotemcp.NewToolsCallUsageTrackingInterceptor(toolTracker, testenv.NewLogger(t))
			require.NoError(t, toolInterceptor.InterceptToolsCallResponse(ctx, newToolsCallResponseForInterceptor(t, "public-session")))
			resourceTracker := newFakeBillingTracker()
			resourceInterceptor := remotemcp.NewResourcesReadUsageTrackingInterceptor(resourceTracker, testenv.NewLogger(t))
			require.NoError(t, resourceInterceptor.InterceptResourcesReadResponse(ctx, newResourcesReadResponseForInterceptor(t, "public-session", "file:///example")))
			for _, event := range []billing.ToolCallUsageEvent{toolTracker.waitForEvent(t), resourceTracker.waitForEvent(t)} {
				require.Equal(t, owner.OrganizationID, event.OrganizationID)
				require.Equal(t, owner.ProjectID.String(), event.ProjectID)
				require.Equal(t, new(owner.OrganizationSlug), event.OrganizationSlug)
				require.Equal(t, new(owner.ProjectSlug), event.ProjectSlug)
				require.Equal(t, new("public-session"), event.MCPSessionID)
			}
			if caller == "anonymous" {
				_, authenticated := contextvalues.GetAuthContext(ctx)
				require.False(t, authenticated, "accounting must not install an authenticated identity")
			}
		})
	}
}

func TestServerContext_LimitsUseServerTier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		ownerTier   billing.Tier
		callerTier  billing.Tier
		noProject   bool
		storedUsage *usage.PeriodUsage
		storedErr   error
		rejected    bool
	}{
		{name: "anonymous free owner", ownerTier: billing.TierBase, storedUsage: &usage.PeriodUsage{ToolCalls: 200, IncludedToolCalls: 100}, rejected: true},
		{name: "owner without project", ownerTier: billing.TierBase, noProject: true, storedUsage: &usage.PeriodUsage{ToolCalls: 200, IncludedToolCalls: 100}, rejected: true},
		{name: "paid caller free owner", ownerTier: billing.TierBase, callerTier: billing.TierPro, storedUsage: &usage.PeriodUsage{ToolCalls: 200, IncludedToolCalls: 100}, rejected: true},
		{name: "free caller paid owner", ownerTier: billing.TierPro, callerTier: billing.TierBase},
		{name: "anonymous below cap", ownerTier: billing.TierBase, storedUsage: &usage.PeriodUsage{ToolCalls: 199, IncludedToolCalls: 100}},
		{name: "anonymous active subscription", ownerTier: billing.TierBase, storedUsage: &usage.PeriodUsage{ToolCalls: 200, IncludedToolCalls: 100, HasActiveSubscription: true}},
		{name: "anonymous cache unavailable", ownerTier: billing.TierBase, storedErr: errors.New("cache unavailable")},
		{name: "anonymous cache empty", ownerTier: billing.TierBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.callerTier != "" {
				ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{
					ActiveOrganizationID: "org-caller", AccountType: string(tc.callerTier),
				})
			}
			projectID := uuid.New()
			if tc.noProject {
				projectID = uuid.Nil
			}
			ctx = remotemcp.WithServerContext(ctx, remotemcp.ServerContext{
				OrganizationID: "org-owner", ProjectID: projectID, AccountType: string(tc.ownerTier),
			})
			repo := &fakeBillingRepo{storedUsage: tc.storedUsage, storedErr: tc.storedErr}
			toolInterceptor := remotemcp.NewToolsCallUsageLimitsInterceptor(repo, testenv.NewLogger(t))
			resourceInterceptor := remotemcp.NewResourcesReadUsageLimitsInterceptor(repo, testenv.NewLogger(t))
			for _, err := range []error{
				toolInterceptor.InterceptToolsCallRequest(ctx, newToolsCallRequestForInterceptor(t, ctx)),
				resourceInterceptor.InterceptResourcesReadRequest(ctx, newResourcesReadRequestForInterceptor(t, ctx)),
			} {
				if tc.rejected {
					var refusal *oops.ShareableError
					require.ErrorAs(t, err, &refusal)
					require.Equal(t, oops.CodeForbidden, refusal.Code)
				} else {
					require.NoError(t, err)
				}
			}
			if tc.ownerTier == billing.TierBase {
				require.Equal(t, "org-owner", repo.lastOrganizationID)
			} else {
				require.Empty(t, repo.lastOrganizationID)
			}
		})
	}
}

func TestUsageTracking_ServerCopiesDoNotLeakAttribution(t *testing.T) {
	t.Parallel()
	for _, serverID := range []string{"", "server-one", "server-two"} {
		t.Run("server="+serverID, func(t *testing.T) {
			t.Parallel()
			ctx := remotemcp.WithServerContext(t.Context(), remotemcp.ServerContext{
				OrganizationID: "org-owner", ProjectID: uuid.New(),
			})
			toolTracker, resourceTracker := newFakeBillingTracker(), newFakeBillingTracker()
			toolShared := remotemcp.NewToolsCallUsageTrackingInterceptor(toolTracker, testenv.NewLogger(t))
			resourceShared := remotemcp.NewResourcesReadUsageTrackingInterceptor(resourceTracker, testenv.NewLogger(t))
			tools := map[string]*remotemcp.ToolsCallUsageTrackingInterceptor{"": toolShared}
			resources := map[string]*remotemcp.ResourcesReadUsageTrackingInterceptor{"": resourceShared}
			for _, id := range []string{"server-one", "server-two"} {
				tools[id] = toolShared.WithMCPServerID(id).WithMetaMCPServerID("gateway")
				resources[id] = resourceShared.WithMCPServerID(id)
			}
			require.NoError(t, tools[serverID].InterceptToolsCallResponse(ctx, newToolsCallResponseForInterceptor(t, "")))
			require.NoError(t, resources[serverID].InterceptResourcesReadResponse(ctx, newResourcesReadResponseForInterceptor(t, "", "file:///example")))
			toolEvent, resourceEvent := toolTracker.waitForEvent(t), resourceTracker.waitForEvent(t)
			for _, event := range []billing.ToolCallUsageEvent{toolEvent, resourceEvent} {
				if serverID == "" {
					require.Nil(t, event.MCPServerID)
				} else {
					require.Equal(t, new(serverID), event.MCPServerID)
				}
				require.Nil(t, event.MCPEndpointID)
			}
			if serverID == "" {
				require.Nil(t, toolEvent.MetaMCPServerID)
			} else {
				require.Equal(t, new("gateway"), toolEvent.MetaMCPServerID)
			}
		})
	}
}
