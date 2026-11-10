package auditapi_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/auditlogs"
)

func seedOriginLogs(t *testing.T, ti *testInstance, orgID string, projectID uuid.NullUUID) (system, dashboard, unattributed uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	system = insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: orgID,
		projectID:      projectID,
		actorID:        "system:plugin-publisher",
		actorType:      "system",
		actingSurface:  new("system"),
		action:         "plugin:publish",
		subjectID:      uuid.NewString(),
		subjectType:    "plugin",
	})
	dashboard = insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: orgID,
		projectID:      projectID,
		actorID:        "user:alice",
		actorType:      "user",
		actingSurface:  new("dashboard"),
		action:         "project:update",
		subjectID:      uuid.NewString(),
		subjectType:    "project",
	})
	unattributed = insertAuditLog(t, ctx, ti, auditLogSeed{
		organizationID: orgID,
		projectID:      projectID,
		actorID:        "user:bob",
		actorType:      "user",
		action:         "toolset:update",
		subjectID:      uuid.NewString(),
		subjectType:    "toolset",
	})
	return system, dashboard, unattributed
}

func logIDs(logs []*gen.AuditLog) []string {
	ids := make([]string, 0, len(logs))
	for _, log := range logs {
		ids = append(ids, log.ID)
	}
	return ids
}

func TestAuditService_List_FiltersByOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	system, dashboard, unattributed := seedOriginLogs(t, ti, authCtx.ActiveOrganizationID, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true})

	all, err := ti.service.List(ctx, &gen.ListPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{system.String(), dashboard.String(), unattributed.String()}, logIDs(all.Logs))

	systemOnly, err := ti.service.List(ctx, &gen.ListPayload{Origin: new("system")})
	require.NoError(t, err)
	require.Equal(t, []string{system.String()}, logIDs(systemOnly.Logs))

	userOnly, err := ti.service.List(ctx, &gen.ListPayload{Origin: new("user")})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{dashboard.String(), unattributed.String()}, logIDs(userOnly.Logs), "rows without a recorded surface count as user changes")
}

func TestAuditService_ListFacets_FiltersByOrigin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAuditService(t)
	authCtx := testAuthContext(t, ctx)
	seedOriginLogs(t, ti, authCtx.ActiveOrganizationID, uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true})

	systemFacets, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{Origin: new("system")})
	require.NoError(t, err)
	require.Equal(t, []string{"plugin:publish"}, facetValues(systemFacets.Actions))
	require.Equal(t, []string{"system:plugin-publisher"}, facetValues(systemFacets.Actors))
	require.Equal(t, []string{"system"}, facetValues(systemFacets.Surfaces))

	userFacets, err := ti.service.ListFacets(ctx, &gen.ListFacetsPayload{Origin: new("user")})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"project:update", "toolset:update"}, facetValues(userFacets.Actions))
	require.ElementsMatch(t, []string{"user:alice", "user:bob"}, facetValues(userFacets.Actors))
	require.ElementsMatch(t, []string{"dashboard", "unknown"}, facetValues(userFacets.Surfaces))
}

func facetValues(options []*gen.AuditLogFacetOption) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.Value)
	}
	return values
}
