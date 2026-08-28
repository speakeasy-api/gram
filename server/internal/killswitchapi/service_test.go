//nolint:glint // Integration state transitions use isolated raw SQL to exercise deleted historical references.
package killswitchapi

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"

	gen "github.com/speakeasy-api/gram/server/gen/killswitches"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCustomerKillswitchLifecycleAndReadModels(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, servers := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	endsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	payload := &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:        &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[1].String(), servers[0].String(), servers[1].String()}},
		Schedule:     &gen.KillswitchSchedule{Start: "now", End: "bounded", EndsAt: &endsAt},
		ExternalNote: "  Customer message  ", InternalNote: "\n operator context \t",
	}

	created, err := service.Create(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)
	require.False(t, created.Replayed)

	replayPayload := *payload
	replayPayload.Scope = &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String(), servers[1].String()}}
	replayed, err := service.Create(ctx, &replayPayload)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, created.ID, replayed.ID)

	conflicting := replayPayload
	conflicting.ExternalNote = "different"
	_, err = service.Create(ctx, &conflicting)
	requireServiceError(t, err, "operation_conflict")

	detail, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, "Customer message", detail.ExternalNote)
	require.Equal(t, "operator context", detail.InternalNote)
	expectedServers := []string{servers[0].String(), servers[1].String()}
	slices.Sort(expectedServers)
	require.Equal(t, expectedServers, detail.Scope.ServerIds)
	require.Len(t, detail.History, 1)
	require.Equal(t, "Customer message", detail.History[0].ExternalNote)
	require.Equal(t, "operator context", detail.History[0].InternalNote)

	listed, err := service.List(ctx, &gen.ListPayload{UserID: &userID, Limit: new(int32(1))})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, CapabilityMCPToolCalls, listed.Items[0].CapabilityKey)
	require.Nil(t, listed.NextCursor)

	badges, err := service.BatchUserBadges(ctx, &gen.BatchUserBadgesPayload{UserIds: []string{userID, userID, "unknown-user"}})
	require.NoError(t, err)
	require.Len(t, badges.Badges, 2)
	require.True(t, badgeFor(t, badges, userID).AffectedNow)
	require.False(t, badgeFor(t, badges, "unknown-user").Affected)

	overlaps, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{
		CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
	})
	require.NoError(t, err)
	require.Len(t, overlaps.Overlaps, 1)

	_, err = db.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, userID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = ANY($1::uuid[])`, servers)
	require.NoError(t, err)
	_, err = service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	lifted, err := service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	require.NoError(t, err)
	require.Equal(t, "lifted", lifted.Result.Status)
	require.Empty(t, lifted.RemainingOverlaps)
}

func TestCustomerKillswitchScheduleOverlapPaginationAndStaleEdit(t *testing.T) {
	t.Parallel()
	service, _, orgID, userID, servers := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	boundary := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Millisecond)
	boundaryText := boundary.Format(time.RFC3339Nano)

	first, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "bounded", EndsAt: &boundaryText}, ExternalNote: "message", InternalNote: "context",
	})
	require.NoError(t, err)

	adjacentStart := boundaryText
	preview, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{
		CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &adjacentStart, End: "until_lifted"},
	})
	require.NoError(t, err)
	require.Empty(t, preview.Overlaps, "adjacent half-open intervals must not overlap")

	second, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:    &gen.KillswitchScope{Type: "all_servers"},
		Schedule: &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &adjacentStart, End: "until_lifted"}, ExternalNote: "message two", InternalNote: "context two",
	})
	require.NoError(t, err)

	page1, err := service.List(ctx, &gen.ListPayload{Limit: new(int32(1))})
	require.NoError(t, err)
	require.Len(t, page1.Items, 1)
	require.NotNil(t, page1.NextCursor)
	page2, err := service.List(ctx, &gen.ListPayload{Limit: new(int32(1)), Cursor: page1.NextCursor})
	require.NoError(t, err)
	require.Len(t, page2.Items, 1)
	require.NotEqual(t, page1.Items[0].ID, page2.Items[0].ID)
	require.ElementsMatch(t, []string{first.ID, second.ID}, []string{page1.Items[0].ID, page2.Items[0].ID})

	badCursor := *page1.NextCursor
	active := "active"
	_, err = service.List(ctx, &gen.ListPayload{Limit: new(int32(1)), Cursor: &badCursor, Status: &active})
	requireOops(t, err, oops.CodeBadRequest)

	edit, err := service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: first.ID, ExpectedVersion: first.Version,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}, ExternalNote: "updated", InternalNote: "updated context",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), edit.Version)
	_, err = service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: first.ID, ExpectedVersion: first.Version,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}, ExternalNote: "stale", InternalNote: "stale context",
	})
	requireServiceError(t, err, "version_conflict")
}

func TestDesiredRequiresExplicitTaggedScopeAndSchedule(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tests := []struct {
		name     string
		scope    *gen.KillswitchScope
		schedule *gen.KillswitchSchedule
	}{
		{name: "missing scope", schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}},
		{name: "all with ids", scope: &gen.KillswitchScope{Type: "all_servers", ServerIds: []string{uuid.NewString()}}, schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}},
		{name: "empty selected", scope: &gen.KillswitchScope{Type: "selected_servers"}, schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}},
		{name: "missing schedule", scope: &gen.KillswitchScope{Type: "all_servers"}},
		{name: "now with timestamp", scope: &gen.KillswitchScope{Type: "all_servers"}, schedule: &gen.KillswitchSchedule{Start: "now", StartsAt: &now, End: "until_lifted"}},
		{name: "scheduled without timestamp", scope: &gen.KillswitchScope{Type: "all_servers"}, schedule: &gen.KillswitchSchedule{Start: "scheduled", End: "until_lifted"}},
		{name: "bounded without timestamp", scope: &gen.KillswitchScope{Type: "all_servers"}, schedule: &gen.KillswitchSchedule{Start: "now", End: "bounded"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := desired(test.scope, test.schedule, "context", "message")
			requireOops(t, err, oops.CodeBadRequest)
		})
	}
}

func TestCustomerAuthorizationAndOpaqueForeignReferences(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	capabilities, err := service.ListCapabilities(ctx, &gen.ListCapabilitiesPayload{})
	require.NoError(t, err)
	require.Equal(t, []*gen.KillswitchCapability{{Key: CapabilityMCPToolCalls, Label: "MCP tool calls"}}, capabilities.Capabilities)
	require.Equal(t, []*gen.KillswitchComingSoonCapability{{Label: "AI access"}}, capabilities.ComingSoon)

	tooManyUsers := make([]string, 101)
	for i := range tooManyUsers {
		tooManyUsers[i] = fmt.Sprintf("user-%03d", i)
	}
	_, err = service.BatchUserBadges(ctx, &gen.BatchUserBadgesPayload{UserIds: tooManyUsers})
	requireOops(t, err, oops.CodeBadRequest)

	_, err = service.ListCapabilities(supportContext(t, orgID), &gen.ListCapabilitiesPayload{})
	requireOops(t, err, oops.CodeForbidden)
	_, err = service.ListCapabilities(customerContext(t, constants.DemoOrganizationID, userID), &gen.ListCapabilitiesPayload{})
	requireOops(t, err, oops.CodeForbidden)
	_, err = service.ListCapabilities(contextvalues.SetRBACScopeOverride(ctx, string(authz.ScopeOrgAdmin)), &gen.ListCapabilitiesPayload{})
	requireOops(t, err, oops.CodeForbidden)

	create := func(serverID string) error {
		_, createErr := service.Create(ctx, &gen.CreatePayload{
			OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
			Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{serverID}},
			Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"}, ExternalNote: "message", InternalNote: "context",
		})
		return createErr
	}
	requireOops(t, create(uuid.NewString()), oops.CodeBadRequest)
	requireOops(t, create(insertForeignServer(t, db).String()), oops.CodeBadRequest)
	requireOops(t, create("not-a-server"), oops.CodeBadRequest)
}

func customerContext(t *testing.T, organizationID, userID string) context.Context {
	t.Helper()
	sessionID, email := "session", userID+"@example.test"
	return contextvalues.WithValidatedGramSession(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: organizationID, UserID: userID, SessionID: &sessionID, Email: &email}, false)
}

func supportContext(t *testing.T, organizationID string) context.Context {
	t.Helper()
	ctx := customerContext(t, organizationID, "support-user")
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	authCtx.IsAdmin = true
	authCtx.SupportOrganizationID = organizationID
	return contextvalues.WithValidatedSupportSession(ctx, authCtx)
}

func badgeFor(t *testing.T, result *gen.BatchUserBadgesResult, userID string) *gen.KillswitchUserBadge {
	t.Helper()
	for _, badge := range result.Badges {
		if badge.UserID == userID {
			return badge
		}
	}
	t.Fatalf("badge for %q not found", userID)
	return nil
}

//go:fix inline
func ptr[T any](value T) *T { return new(value) }

func requireServiceError(t *testing.T, err error, name string) {
	t.Helper()
	var serviceErr *goa.ServiceError
	require.ErrorAs(t, err, &serviceErr)
	require.Equal(t, name, serviceErr.Name)
}

func requireOops(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
