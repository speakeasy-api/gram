package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type dataExportToggleFixture struct {
	conn        *pgxpool.Pool
	service     *DataExportRouteToggleService
	principal   Principal
	project     ResolvedProject
	destination uuid.UUID
}

func newDataExportToggleFixture(t *testing.T, ctx context.Context, database string) dataExportToggleFixture {
	t.Helper()
	conn, err := platformMCPInfra.CloneTestDatabase(t, database)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), conn, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), stubAuthorizer{err: nil}, allowingBudget())
	require.NoError(t, err)
	destination, err := dataexportsrepo.New(conn).CreateOtelDestination(ctx, dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		Name:             "Collector",
		EndpointUrl:      "https://otel.example.test/v1",
		HeadersEncrypted: pgtype.Text{},
		SensitiveData:    pgtype.Text{String: "exclude", Valid: true},
	})
	require.NoError(t, err)
	return dataExportToggleFixture{conn: conn, service: service, principal: principal, project: project, destination: destination.ID}
}

func (f dataExportToggleFixture) createRoute(t *testing.T, ctx context.Context, dataSource string, enabled bool, destination uuid.NullUUID) dataexportsrepo.DataExportRoute {
	t.Helper()
	route, err := dataexportsrepo.New(f.conn).CreateDataExportRoute(ctx, dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    f.principal.OrganizationID,
		ProjectID:         f.project.ID,
		DataSource:        dataSource,
		Enabled:           enabled,
		OtelDestinationID: destination,
	})
	require.NoError(t, err)
	return route
}

func (f dataExportToggleFixture) route(t *testing.T, ctx context.Context, routeID uuid.UUID) dataexportsrepo.DataExportRoute {
	t.Helper()
	rows, err := dataexportsrepo.New(f.conn).ListDataExportRoutes(ctx, dataexportsrepo.ListDataExportRoutesParams{OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID})
	require.NoError(t, err)
	for _, row := range rows {
		if row.ID == routeID {
			return row
		}
	}
	require.FailNow(t, "route not found")
	return dataexportsrepo.DataExportRoute{}
}

func (f dataExportToggleFixture) input(routeID uuid.UUID, key string) ToggleDataExportRouteInput {
	return ToggleDataExportRouteInput{ProjectID: f.project.ID.String(), RouteID: routeID.String(), IdempotencyKey: key, Confirmed: true}
}

func (f dataExportToggleFixture) receiptStored(t *testing.T, ctx context.Context, operation, key string) bool {
	t.Helper()
	_, err := platformrepo.New(f.conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: f.principal.OrganizationID, UserID: conv.ToPGText(f.principal.UserID), SubjectUrn: userSubjectURN(f.principal.UserID),
		ProjectID: f.project.ID, Operation: operation, IdempotencyKey: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	require.NoError(t, err)
	return true
}

func auditCount(t *testing.T, ctx context.Context, conn *pgxpool.Pool, action audit.Action) int64 {
	t.Helper()
	count, err := audittest.AuditLogCountByAction(ctx, conn, action)
	require.NoError(t, err)
	return count
}

// configurationOf blanks the two columns pause and resume are allowed to
// change; everything left must be byte-identical across a pause and a resume.
func configurationOf(row dataexportsrepo.DataExportRoute) dataexportsrepo.DataExportRoute {
	row.Enabled = false
	row.UpdatedAt = row.CreatedAt
	return row
}

func requireDataExportToggleCode(t *testing.T, err error, code string) string {
	t.Helper()
	require.Error(t, err)
	result, ok := dataExportToggleToolResult(err)
	require.True(t, ok, "error must map to a readable refusal: %v", err)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var refusal struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, code, refusal.Code, text.Text)
	return text.Text
}

