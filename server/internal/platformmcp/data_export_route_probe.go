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
	RouteID               string `json:"route_id" jsonschema:"exact route ID from list_data_exports"`
	ExpectedDataSource    string `json:"expected_data_source" jsonschema:"the route's data_source exactly as list_data_exports returned it and the user confirmed"`
	ExpectedDestinationID string `json:"expected_destination_id" jsonschema:"the route's destination_id exactly as list_data_exports returned it and the user confirmed; empty only when the list showed no destination"`
	Confirmed             bool   `json:"confirmed" jsonschema:"true only after the user confirmed this exact project, route, data source, and destination"`
}

// SendDataExportTestEventSignal is what the destination answered to one signal's test event.
type SendDataExportTestEventSignal struct {
	Signal     string `json:"signal" jsonschema:"logs, traces, or metrics"`
	Outcome    string `json:"outcome" jsonschema:"accepted, refused, not_found, unreachable, or error"`
	StatusCode int    `json:"status_code,omitempty" jsonschema:"HTTP status the destination answered with; omitted when it did not answer"`
	Message    string `json:"message" jsonschema:"what happened to this signal, in plain language"`
}

// SendDataExportTestEventOutput is what the destination answered. It never
// carries header values or the destination's response body.
type SendDataExportTestEventOutput struct {
	Outcome       string                          `json:"outcome" jsonschema:"accepted when every signal was accepted; otherwise the worst signal's outcome: unreachable, refused, not_found, or error"`
	StatusCode    int                             `json:"status_code,omitempty" jsonschema:"HTTP status of the signal that set the outcome; omitted when it did not answer"`
	Message       string                          `json:"message" jsonschema:"what happened, in plain language, to show the user"`
	Signals       []SendDataExportTestEventSignal `json:"signals" jsonschema:"one result for each signal the route's data source exports"`
	TestEventID   string                          `json:"test_event_id" jsonschema:"the speakeasy.test_event.id this test's logs and traces carry; the user can search the destination for it. Metrics carry only speakeasy.test_event=true"`
	ProjectID     string                          `json:"project_id"`
	RouteID       string                          `json:"route_id"`
	DataSource    string                          `json:"data_source"`
	DestinationID string                          `json:"destination_id"`
}

// DataExportTestEventService sends a data export route's test events through
// dataexports.RouteTestCore, the dashboard's own path.
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
	refusal, ok := errors.AsType[*DataExportRefusalError](err)
	if ok && refusal.Code != unavailableCode {
		return SendDataExportTestEventOutput{}, err
	}
	if s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "platform mcp data export test event failed", attr.SlogError(err))
	}
	if ok {
		return SendDataExportTestEventOutput{}, err
	}
	return SendDataExportTestEventOutput{}, dataExportTestUnavailable(err)
}

func (s *DataExportTestEventService) send(ctx context.Context, principal Principal, input SendDataExportTestEventInput) (SendDataExportTestEventOutput, error) {
	if !s.valid() {
		return SendDataExportTestEventOutput{}, errors.New("data export test event service is not composed")
	}
	if !input.Confirmed {
		return SendDataExportTestEventOutput{}, dataExportInvalid("Confirm the exact project, route, data source, and destination with the user before sending a test event to it.")
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return SendDataExportTestEventOutput{}, dataExportInvalid("Provide the explicit project ID that owns the route.")
	}
	routeID, err := uuid.Parse(strings.TrimSpace(input.RouteID))
	if err != nil {
		return SendDataExportTestEventOutput{}, dataExportInvalid("Provide the exact route ID from the current data export list.")
	}
	expect, err := validateRouteExpectation(input.ExpectedDataSource, input.ExpectedDestinationID)
	if err != nil {
		return SendDataExportTestEventOutput{}, err
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return SendDataExportTestEventOutput{}, dataExportInvalid("The request is missing its caller identity.")
	}
	// Authorized before the target is read, so a non-admin gets the same answer for any route.
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return SendDataExportTestEventOutput{}, dataExportAdminError(err, dataExportTestUnavailable)
	}
	project, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SendDataExportTestEventOutput{}, dataExportRouteMissing()
	}
	if err != nil {
		return SendDataExportTestEventOutput{}, fmt.Errorf("resolve data export test project: %w", err)
	}

	result, err := s.core.SendTest(ctx, dataexports.SendRouteTestParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		RouteID:        routeID,
		Expect:           expect,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
		ActorDisplayName: nil,
	})
	if err != nil {
		return SendDataExportTestEventOutput{}, classifyDataExportTestError(err)
	}
	signals := make([]SendDataExportTestEventSignal, len(result.Signals))
	for i, signal := range result.Signals {
		signals[i] = SendDataExportTestEventSignal{Signal: string(signal.Signal), Outcome: string(signal.Outcome), StatusCode: signal.StatusCode, Message: signal.Message}
	}
	return SendDataExportTestEventOutput{
		Outcome:       string(result.Outcome),
		StatusCode:    result.StatusCode,
		Message:       result.Message,
		Signals:       signals,
		TestEventID:   result.TestEventID,
		ProjectID:     project.ID.String(),
		RouteID:       routeID.String(),
		DataSource:    result.DataSource,
		DestinationID: result.DestinationID.String(),
	}, nil
}

func classifyDataExportTestError(err error) error {
	refusal := func(code, message string) error {
		return &DataExportRefusalError{Code: code, Message: message, Cause: err}
	}
	switch {
	case errors.Is(err, dataexports.ErrRouteNotFound):
		return dataExportRouteMissing()
	case errors.Is(err, dataexports.ErrRouteTestUnsupported):
		return refusal("unsupported_data_source", "This route's data source has no test event. Nothing was sent.")
	case errors.Is(err, dataexports.ErrRouteChanged):
		return refusal("route_changed", "This route's data source or destination changed after it was read, so nothing was sent. Read the current data exports again and confirm them with the user.")
	case errors.Is(err, dataexports.ErrRouteDestinationRequired):
		return refusal("no_destination", "This route has no destination, so there is nowhere to send a test event. Choose a destination for the route in the dashboard first.")
	case errors.Is(err, dataexports.ErrRouteDestinationInactive):
		return refusal("destination_deleted", "This route's destination was deleted, so nothing was sent. Point the route at an active destination in the dashboard first.")
	case errors.Is(err, dataexports.ErrRouteDestinationInvalid):
		return refusal("destination_invalid", "This route's destination has a stored configuration that can no longer be used, so nothing was sent. Open the destination in the dashboard and save it again.")
	case errors.Is(err, dataexports.ErrRouteTestRateLimited):
		return refusal("rate_limited", "Too many test events were sent just now. Try again in a minute.")
	case errors.Is(err, dataexports.ErrRouteTestUnavailable):
		return &DataExportRefusalError{Code: unavailableCode, Message: "Speakeasy can't send test events right now. Try again shortly.", Cause: errors.Join(ErrUnavailable, err)}
	default:
		return err
	}
}

func dataExportTestUnavailable(cause error) error {
	return &DataExportRefusalError{
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
