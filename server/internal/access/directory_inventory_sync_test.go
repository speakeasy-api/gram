package access

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	thirdpartyworkos "github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// The inventory contains a group never seen locally. A row-level tombstone guard
// cannot protect its fresh INSERT after the directory deletion commits.
func TestService_SyncDirectoryGroups_RejectsInventoryAcrossDirectoryDeletion(t *testing.T) {
	t.Parallel()
	for _, replay := range []bool{false, true} {
		t.Run(fmt.Sprintf("replay=%t", replay), func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			at := time.Now().UTC()
			data, err := json.Marshal(map[string]any{"id": "directory_deleted", "organization_id": mockidp.MockOrgID, "updated_at": at})
			require.NoError(t, err)
			stub := thirdpartyworkos.NewStubClient()
			activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), ti.conn, stub, cache.NoopCache, nil)
			if replay {
				_, err := workosrepo.New(ti.conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{WorkosOrganizationID: mockidp.MockOrgID, LastEventID: "event_deleted"})
				require.NoError(t, err)
			}
			ti.roles.On("ListDirectories", mock.Anything, mockidp.MockOrgID).Return([]thirdpartyworkos.Directory{
				{ID: "directory_deleted", OrganizationID: mockidp.MockOrgID, State: "linked"},
			}, nil).Once()
			ti.roles.On("ListDirectoryGroups", mock.Anything, "directory_deleted").Run(func(mock.Arguments) {
				stub.SetEventPages([][]events.Event{{{ID: "event_deleted", Event: "dsync.deleted", CreatedAt: at, Data: data}}})
				_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: mockidp.MockOrgID})
				require.NoError(t, err)
			}).Return([]thirdpartyworkos.DirectoryGroup{
				{ID: "group_previously_unseen", DirectoryID: "directory_deleted", OrganizationID: mockidp.MockOrgID, Name: "Unseen", CreatedAt: at.Format(time.RFC3339), UpdatedAt: at.Format(time.RFC3339)},
			}, nil).Once()

			result, err := ti.service.SyncDirectoryGroups(ctx, &gen.SyncDirectoryGroupsPayload{})
			require.Error(t, err)
			require.Nil(t, result)
			_, err = directoryrepo.New(ti.conn).GetDirectoryGroupByWorkOSID(ctx, "group_previously_unseen")
			require.ErrorIs(t, err, pgx.ErrNoRows, "stale inventory must not insert a previously unseen live group")
		})
	}
}
