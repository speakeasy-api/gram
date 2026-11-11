package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	operationSendDataExportTestEvent = "send_data_export_test_event"
	dataExportTestFeature            = "data_export_test_event"
)

type SendDataExportTestEventInput struct {
	ProjectID             string `json:"project_id" jsonschema:"project ID that owns the route, from list_data_exports"`
	RouteID               string `json:"route_id" jsonschema:"exact route ID of an mcp_connection_logs route, from list_data_exports"`
	ExpectedDestinationID string `json:"expected_destination_id" jsonschema:"the route's destination_id exactly as list_data_exports returned it and the user confirmed"`
	Confirmed             bool   `json:"confirmed" jsonschema:"true only after the user confirmed this exact project, route, and destination"`
}

// SendDataExportTestEventOutput is what the destination answered. It never
// carries header values or the destination's response body.
type SendDataExportTestEventOutput struct {
	Outcome       string `json:"outcome" jsonschema:"accepted, refused, not_found, unreachable, or error"`
	StatusCode    int    `json:"status_code,omitempty" jsonschema:"HTTP status the destination answered with; omitted when it did not answer"`
	Message       string `json:"message" jsonschema:"what happened, in plain language, to show the user"`
	ProjectID     string `json:"project_id"`
	RouteID       string `json:"route_id"`
	DestinationID string `json:"destination_id"`
}

// DataExportTestEventService sends one test event for an MCP connection logs
// export through dataexports.RouteTestCore, the dashboard's own path.
type DataExportTestEventService struct {
	logger  *slog.Logger
	queries *platformrepo.Queries
	core    *dataexports.RouteTestCore
	admin   Authorizer
}

func NewDataExportTestEventService(logger *slog.Logger, db *pgxpool.Pool, core *dataexports.RouteTestCore, admin Authorizer) *DataExportTestEventService {
	if logger == nil || db == nil || core == nil || admin == nil {
		return nil
	}
	return &DataExportTestEventService{logger: logger, queries: platformrepo.New(db), core: core, admin: admin}
}

func (s *DataExportTestEventService) valid() bool {
	return s != nil && s.logger != nil && s.queries != nil && s.core != nil && s.admin != nil
}

func (s *DataExportTestEventService) Send(ctx context.Context, principal Principal, input SendDataExportTestEventInput) (SendDataExportTestEventOutput, error) {
	output, err := s.send(ctx, principal, input)
	if err == nil {
		return output, nil
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return SendDataExportTestEventOutput{}, err
	}
	if refusal, ok := errors.AsType[*DataExportToggleError](err); ok && refusal.Code != unavailableCode {
		return SendDataExportTestEventOutput{}, err
	}
	if s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "platform mcp data export test event failed", attr.SlogError(err))
	}
	return SendDataExportTestEventOutput{}, dataExportTestUnavailable(err)
}

func (s *DataExportTestEventService) send(ctx context.Context, principal Principal, input SendDataExportTestEventInput) (SendDataExportTestEventOutput, error) {
	if !s.valid() {
		return SendDataExportTestEventOutput{}, errors.New("data export test event service is not composed")
	}
	if !input.Confirmed {
		return SendDataExportTestEventOutput{}, dataExportTestInvalid("Confirm the exact project, route, and destination with the user before sending a test event to it.")
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return SendDataExportTestEventOutput{}, dataExportTestInvalid("Provide the explicit project ID that owns the route.")
	}
	routeID, err := uuid.Parse(strings.TrimSpace(input.RouteID))
	if err != nil {
		return SendDataExportTestEventOutput{}, dataExportTestInvalid("Provide the exact route ID from the current data export list.")
	}
	destinationID, err := uuid.Parse(strings.TrimSpace(input.ExpectedDestinationID))
	if err != nil {
		return SendDataExportTestEventOutput{}, dataExportTestInvalid("Provide expected_destination_id exactly as the current data export list shows it for this route.")
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return SendDataExportTestEventOutput{}, dataExportTestInvalid("The request is missing its caller identity.")
	}
	// Authorized before the target is read, so a non-admin gets the same answer for any route.
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return SendDataExportTestEventOutput{}, dataExportToggleAdminError(err)
	}
	project, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SendDataExportTestEventOutput{}, dataExportTestMissing()
	}
	if err != nil {
		return SendDataExportTestEventOutput{}, fmt.Errorf("resolve data export test project: %w", err)
	}

	result, err := s.core.SendTest(ctx, dataexports.SendRouteTestParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		RouteID:        routeID,
		Expect: &dataexports.RouteExpectation{
			DataSource:        dataexports.DataSourceMCPConnectionLogs,
			OtelDestinationID: uuid.NullUUID{UUID: destinationID, Valid: true},
		},
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName: nil,
	})
	if err != nil {
		return SendDataExportTestEventOutput{}, classifyDataExportTestError(err)
	}
	return SendDataExportTestEventOutput{
		Outcome:       string(result.Outcome),
		StatusCode:    result.StatusCode,
		Message:       result.Message,
		ProjectID:     project.ID.String(),
		RouteID:       routeID.String(),
		DestinationID: result.DestinationID.String(),
	}, nil
}

func classifyDataExportTestError(err error) error {
	refusal := func(code, message string) error {
		return &DataExportToggleError{Code: code, Message: message, Cause: err}
	}
	switch {
	case errors.Is(err, dataexports.ErrRouteNotFound):
		return dataExportTestMissing()
	case errors.Is(err, dataexports.ErrRouteTestUnsupported):
		return refusal("unsupported_data_source", "Test events are only available for MCP connection logs exports. Nothing was sent.")
	case errors.Is(err, dataexports.ErrRouteChanged):
		return refusal("route_changed", "This route's destination changed after it was read, so nothing was sent. Read the current data exports again and confirm the destination with the user.")
	case errors.Is(err, dataexports.ErrRouteDestinationRequired):
		return refusal("no_destination", "This route has no destination, so there is nowhere to send a test event. Choose a destination for the route in the dashboard first.")
	case errors.Is(err, dataexports.ErrRouteDestinationInactive):
		return refusal("destination_deleted", "This route's destination was deleted, so nothing was sent. Point the route at an active destination in the dashboard first.")
	case errors.Is(err, dataexports.ErrRouteDestinationInvalid):
		return refusal("destination_invalid", "This route's destination has a stored configuration that can no longer be used, so nothing was sent. Open the destination in the dashboard and save it again.")
	case errors.Is(err, dataexports.ErrRouteTestRateLimited):
		return refusal("rate_limited", "This route sent too many test events just now. Try again in a minute.")
	default:
		return err
	}
}

func dataExportTestInvalid(message string) error {
	return &DataExportToggleError{Code: "invalid_request", Message: message, Cause: ErrDataExportToggleInvalid}
}

// dataExportTestMissing is one message for a missing project and a missing route.
func dataExportTestMissing() error {
	return &DataExportToggleError{
		Code:    "not_found",
		Message: "No data export route with that ID exists in that project. Read the current data exports and use a route ID and project ID from that list.",
		Cause:   ErrDataExportToggleMissing,
	}
}

func dataExportTestUnavailable(cause error) error {
	return &DataExportToggleError{
		Code:    unavailableCode,
		Message: "Sending a data export test event is temporarily unavailable.",
		Cause:   errors.Join(ErrUnavailable, cause),
	}
}

// WithDataExportTestEvent composes the test event tool. Without it the tool
// stays in the catalogue as a stable refusal.
func (r *PostgresReader) WithDataExportTestEvent(service *DataExportTestEventService) *PostgresReader {
	if r != nil {
		r.dataExportTestEvent = service
	}
	return r
}
