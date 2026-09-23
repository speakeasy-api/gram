package slackdirectoryconnections_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/slack_directory_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections"
	"github.com/stretchr/testify/require"
)

func memberRequest() *gen.ListMembersPayload {
	return &gen.ListMembersPayload{SessionToken: nil, ConnectionID: nil, Search: nil, Cursor: nil, Limit: 50}
}

func TestDirectoryEndpointsRequireAdminSessionAndProductFeature(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	request := &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation}
	missingSession := *f.auth
	missingSession.SessionID = nil
	support := *f.auth
	support.IsAdmin = true
	support.SupportOrganizationID = support.ActiveOrganizationID
	denied := []context.Context{authztest.WithExactGrants(t, ctx), contextvalues.SetAuthContext(ctx, &missingSession), contextvalues.WithLegacyAPIKeyAuthorization(ctx, f.auth), contextvalues.WithValidatedSupportSession(ctx, &support)}
	for _, deniedCtx := range denied {
		_, err := f.service.Sync(deniedCtx, request)
		require.Error(t, err)
		_, err = f.service.ListMembers(deniedCtx, memberRequest())
		require.Error(t, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err := f.service.Sync(cancelled, request)
	require.Error(t, err)
	_, err = f.service.ListMembers(cancelled, memberRequest())
	require.Error(t, err)
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, f.auth.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, false))
	_, err = f.service.Sync(ctx, request)
	require.Error(t, err)
	_, err = f.service.ListMembers(ctx, memberRequest())
	require.Error(t, err)
}

func TestDirectoryTenantIsolationAndGeneration(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil))
	// A different active organization cannot resolve this connection, even with grants.
	other := *f.auth
	other.ActiveOrganizationID = "org_synthetic_other"
	otherCtx := contextvalues.SetAuthContext(ctx, &other)
	otherCtx = authztest.WithExactGrants(t, otherCtx, authz.NewGrant(authz.ScopeOrgAdmin, other.ActiveOrganizationID))
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, other.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, true))
	empty, err := f.service.ListMembers(otherCtx, memberRequest())
	require.NoError(t, err)
	require.Empty(t, empty.Members)
	request := memberRequest()
	request.ConnectionID = &c.ID
	_, err = f.service.ListMembers(otherCtx, request)
	require.Error(t, err)
	_, err = f.service.Sync(otherCtx, &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.Error(t, err)
	_, err = f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: uuid.NewString()})
	require.Error(t, err)
	_, err = f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: uuid.NewString(), Generation: c.Generation})
	require.Error(t, err)
}

func TestDirectorySearchWorkspaceAndPagination(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	first := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	second := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE02")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01", "UEXAMPLE02")).Run(ctx, syncRequest(f, first), nil))
	require.NoError(t, syncer(f, snapshot("UEXAMPLE03")).Run(ctx, syncRequest(f, second), nil))
	p := memberRequest()
	p.Limit = 1
	page, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(3), page.Total)
	require.Len(t, page.Members, 1)
	require.NotNil(t, page.NextCursor)
	p.Cursor = page.NextCursor
	next, err := f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.NotEqual(t, page.Members[0].ID, next.Members[0].ID)
	p = memberRequest()
	p.ConnectionID = &second.ID
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, "UEXAMPLE03", page.Members[0].SlackUserID)
	search := "%"
	p.Search = &search
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Empty(t, page.Members)
	search = "example03"
	page, err = f.service.ListMembers(ctx, p)
	require.NoError(t, err)
	require.Len(t, page.Members, 1)
}