func TestPauseDataExportChangesOnlyEnabledAndResumeRestoresIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle")
	created := f.createRoute(t, ctx, "tool_call_logs", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	original := f.route(t, ctx, created.ID)

	paused, err := f.service.Pause(ctx, f.principal, f.input(created.ID, "pause-1"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomePaused, paused.Outcome)
	require.Equal(t, "fresh_read_after_commit", paused.SnapshotScope)
	require.NotNil(t, paused.Route)
	require.False(t, paused.Route.Enabled)
	require.Equal(t, f.destination.String(), paused.Route.DestinationID)
	require.Equal(t, "tool_call_logs", paused.Route.DataSource)
	require.Equal(t, dataExportLastDeliveryNotRecorded, paused.LastDelivery)
	require.Contains(t, paused.WhilePaused, "Dropped, not buffered")
	require.False(t, paused.Receipt.Replayed)
	afterPause := f.route(t, ctx, created.ID)
	require.False(t, afterPause.Enabled)
	require.Equal(t, configurationOf(original), configurationOf(afterPause), "pausing must not touch the data source, destination, or anything else")
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRoutePause))

	resumed, err := f.service.Resume(ctx, f.principal, f.input(created.ID, "resume-1"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomeResumed, resumed.Outcome)
	require.True(t, resumed.Route.Enabled)
	afterResume := f.route(t, ctx, created.ID)
	require.True(t, afterResume.Enabled)
	require.Equal(t, configurationOf(original), configurationOf(afterResume), "resuming must restore exactly the prior configuration")
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteResume))
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteUpdate), "pause and resume record their own actions")
}

func TestPauseDataExportReportsUnchangedForAPausedRoute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_unchanged")
	created := f.createRoute(t, ctx, "product_telemetry", false, uuid.NullUUID{UUID: f.destination, Valid: true})
	before := f.route(t, ctx, created.ID)

	output, err := f.service.Pause(ctx, f.principal, f.input(created.ID, "pause-paused"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomeUnchanged, output.Outcome)
	require.False(t, output.Route.Enabled)
	require.False(t, output.Receipt.Replayed)
	require.Equal(t, before, f.route(t, ctx, created.ID), "a no-op must not touch updated_at")
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRoutePause))
	require.True(t, f.receiptStored(t, ctx, operationPauseDataExport, "pause-paused"), "a no-op is still keyed")
}

func TestPauseDataExportReplayWritesNothingNew(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_replay")
	created := f.createRoute(t, ctx, "risk_findings", true, uuid.NullUUID{UUID: f.destination, Valid: true})

	first, err := f.service.Pause(ctx, f.principal, f.input(created.ID, "pause-replay"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomePaused, first.Outcome)
	_, err = f.service.Resume(ctx, f.principal, f.input(created.ID, "resume-between"))
	require.NoError(t, err)
	resumedRow := f.route(t, ctx, created.ID)

	// The same key replays the stored result and does not pause again, even
	// though the route has since been resumed.
	replayed, err := f.service.Pause(ctx, f.principal, f.input(created.ID, "pause-replay"))
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, dataExportOutcomePaused, replayed.Outcome)
	require.True(t, replayed.Route.Enabled, "the verification read reports the committed state, not the replayed one")
	require.Equal(t, resumedRow, f.route(t, ctx, created.ID))
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRoutePause))

	other := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	_, err = f.service.Pause(ctx, f.principal, f.input(other.ID, "pause-replay"))
	requireDataExportToggleCode(t, err, "conflict")
	require.True(t, f.route(t, ctx, other.ID).Enabled)
}

