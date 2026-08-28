//nolint:glint // Integration state transitions use isolated raw SQL to exercise deleted historical references.
package killswitchapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"gopkg.in/yaml.v3"

	srv "github.com/speakeasy-api/gram/server/gen/http/killswitches/server"
	gen "github.com/speakeasy-api/gram/server/gen/killswitches"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCustomerKillswitchLifecycleAndReadModels(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, servers := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	subjectUserID := "user_" + uuid.NewString()
	_, err := db.Exec(t.Context(), `INSERT INTO users (id, email, display_name) VALUES ($1, $1 || '@example.test', 'Affected User')`, subjectUserID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO organization_user_relationships (organization_id, user_id) VALUES ($1, $2)`, orgID, subjectUserID)
	require.NoError(t, err)
	endsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	payload := &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: subjectUserID,
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

	listed, err := service.List(ctx, &gen.ListPayload{UserID: &subjectUserID, Limit: new(int32(1))})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, CapabilityMCPToolCalls, string(listed.Items[0].CapabilityKey))
	require.Nil(t, listed.NextCursor)

	badges, err := service.BatchUserBadges(ctx, &gen.BatchUserBadgesPayload{UserIds: []string{subjectUserID, subjectUserID, "unknown-user"}})
	require.NoError(t, err)
	require.Len(t, badges.Badges, 2)
	require.True(t, badgeFor(t, badges, subjectUserID).AffectedNow)
	require.False(t, badgeFor(t, badges, "unknown-user").Affected)

	overlaps, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{
		CapabilityKey: CapabilityMCPToolCalls, UserID: subjectUserID,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: []string{servers[0].String()}},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
	})
	require.NoError(t, err)
	require.Len(t, overlaps.Overlaps, 1)
	require.False(t, overlaps.Truncated)

	_, err = db.Exec(t.Context(), `UPDATE organization_user_relationships SET deleted_at = clock_timestamp() WHERE organization_id = $1 AND user_id = $2`, orgID, subjectUserID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE mcp_servers SET deleted_at = clock_timestamp() WHERE id = ANY($1::uuid[])`, servers)
	require.NoError(t, err)
	_, err = service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	lifted, err := service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version})
	require.NoError(t, err)
	require.Equal(t, created.Version+1, lifted.Result.Version)
	require.Empty(t, lifted.RemainingOverlaps)
	require.False(t, lifted.Truncated)
}

func TestCustomerKillswitchOverlapResultsReportTruncation(t *testing.T) {
	t.Parallel()
	service, _, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	created := make([]*gen.KillswitchMutationReceipt, 102)
	for i := range created {
		result, err := service.Create(ctx, &gen.CreatePayload{
			OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
			Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
			ExternalNote: fmt.Sprintf("message %d", i), InternalNote: fmt.Sprintf("context %d", i),
		})
		require.NoError(t, err)
		created[i] = result
	}

	preview, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{
		CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
	})
	require.NoError(t, err)
	require.Len(t, preview.Overlaps, 100)
	require.True(t, preview.Truncated)

	lifted, err := service.Lift(ctx, &gen.LiftPayload{OperationID: uuid.NewString(), ID: created[0].ID, ExpectedVersion: created[0].Version})
	require.NoError(t, err)
	require.Len(t, lifted.RemainingOverlaps, 100)
	require.True(t, lifted.Truncated)
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
	active := gen.KillswitchStatus("active")
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

func TestCustomerKillswitchEditNowPreservesRequestedStartMode(t *testing.T) {
	t.Parallel()
	service, _, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	startsAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano)

	created, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &startsAt, End: "until_lifted"},
		ExternalNote: "message", InternalNote: "context",
	})
	require.NoError(t, err)

	edited, err := service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
		ExternalNote: "updated", InternalNote: "updated context",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), edited.Version)

	detail, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, gen.KillswitchStatus("active"), detail.Status)
	require.Equal(t, gen.KillswitchScheduleStart("now"), detail.Schedule.Start)
	require.Nil(t, detail.Schedule.StartsAt)
	require.NotEmpty(t, detail.History)
	require.Equal(t, gen.KillswitchScheduleStart("now"), detail.History[0].Schedule.Start)
	require.Nil(t, detail.History[0].Schedule.StartsAt)

	listed, err := service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1)
	require.Equal(t, gen.KillswitchStatus("active"), listed.Items[0].Status)
	require.Equal(t, gen.KillswitchScheduleStart("now"), listed.Items[0].Schedule.Start)
	require.Nil(t, listed.Items[0].Schedule.StartsAt)
}