func TestSyncInitialManualAndSchedulerFailure(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.Len(t, f.scheduler.inputs, 1)
	require.Equal(t, c.Generation, f.scheduler.inputs[0].Generation.String())
	result, err := f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.Len(t, f.scheduler.inputs, 2)
	f.scheduler.err = errors.New("synthetic unavailable")
	list, err := f.service.List(ctx, &gen.ListPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Equal(t, "unknown", list.Connections[0].SyncStatus)
	// Reconnect still completes when initial sync cannot be scheduled.
	authorize(t, ctx, f, begin(t, ctx, f, &c.ID), c.WorkspaceID)
}

func TestDirectorySearchUsesCharactersAndDistinctFields(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	name := strings.Repeat("界", 200)
	provider := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		return []slackdirectoryconnections.DirectoryMember{{UserID: "UEXAMPLE01", DisplayName: name, Email: "unique@example.com", Status: "active", MemberType: "person", UpdatedAt: nil}}, nil
	})
	require.NoError(t, syncer(f, provider).Run(ctx, syncRequest(f, c), nil))
	for _, search := range []string{name, "unique@example.com", "UEXAMPLE01"} {
		p := memberRequest()
		p.Search = &search
		result, err := f.service.ListMembers(ctx, p)
		require.NoError(t, err)
		require.Len(t, result.Members, 1)
	}
	p := memberRequest()
	tooLong := name + "界"
	p.Search = &tooLong
	_, err := f.service.ListMembers(ctx, p)
	require.Error(t, err)
}

func TestDirectoryDisconnectRetainsCountAndAudit(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	c := authorize(t, ctx, f, begin(t, ctx, f, nil), "TEXAMPLE01")
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01", "UEXAMPLE02")).Run(ctx, syncRequest(f, c), nil))
	require.NoError(t, syncer(f, snapshot("UEXAMPLE01")).Run(ctx, syncRequest(f, c), nil))
	require.Len(t, members(t, ctx, f), 2, "absent profiles remain available in history")
	disconnected, err := f.service.Disconnect(ctx, &gen.DisconnectPayload{SessionToken: nil, ID: c.ID, Generation: c.Generation})
	require.NoError(t, err)
	require.Equal(t, int64(1), disconnected.MemberCount)
	require.Equal(t, "stale", disconnected.DirectoryStatus)
	entry, err := audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionDisconnect)
	require.NoError(t, err)
	var before, after struct {
		MemberCount int64 `json:"MemberCount"`
	}
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &after))
	require.Equal(t, int64(1), before.MemberCount)
	require.Equal(t, int64(1), after.MemberCount)
	reconnected := authorize(t, ctx, f, begin(t, ctx, f, &c.ID), "TEXAMPLE01")
	require.Equal(t, int64(1), reconnected.MemberCount)
	entry, err = audittest.LatestAuditLogByAction(ctx, f.db, audit.ActionSlackDirectoryConnectionAuthorize)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &after))
	require.Equal(t, int64(1), before.MemberCount)
	require.Equal(t, int64(1), after.MemberCount)
}

func TestSharedDemoDirectoryRemainsReadableWithoutSync(t *testing.T) {
	t.Parallel()
	ctx, f := newService(t)
	visitor := *f.auth
	visitor.ActiveOrganizationID = constants.DemoOrganizationID
	ctx = authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &visitor), authz.DemoScopeGrants()...)
	require.NoError(t, f.productFeatures.SetFeatureEnabled(ctx, visitor.ActiveOrganizationID, productfeatures.FeatureClaudeTagSupport, true))
	_, err := f.service.ListMembers(ctx, memberRequest())
	require.NoError(t, err)
	_, err = f.service.Sync(ctx, &gen.SyncPayload{SessionToken: nil, ID: uuid.NewString(), Generation: uuid.NewString()})
	var failure *oops.ShareableError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, oops.CodeForbidden, failure.Code)
	require.Empty(t, f.scheduler.inputs)
	input := slackdirectoryconnections.SyncInput{OrganizationID: constants.DemoOrganizationID, ConnectionID: uuid.New(), Generation: uuid.New(), ActorID: visitor.UserID, StartedAt: time.Now()}
	provider := directoryFunc(func(context.Context, string, string, func(slackdirectoryconnections.SyncProgress)) ([]slackdirectoryconnections.DirectoryMember, error) {
		t.Error("shared demo fetched Slack profiles")
		return nil, nil
	})
	err = syncer(f, provider).Run(ctx, input, nil)
	var syncErr *slackdirectoryconnections.SyncError
	require.ErrorAs(t, err, &syncErr)
	require.Equal(t, "demo_read_only", syncErr.Code)
	require.False(t, syncErr.Retryable)
}