func TestResumeDataExportRefusesWithoutAUsableDestination(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_no_destination")

	destinationless := f.createRoute(t, ctx, "product_telemetry", false, uuid.NullUUID{})
	before := f.route(t, ctx, destinationless.ID)
	_, err := f.service.Resume(ctx, f.principal, f.input(destinationless.ID, "resume-none"))
	message := requireDataExportToggleCode(t, err, "no_destination")
	require.Contains(t, message, "has no destination")
	require.Equal(t, before, f.route(t, ctx, destinationless.ID))
	require.False(t, f.receiptStored(t, ctx, operationResumeDataExport, "resume-none"), "a refusal stores no receipt")

	orphaned := f.createRoute(t, ctx, "risk_findings", false, uuid.NullUUID{UUID: f.destination, Valid: true})
	_, err = dataexportsrepo.New(f.conn).SoftDeleteOtelDestination(ctx, dataexportsrepo.SoftDeleteOtelDestinationParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, ID: f.destination,
	})
	require.NoError(t, err)
	before = f.route(t, ctx, orphaned.ID)
	_, err = f.service.Resume(ctx, f.principal, f.input(orphaned.ID, "resume-deleted"))
	message = requireDataExportToggleCode(t, err, "destination_deleted")
	require.Contains(t, message, "has been deleted")
	require.Equal(t, before, f.route(t, ctx, orphaned.ID))
	require.False(t, f.receiptStored(t, ctx, operationResumeDataExport, "resume-deleted"))
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteResume))
}

func TestPauseDataExportRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_confirmation")
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})

	input := f.input(created.ID, "pause-unconfirmed")
	input.Confirmed = false
	_, err := f.service.Pause(ctx, f.principal, input)
	requireDataExportToggleCode(t, err, "invalid_request")
	require.True(t, f.route(t, ctx, created.ID).Enabled)
	require.False(t, f.receiptStored(t, ctx, operationPauseDataExport, "pause-unconfirmed"))
}

func TestPauseDataExportRefusesANonAdminIdenticallyForRealAndInventedRoutes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_member")
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, f.conn, f.principal.OrganizationID))
	memberID := "member_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, f.conn, f.principal.OrganizationID, memberID, authz.SystemRoleMember)
	engine := authz.NewEngine(testenv.NewLogger(t), f.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), f.conn, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), NewLiveOrgAdminAuthorizer(f.conn, engine), allowingBudget())
	require.NoError(t, err)
	member := Principal{UserID: memberID, OrganizationID: f.principal.OrganizationID, ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "client-test", Surface: SurfacePlatformMCP}

	_, realErr := service.Pause(ctx, member, f.input(created.ID, "member-real"))
	_, inventedErr := service.Pause(ctx, member, f.input(uuid.New(), "member-invented"))
	var denied *ExternalAuthorizationError
	require.ErrorAs(t, realErr, &denied)
	require.Equal(t, "org:admin", denied.RequiredScope)
	realRefusal := requireDataExportToggleCode(t, realErr, "permission_denied")
	inventedRefusal := requireDataExportToggleCode(t, inventedErr, "permission_denied")
	require.Equal(t, realRefusal, inventedRefusal, "a non-admin must not be able to tell a real route from an invented one")
	require.True(t, f.route(t, ctx, created.ID).Enabled)
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRoutePause))
}

func TestPauseDataExportCannotReachAnotherOrganizationsRoute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_tenancy")
	ownRoute := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	otherPrincipal, otherProject := seedRegistrationLifecycle(t, ctx, f.conn)
	other := dataExportToggleFixture{conn: f.conn, service: f.service, principal: otherPrincipal, project: otherProject, destination: uuid.Nil}
	foreign := other.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{})

	// The other organization's project and route, named by a caller in this one.
	_, err := f.service.Pause(ctx, f.principal, ToggleDataExportRouteInput{ProjectID: otherProject.ID.String(), RouteID: foreign.ID.String(), IdempotencyKey: "cross-org", Confirmed: true})
	notFound := requireDataExportToggleCode(t, err, "not_found")
	// This organization's project with the other organization's route.
	_, err = f.service.Pause(ctx, f.principal, f.input(foreign.ID, "cross-org-route"))
	require.Equal(t, notFound, requireDataExportToggleCode(t, err, "not_found"), "a missing project and a missing route read the same")
	require.True(t, other.route(t, ctx, foreign.ID).Enabled)
	require.True(t, f.route(t, ctx, ownRoute.ID).Enabled)
}