func TestCustomerCreateAndEditRequireScheduledStartAfterDatabaseTime(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)

	var databaseNow time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&databaseNow))
	notFuture := databaseNow.UTC().Format(time.RFC3339Nano)
	schedule := &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &notFuture, End: "until_lifted"}
	_, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: schedule,
		ExternalNote: "message", InternalNote: "context",
	})
	requireOops(t, err, oops.CodeBadRequest)

	created, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
		ExternalNote: "message", InternalNote: "context",
	})
	require.NoError(t, err)

	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&databaseNow))
	notFuture = databaseNow.UTC().Format(time.RFC3339Nano)
	_, err = service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &notFuture, End: "until_lifted"},
		ExternalNote: "updated", InternalNote: "updated context",
	})
	requireOops(t, err, oops.CodeBadRequest)
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

func TestCustomerAuthorizationUsesLiveOrgAdminGrant(t *testing.T) {
	t.Parallel()

	service, db, orgID, userID, _, engine := newIntegrationServiceWithAdmin(t, true)
	ctx := customerContext(t, orgID, userID)
	prepared, err := engine.PrepareContext(ctx)
	require.NoError(t, err)
	check := authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: orgID}
	require.NoError(t, engine.Require(prepared, check))

	_, err = service.ListCapabilities(prepared, &gen.ListCapabilitiesPayload{})
	require.NoError(t, err)

	result, err := db.Exec(t.Context(), `DELETE FROM principal_grants WHERE organization_id = $1 AND principal_urn = $2`, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, userID))
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())
	// The request's prepared grants remain stale and permissive. The customer
	// service must reload grants and observe the revocation.
	require.NoError(t, engine.Require(prepared, check))
	_, err = service.ListCapabilities(prepared, &gen.ListCapabilitiesPayload{})
	requireOops(t, err, oops.CodeForbidden)

	deniedService, _, deniedOrgID, deniedUserID, _, _ := newIntegrationServiceWithAdmin(t, false)
	_, err = deniedService.ListCapabilities(customerContext(t, deniedOrgID, deniedUserID), &gen.ListCapabilitiesPayload{})
	requireOops(t, err, oops.CodeForbidden)
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

func TestCustomerListSnapshotSurvivesMutationBetweenPages(t *testing.T) {
	t.Parallel()
	service, _, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	receipts := make([]*gen.KillswitchMutationReceipt, 3)
	for i := range receipts {
		created, err := service.Create(ctx, &gen.CreatePayload{
			OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
			Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
			ExternalNote: fmt.Sprintf("message %d", i), InternalNote: fmt.Sprintf("context %d", i),
		})
		require.NoError(t, err)
		receipts[i] = created
	}

	page, err := service.List(ctx, &gen.ListPayload{Limit: ptr(int32(1))})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.NextCursor)

	mutated, err := service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: receipts[0].ID, ExpectedVersion: receipts[0].Version,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
		ExternalNote: "changed after snapshot", InternalNote: "changed after snapshot",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), mutated.Version)

	seen := map[string]int64{page.Items[0].ID: page.Items[0].Version}
	for page.NextCursor != nil {
		page, err = service.List(ctx, &gen.ListPayload{Limit: ptr(int32(1)), Cursor: page.NextCursor})
		require.NoError(t, err)
		for _, item := range page.Items {
			_, duplicate := seen[item.ID]
			require.False(t, duplicate, "snapshot item repeated across pages")
			seen[item.ID] = item.Version
		}
	}
	require.Len(t, seen, len(receipts))
	require.Equal(t, int64(1), seen[receipts[0].ID], "snapshot must expose the version current at as_of")
}

