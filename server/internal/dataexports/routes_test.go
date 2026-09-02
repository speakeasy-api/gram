package dataexports_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestRouteCRUDPreservesNullableDestinationAndAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")

	created, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: false, OtelDestinationID: nil})
	require.NoError(t, err)
	require.False(t, created.Enabled)
	require.Nil(t, created.OtelDestinationID)

	listed, err := ti.service.ListRoutes(ctx, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Equal(t, []*gen.DataExportRoute{created}, listed.Routes)

	updated, err := ti.service.UpdateRoute(ctx, &gen.UpdateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		ID: created.ID, DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Equal(t, destination.ID, *updated.OtelDestinationID)

	auditRecord, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDataExportRouteUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(auditRecord.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(auditRecord.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, false, beforeSnapshot["enabled"])
	require.NotContains(t, beforeSnapshot, "otel_destination_id")
	require.Equal(t, true, afterSnapshot["enabled"])
	require.Equal(t, destination.ID, afterSnapshot["otel_destination_id"])

	require.NoError(t, ti.service.DeleteRoute(ctx, &gen.DeleteRoutePayload{ID: created.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}))
	listed, err = ti.service.ListRoutes(ctx, &gen.ListRoutesPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Routes)

	for _, action := range []audit.Action{audit.ActionDataExportRouteCreate, audit.ActionDataExportRouteUpdate, audit.ActionDataExportRouteDelete} {
		count, err := audittest.AuditLogCountByAction(ctx, ti.conn, action)
		require.NoError(t, err)
		require.EqualValues(t, 1, count)
	}
}

func TestRouteRejectsEnabledWithoutUsableDestinationAndUnsupportedSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: nil})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "risk_findings", Enabled: false, OtelDestinationID: nil})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRouteRejectsSecondRouteForSameSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createDestination(t, ctx, ti, "https://first.example.test", "exclude")
	second := createDestination(t, ctx, ti, "https://second.example.test", "exclude")

	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &first.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &second.ID,
	})
	requireOopsCode(t, err, oops.CodeConflict)
	require.Equal(t, "a route already exists for this data source", err.Error())
}

func TestDestinationlessRouteReservesSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")

	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           false,
		OtelDestinationID: nil,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &destination.ID,
	})
	requireOopsCode(t, err, oops.CodeConflict)
}

func TestRouteSoftDeleteReleasesSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	firstDestination := createDestination(t, ctx, ti, "https://first.example.test", "exclude")
	secondDestination := createDestination(t, ctx, ti, "https://second.example.test", "exclude")

	firstRoute, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &firstDestination.ID,
	})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteRoute(ctx, &gen.DeleteRoutePayload{
		ID:               firstRoute.ID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	replacement, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &secondDestination.ID,
	})
	require.NoError(t, err)
	require.NotEqual(t, firstRoute.ID, replacement.ID)
}

func TestRouteSourceUniquenessIsProjectScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherSlug := "data-exports-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherAuthCtx := *authCtx
	otherAuthCtx.ProjectID = &otherProject.ID
	otherAuthCtx.ProjectSlug = &otherSlug
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuthCtx)

	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	otherDestination := createDestination(t, otherCtx, ti, "https://other-collector.example.test", "exclude")

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &destination.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRoute(otherCtx, &gen.CreateRoutePayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: &otherDestination.ID,
	})
	require.NoError(t, err)
}

func TestRouteRejectsCrossProjectDestinationReferences(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherSlug := "data-exports-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	otherAuthCtx := *authCtx
	otherAuthCtx.ProjectID = &otherProject.ID
	otherAuthCtx.ProjectSlug = &otherSlug
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuthCtx)
	otherDestination := createDestination(t, otherCtx, ti, "https://other-collector.example.test", "exclude")

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &otherDestination.ID})
	requireOopsCode(t, err, oops.CodeInvalid)

	listed, err := ti.service.ListDestinations(ctx, &gen.ListDestinationsPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	require.Empty(t, listed.Destinations)
}

func TestRouteRejectsInvalidStoredDestinationAsUnexpected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	queries := repo.New(ti.conn)
	rows, err := queries.ListOtelDestinations(ctx, repo.ListOtelDestinationsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	row := rows[0]
	_, err = queries.UpdateOtelDestination(ctx, repo.UpdateOtelDestinationParams{
		Name:             row.Name,
		EndpointUrl:      "not-a-url",
		HeadersEncrypted: row.HeadersEncrypted,
		SensitiveData:    row.SensitiveData,
		OrganizationID:   row.OrganizationID,
		ProjectID:        row.ProjectID,
		ID:               row.ID,
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeUnexpected)
}

func TestRouteCreateRollsBackWhenAuditInsertFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	destination := createDestination(t, ctx, ti, "https://collector.example.test", "exclude")
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionDataExportRouteCreate))

	_, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: "product_telemetry", Enabled: true, OtelDestinationID: &destination.ID})
	requireOopsCode(t, err, oops.CodeUnexpected)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	rows, err := repo.New(ti.conn).ListDataExportRoutes(ctx, repo.ListDataExportRoutesParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Empty(t, rows)
}
