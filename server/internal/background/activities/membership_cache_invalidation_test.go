package activities_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProcessWorkOSOrganizationEvents_MembershipCreatedInvalidatesUserInfoCache(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_cache")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_cache"
	const workosOrgID = "org_01HZMEMCACHE"
	const userID = "user_mem_cache"
	const workosUserID = "user_01HZMEMCACHE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEMCACHE", "mem_01HZCACHE", workosOrgID, workosUserID, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), "member"),
		},
	})
	capturingCache := newCaptureCache()
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub, capturingCache, nil)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	// A cached organization list that predates the membership would keep
	// denying the new member until its TTL ran out.
	deletedKeys := capturingCache.Deleted()
	require.Len(t, deletedKeys, 1)
	require.Contains(t, deletedKeys[0], sessions.UserInfoCacheKey(userID))
}
