package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func (f dataExportToggleFixture) setLevel(t *testing.T, ctx context.Context, routeID uuid.UUID, level string) {
	t.Helper()
	_, err := dataexportsrepo.New(f.conn).UpsertMCPConnectionLogRouteSettings(ctx, dataexportsrepo.UpsertMCPConnectionLogRouteSettingsParams{
		OrganizationID: f.principal.OrganizationID, ProjectID: f.project.ID, MinSeverity: level, DataExportRouteID: routeID,
	})
	require.NoError(t, err)
}

func (f dataExportToggleFixture) levelInput(routeID uuid.UUID, level, key string) SetDataExportMinSeverityInput {
	return SetDataExportMinSeverityInput{ProjectID: f.project.ID.String(), RouteID: routeID.String(), MinSeverity: level, IdempotencyKey: key, Confirmed: true}
}

func requireAuditSnapshotLevel(t *testing.T, ctx context.Context, f dataExportToggleFixture, action audit.Action, before, after string) {
	t.Helper()
	record, err := audittest.LatestAuditLogByAction(ctx, f.conn, action)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, before, beforeSnapshot["min_severity"], "%s before", action)
	require.Equal(t, after, afterSnapshot["min_severity"], "%s after", action)
}

func TestPauseAndResumeDataExportKeepTheConnectionLogLevel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_toggle_level")
	created := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})
	f.setLevel(t, ctx, created.ID, dataexports.MinSeverityInfo)

	paused, err := f.service.Pause(ctx, f.principal, f.input(created.ID, "pause-level"))
	require.NoError(t, err)
	require.Equal(t, dataexports.MinSeverityInfo, paused.Route.MinSeverity)
	requireAuditSnapshotLevel(t, ctx, f, audit.ActionDataExportRoutePause, dataexports.MinSeverityInfo, dataexports.MinSeverityInfo)

	resumed, err := f.service.Resume(ctx, f.principal, f.resumeInput(t, ctx, created.ID, "resume-level"))
	require.NoError(t, err)
	require.True(t, resumed.Route.Enabled)
	require.Equal(t, dataexports.MinSeverityInfo, resumed.Route.MinSeverity)
	requireAuditSnapshotLevel(t, ctx, f, audit.ActionDataExportRouteResume, dataexports.MinSeverityInfo, dataexports.MinSeverityInfo)
}

func TestSetDataExportMinSeverityChangesOnlyTheLevel(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_level")
	created := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})
	original := f.route(t, ctx, created.ID)

	output, err := f.service.SetMinSeverity(ctx, f.principal, f.levelInput(created.ID, " WARN ", "level-1"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomeLevelChanged, output.Outcome)
	require.Equal(t, dataexports.MinSeverityError, output.PreviousMinSeverity)
	require.Equal(t, "fresh_read_after_commit", output.SnapshotScope)
	require.Equal(t, dataexports.MinSeverityWarn, output.Route.MinSeverity)
	require.True(t, output.Route.Enabled)
	require.Equal(t, original, f.route(t, ctx, created.ID), "the route row is untouched")
	requireAuditSnapshotLevel(t, ctx, f, audit.ActionDataExportRouteUpdate, dataexports.MinSeverityError, dataexports.MinSeverityWarn)

	replayed, err := f.service.SetMinSeverity(ctx, f.principal, f.levelInput(created.ID, "warn", "level-1"))
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, dataExportOutcomeLevelChanged, replayed.Outcome)

	unchanged, err := f.service.SetMinSeverity(ctx, f.principal, f.levelInput(created.ID, "warn", "level-2"))
	require.NoError(t, err)
	require.Equal(t, dataExportOutcomeUnchanged, unchanged.Outcome)
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteUpdate))

	_, err = f.service.SetMinSeverity(ctx, f.principal, f.levelInput(created.ID, "info", "level-1"))
	requireDataExportToggleCode(t, err, "conflict")
}