func TestDataExportToggleUnavailableManifestMatchesLive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_manifest")
	describe := func(service *DataExportRouteToggleService) map[string]Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "data-export-toggle", Version: "0.0.1"}, nil))
		registerDataExportRouteToggleTools(registrar, service)
		out := map[string]Descriptor{}
		for _, descriptor := range registrar.Descriptors() {
			out[descriptor.Name] = descriptor
		}
		return out
	}
	live := describe(f.service)
	unavailable := describe(nil)
	require.Len(t, unavailable, 2)
	require.Len(t, live, 2)
	for name, descriptor := range unavailable {
		other, ok := live[name]
		require.True(t, ok, "tool %q is registered on both paths", name)
		require.Equal(t, other.Title, descriptor.Title)
		require.Equal(t, other.Description, descriptor.Description)
		require.Equal(t, other.Meta, descriptor.Meta)
		require.Equal(t, other.Annotations, descriptor.Annotations)
		require.Equal(t, other.InputSchema, descriptor.InputSchema)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences, "%s", name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization, "%s", name)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope, "%s", name)
		require.Contains(t, descriptor.Description, "dropped, not buffered", "%s must say what happens to data while paused", name)
		require.Contains(t, descriptor.Description, "confirmed: true", "%s", name)

		refusal := invokeUnavailable(t, descriptor, map[string]any{
			"project_id": f.project.ID.String(), "route_id": uuid.NewString(), "idempotency_key": "stub", "confirmed": true,
		})
		require.Contains(t, refusal, `"feature":"data_export_pause"`)
	}
}

func TestPauseDataExportReplayIsNotChargedAgainstAnExhaustedBudget(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_budget_replay")
	connection := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	organization := &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}}
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), f.conn, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), stubAuthorizer{err: nil}, OperationBudget{Connection: connection, Organization: organization})
	require.NoError(t, err)
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})

	first, err := service.Pause(ctx, f.principal, f.input(created.ID, "pause-before-exhaustion"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomePaused, first.Outcome)
	require.Len(t, connection.keys, 1, "the first write is charged once")

	// Exhaust the allowance. A new change is refused and writes nothing.
	connection.result = ratelimit.Result{Allowed: false}
	organization.result = ratelimit.Result{Allowed: false}
	other := f.createRoute(t, ctx, "risk_findings", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	_, err = service.Pause(ctx, f.principal, f.input(other.ID, "pause-after-exhaustion"))
	requireDataExportToggleCode(t, err, "rate_limited")
	require.True(t, f.route(t, ctx, other.ID).Enabled)
	require.False(t, f.receiptStored(t, ctx, operationPauseDataExport, "pause-after-exhaustion"), "a refused charge rolls back the pending receipt")
	chargesBeforeReplay := len(connection.keys)

	// A retry of the change that already committed still returns its stored
	// result, and is not charged.
	replayed, err := service.Pause(ctx, f.principal, f.input(created.ID, "pause-before-exhaustion"))
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, first.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, dataExportOutcomePaused, replayed.Outcome)
	require.Len(t, connection.keys, chargesBeforeReplay, "a replay must not be charged")
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRoutePause))
}

// blockingLimiter holds every charge until released, so a test can inspect
// what the caller is holding while the budget round-trip is in flight.
type blockingLimiter struct {
	entered chan struct{}
	release chan struct{}
}

func (l *blockingLimiter) Allow(ctx context.Context, _ string) (ratelimit.Result, error) {
	select {
	case l.entered <- struct{}{}:
	default:
	}
	select {
	case <-l.release:
	case <-ctx.Done():
		return ratelimit.Result{}, fmt.Errorf("wait for blocked charge: %w", ctx.Err())
	}
	return ratelimit.Result{Allowed: true}, nil
}

func (l *blockingLimiter) AllowN(ctx context.Context, key string, _ int) (ratelimit.Result, error) {
	return l.Allow(ctx, key)
}