func TestCustomerHistoryUsesEventTimeStatus(t *testing.T) {
	t.Parallel()
	service, db, orgID, userID, _ := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	var transitionAt time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp() + interval '2 seconds'`).Scan(&transitionAt))
	transitionAt = transitionAt.UTC()
	startText := transitionAt.Format(time.RFC3339Nano)
	created, err := service.Create(ctx, &gen.CreatePayload{
		OperationID: uuid.NewString(), CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "scheduled", StartsAt: &startText, End: "until_lifted"},
		ExternalNote: "scheduled event", InternalNote: "scheduled event",
	})
	require.NoError(t, err)

	expiresText := transitionAt.Format(time.RFC3339Nano)
	edited, err := service.Edit(ctx, &gen.EditPayload{
		OperationID: uuid.NewString(), ID: created.ID, ExpectedVersion: created.Version,
		Scope: &gen.KillswitchScope{Type: "all_servers"}, Schedule: &gen.KillswitchSchedule{Start: "now", End: "bounded", EndsAt: &expiresText},
		ExternalNote: "bounded event", InternalNote: "bounded event",
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), edited.Version)
	require.Eventually(t, func() bool {
		var transitioned bool
		err := db.QueryRow(t.Context(), `SELECT clock_timestamp() > $1`, transitionAt).Scan(&transitioned)
		return err == nil && transitioned
	}, 5*time.Second, 20*time.Millisecond)

	detail, err := service.Get(ctx, &gen.GetPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, "expired", string(detail.Status))
	require.Len(t, detail.History, 2)
	require.Equal(t, "edited", string(detail.History[0].Action))
	require.Equal(t, "active", string(detail.History[0].Status), "bounded edit was active at event time")
	require.Equal(t, "created", string(detail.History[1].Action))
	require.Equal(t, "scheduled", string(detail.History[1].Status), "superseded create was scheduled at event time")
}

func TestPreviewSelectedServersCanonicalizesMaximumDuplicateBatch(t *testing.T) {
	t.Parallel()
	service, _, orgID, userID, servers := newIntegrationService(t)
	ctx := customerContext(t, orgID, userID)
	serverIDs := make([]string, 1000)
	for i := range serverIDs {
		serverIDs[i] = servers[i%len(servers)].String()
	}
	result, err := service.PreviewOverlaps(ctx, &gen.PreviewOverlapsPayload{
		CapabilityKey: CapabilityMCPToolCalls, UserID: userID,
		Scope:    &gen.KillswitchScope{Type: "selected_servers", ServerIds: serverIDs},
		Schedule: &gen.KillswitchSchedule{Start: "now", End: "until_lifted"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Overlaps)
}

func TestConflictTransportIsTypedAndCustomerSafe(t *testing.T) {
	t.Parallel()
	for _, conflict := range []*gen.KillswitchConflict{
		{Name: "operation_conflict", Message: "the operation ID was already used for a different request"},
		{Name: "version_conflict", Message: "the killswitch changed after the supplied version"},
	} {
		var body any
		if conflict.Name == "operation_conflict" {
			body = srv.NewEditOperationConflictResponseBody(conflict)
		} else {
			body = srv.NewEditVersionConflictResponseBody(conflict)
		}
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		text := strings.ToLower(string(encoded))
		require.Contains(t, text, conflict.Name)
		for _, internal := range []string{"prescription", "definition", "failure policy", "failure_policy"} {
			require.NotContains(t, text, internal)
		}
	}
}

func TestOpenAPIConflictContractIsMutationOnly(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../gen/http/openapi3.yaml")
	require.NoError(t, err)
	spec := string(data)
	for _, path := range []string{"listCapabilities", "listMCPServers", "list", "get", "previewOverlaps", "batchUserBadges"} {
		require.NotContains(t, openAPIOperation(spec, path), "\"409\":", path)
	}
	for _, path := range []string{"create", "edit", "lift"} {
		block := openAPIOperation(spec, path)
		require.Contains(t, block, "\"409\":", path)
		require.Contains(t, block, "#/components/schemas/KillswitchConflict", path)
	}
	componentStart := strings.Index(spec, "        KillswitchConflict:")
	require.NotEqual(t, -1, componentStart)
	component := spec[componentStart:]
	require.Contains(t, component, "operation_conflict")
	require.Contains(t, component, "version_conflict")
}

func TestPublishedKillswitchOperationContractIsStable(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../../../.speakeasy/out.openapi.yaml")
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID  string                `yaml:"operationId"`
			Security     []map[string][]string `yaml:"security"`
			NameOverride string                `yaml:"x-speakeasy-name-override"`
		} `yaml:"paths"`
		Components struct {
			Schemas map[string]struct {
				Required   []string `yaml:"required"`
				Properties map[string]struct {
					Type     string `yaml:"type"`
					MaxItems int    `yaml:"maxItems"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	require.NoError(t, yaml.Unmarshal(data, &spec))

	methods := map[string]string{
		"batchUserBadges":  "post",
		"create":           "post",
		"edit":             "post",
		"get":              "get",
		"lift":             "post",
		"list":             "get",
		"listCapabilities": "get",
		"listMCPServers":   "get",
		"previewOverlaps":  "post",
	}
	for name, method := range methods {
		operation := spec.Paths["/rpc/killswitches."+name][method]
		require.Equal(t, []map[string][]string{{"session_header_Gram-Session": []string{}}}, operation.Security, name)
		require.Equal(t, name, operation.NameOverride, name)
		require.Equal(t, "killswitches"+strings.ToUpper(name[:1])+name[1:], operation.OperationID, name)

		exportedName := "Killswitches" + strings.ToUpper(name[:1]) + name[1:]
		base := "../../../client/dashboard/src/sdk/src/"
		operationFile := base + "models/operations/" + strings.ToLower(exportedName) + ".ts"
		operationSource, readErr := os.ReadFile(operationFile)
		require.NoError(t, readErr)
		require.Contains(t, string(operationSource), "export type "+exportedName+"Request", operationFile)
		require.NotContains(t, string(operationSource), "Number", operationFile)

		functionName := "killswitches" + strings.ToUpper(name[:1]) + name[1:]
		functionFile := base + "funcs/" + functionName + ".ts"
		functionSource, readErr := os.ReadFile(functionFile)
		require.NoError(t, readErr)
		require.Contains(t, string(functionSource), "export function "+functionName, functionFile)
		require.NotContains(t, string(functionSource), "Number", functionFile)
	}

	for schema, arrayField := range map[string]string{
		"KillswitchLiftResult":            "remaining_overlaps",
		"KillswitchPreviewOverlapsResult": "overlaps",
	} {
		contract := spec.Components.Schemas[schema]
		require.Contains(t, contract.Required, "truncated", schema)
		require.Equal(t, "boolean", contract.Properties["truncated"].Type, schema)
		require.Equal(t, 100, contract.Properties[arrayField].MaxItems, schema)
	}
}