func TestSetDataExportMinSeverityRefusals(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_level_refusals")
	connectionLogs := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})
	telemetry := f.createRoute(t, ctx, "product_telemetry", true, uuid.NullUUID{UUID: f.destination, Valid: true})

	_, err := f.service.SetMinSeverity(ctx, f.principal, f.levelInput(telemetry.ID, "warn", "other-source"))
	requireDataExportToggleCode(t, err, "invalid_input")
	require.False(t, f.receiptStored(t, ctx, operationSetDataExportMinSeverity, "other-source"))

	_, err = f.service.SetMinSeverity(ctx, f.principal, f.levelInput(connectionLogs.ID, "debug", "bad-level"))
	requireDataExportToggleCode(t, err, "invalid_input")

	unconfirmed := f.levelInput(connectionLogs.ID, "warn", "unconfirmed")
	unconfirmed.Confirmed = false
	_, err = f.service.SetMinSeverity(ctx, f.principal, unconfirmed)
	requireDataExportToggleCode(t, err, "confirmation_required")

	_, err = f.service.SetMinSeverity(ctx, f.principal, f.levelInput(uuid.New(), "warn", "missing"))
	requireDataExportToggleCode(t, err, "not_found")

	otherPrincipal, otherProject := seedRegistrationLifecycle(t, ctx, f.conn)
	other := dataExportToggleFixture{conn: f.conn, service: f.service, principal: otherPrincipal, project: otherProject, destination: uuid.Nil}
	foreign := other.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, false, uuid.NullUUID{})
	_, err = f.service.SetMinSeverity(ctx, f.principal, f.levelInput(foreign.ID, "warn", "cross-org"))
	requireDataExportToggleCode(t, err, "not_found")

	require.NoError(t, authz.SeedSystemRoleGrants(ctx, f.conn, f.principal.OrganizationID))
	memberID := "member_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, f.conn, f.principal.OrganizationID, memberID, authz.SystemRoleMember)
	engine := authz.NewEngine(testenv.NewLogger(t), f.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	service, err := NewDataExportRouteToggleService(testenv.NewLogger(t), f.conn, dataexports.NewRouteEnabledCore(audit.NewLogger(), testenv.NewEncryptionClient(t)), NewLiveOrgAdminAuthorizer(f.conn, engine), allowingBudget())
	require.NoError(t, err)
	service = service.WithRouteLevel(dataexports.NewRouteLevelCore(audit.NewLogger()))
	member := Principal{UserID: memberID, OrganizationID: f.principal.OrganizationID, ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "client-test", Surface: SurfacePlatformMCP}
	_, err = service.SetMinSeverity(ctx, member, f.levelInput(connectionLogs.ID, "warn", "member"))
	requireDataExportToggleCode(t, err, "permission_denied")

	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteUpdate))
}

func TestSetDataExportMinSeverityUnavailableManifestMatchesLive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportToggleFixture(t, ctx, "platform_mcp_data_export_level_manifest")
	describe := func(service *DataExportRouteToggleService) Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "data-export-level", Version: "0.0.1"}, nil))
		registrar.withProjects(&RegistrationStore{db: f.conn, icons: nil})
		registerDataExportLevelTool(registrar, service)
		descriptors := registrar.Descriptors()
		require.Len(t, descriptors, 1)
		return descriptors[0]
	}
	live := describe(f.service)
	unavailable := describe(nil)
	require.Equal(t, setDataExportMinSeverityToolName, live.Name)
	require.Equal(t, live.Title, unavailable.Title)
	require.Equal(t, live.Description, unavailable.Description)
	require.Equal(t, live.Meta, unavailable.Meta)
	require.Equal(t, live.Annotations, unavailable.Annotations)
	require.Equal(t, live.InputSchema, unavailable.InputSchema)
	require.Equal(t, externalOnly, live.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationOrgAdmin, live.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, live.Meta.ProjectScope)
	require.Contains(t, live.Description, "confirmed: true")

	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(live.InputSchema, &schema))
	require.Subset(t, schema.Required, []string{"project_slug", "route_id", "min_severity", "idempotency_key", "confirmed"})

	refusal := invokeUnavailable(t, ContextWithPrincipal(ctx, f.principal), unavailable, map[string]any{
		"project_slug": f.project.Slug, "route_id": uuid.NewString(), "min_severity": "warn", "idempotency_key": "stub", "confirmed": true,
	})
	require.Contains(t, refusal, `"feature":"data_export_min_severity"`)

	// A toggle service without the level core keeps the tool as a refusal.
	require.Equal(t, live.InputSchema, describe(&DataExportRouteToggleService{}).InputSchema)
}
