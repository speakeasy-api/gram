package activities_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type recordingIdentityMapSignaler struct {
	mu    sync.Mutex
	count int
}

func (r *recordingIdentityMapSignaler) SignalIdentityMapRefresh(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	return nil
}

func (r *recordingIdentityMapSignaler) refreshCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}

// A processed membership change must request an identity map refresh after
// commit: memberships decide which directory emails the map resolves. Both
// branches signal — the upsert on membership.created and the deprovision on
// membership.deleted.
func TestProcessWorkOSOrganizationEvents_MembershipSignalsIdentityMapRefresh(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	conn, err := infra.CloneTestDatabase(t, "workos_membership_identity_refresh")
	require.NoError(t, err)

	const (
		organizationID = "org-idmap-refresh"
		workosOrgID    = "org_01IDMAPREFRESH"
		userID         = "user_idmap_refresh"
		workosUserID   = "user_01IDMAPREFRESH"
		membershipID   = "mem_01IDMAPREFRESH"
	)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		newWorkOSMembershipEvent(t, "organization_membership.created", "event_0001", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), "member"),
		newWorkOSMembershipEvent(t, "organization_membership.deleted", "event_0002", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC)),
	}})

	signals := &recordingIdentityMapSignaler{mu: sync.Mutex{}, count: 0}
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, signals)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, 2, signals.refreshCount())
}