func TestGeneratedSDKContractUsesStableTaggedUnions(t *testing.T) {
	t.Parallel()
	sdkSpec, err := os.ReadFile("../../../.speakeasy/out.openapi.yaml")
	require.NoError(t, err)
	spec := string(sdkSpec)
	scopeStart := strings.LastIndex(spec, "    KillswitchScope:")
	require.NotEqual(t, -1, scopeStart)
	scopeContract := spec[scopeStart:]
	require.Contains(t, scopeContract, "oneOf:")
	require.Contains(t, scopeContract, "required: [type, server_ids]")
	require.Contains(t, scopeContract, "minItems: 1")
	require.Contains(t, scopeContract, "additionalProperties: false")

	for path, assertions := range map[string][]string{
		"../../../client/dashboard/src/sdk/src/models/components/killswitchscope.ts":           {"export type KillswitchScope =", "type: \"all_servers\"", "type: \"selected_servers\"", "z.union(["},
		"../../../client/dashboard/src/sdk/src/models/components/killswitchschedule.ts":        {"export type KillswitchSchedule =", "KillswitchNowBoundedSchedule", "KillswitchScheduledUntilLiftedSchedule", "z.union(["},
		"../../../client/dashboard/src/sdk/src/models/components/killswitchmutationreceipt.ts": {"export type KillswitchMutationReceipt", "replayed: boolean"},
	} {
		generated, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		text := string(generated)
		for _, assertion := range assertions {
			require.Contains(t, text, assertion, path)
		}
		if strings.HasSuffix(path, "killswitchmutationreceipt.ts") {
			require.NotContains(t, text, "status:")
		}
	}
}

func openAPIOperation(spec, method string) string {
	marker := "    /rpc/killswitches." + method + ":"
	_, after, ok := strings.Cut(spec, marker)
	if !ok {
		return ""
	}
	rest := after
	before, _, ok := strings.Cut(rest, "\n    /rpc/")
	if !ok {
		return rest
	}
	return before
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

func badgeFor(t *testing.T, result *gen.KillswitchBatchUserBadgesResult, userID string) *gen.KillswitchUserBadge {
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
	var named goa.GoaErrorNamer
	require.ErrorAs(t, err, &named)
	require.Equal(t, name, named.GoaErrorName())
	var conflict *gen.KillswitchConflict
	if errors.As(err, &conflict) {
		message := strings.ToLower(conflict.Message)
		for _, internal := range []string{"prescription", "definition", "failure policy", "failure_policy"} {
			require.NotContains(t, message, internal)
		}
	}
}

func requireOops(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, code, shareable.Code)
}