func TestPauseDataExportChargesTheBudgetWithNoConnectionHeld(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_budget_no_tx")
	limiter := &blockingLimiter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), f.conn, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), stubAuthorizer{err: nil}, OperationBudget{Connection: limiter, Organization: limiter})
	require.NoError(t, err)
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})

	done := make(chan error, 1)
	go func() {
		_, pauseErr := service.Pause(ctx, f.principal, f.input(created.ID, "pause-while-charging"))
		done <- pauseErr
	}()
	select {
	case <-limiter.entered:
	case err := <-done:
		require.FailNow(t, "pause finished without charging the budget", "%v", err)
	}
	// The charge is a network round-trip to Redis. While it is in flight the
	// call must hold no PostgreSQL connection — and so no transaction, row
	// lock, or receipt advisory lock — or a slow Redis would pin connections.
	require.Zero(t, f.conn.Stat().AcquiredConns(), "the budget must be charged outside any transaction")
	require.True(t, f.route(t, ctx, created.ID).Enabled, "nothing is written before the charge is granted")
	close(limiter.release)
	require.NoError(t, <-done)
	require.False(t, f.route(t, ctx, created.ID).Enabled)
}

func TestDataExportToggleNeverReturnsDatabaseErrorText(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_db_failure")
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	broken, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_data_export_toggle_db_failure_closed")
	require.NoError(t, err)
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), broken, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), stubAuthorizer{err: nil}, allowingBudget())
	require.NoError(t, err)
	// Every query on a closed pool fails with a driver error, which is what a
	// database outage looks like from here.
	broken.Close()

	_, err = service.Pause(ctx, f.principal, f.input(created.ID, "pause-db-down"))
	text := requireDataExportToggleCode(t, err, unavailableCode)
	for _, leaked := range []string{"pool", "closed", "resolve", "sql", "pgx", "postgres"} {
		require.NotContains(t, strings.ToLower(text), leaked, "the refusal must not carry database error text")
	}
}

func TestResumeDataExportRefusesAnEnabledRouteWhoseDestinationIsGone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_enabled_orphan")
	created := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})
	_, err := dataexportsrepo.New(f.conn).SoftDeleteOtelDestination(ctx, dataexportsrepo.SoftDeleteOtelDestinationParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, ID: f.destination,
	})
	require.NoError(t, err)
	before := f.route(t, ctx, created.ID)

	// Already enabled, but exporting nothing: "unchanged" would hide that.
	_, err = f.service.Resume(ctx, f.principal, f.input(created.ID, "resume-enabled-orphan"))
	requireDataExportToggleCode(t, err, "destination_deleted")
	require.Equal(t, before, f.route(t, ctx, created.ID))
	require.False(t, f.receiptStored(t, ctx, operationResumeDataExport, "resume-enabled-orphan"))
}

func TestResumeDataExportRefusesACorruptDestinationWithoutEchoingIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_corrupt")
	created := f.createRoute(t, ctx, "product_telemetry", false, uuid.NullUUID{UUID: f.destination, Valid: true})
	const corruptEndpoint = "not-a-url-corrupt-endpoint"
	_, err := dataexportsrepo.New(f.conn).UpdateOtelDestination(ctx, dataexportsrepo.UpdateOtelDestinationParams{
		Name: "Collector", EndpointUrl: corruptEndpoint, HeadersEncrypted: pgtype.Text{}, SensitiveData: pgtype.Text{String: "exclude", Valid: true},
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, ID: f.destination,
	})
	require.NoError(t, err)
	before := f.route(t, ctx, created.ID)

	_, err = f.service.Resume(ctx, f.principal, f.input(created.ID, "resume-corrupt"))
	text := requireDataExportToggleCode(t, err, "destination_invalid")
	require.NotContains(t, text, corruptEndpoint, "the refusal must not echo the stored endpoint")
	require.Equal(t, before, f.route(t, ctx, created.ID))
	require.False(t, f.receiptStored(t, ctx, operationResumeDataExport, "resume-corrupt"))
}
