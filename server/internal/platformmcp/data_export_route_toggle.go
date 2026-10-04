package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	operationPauseDataExport  = "pause_data_export"
	operationResumeDataExport = "resume_data_export"

	dataExportToggleFeature = "data_export_pause"

	dataExportOutcomePaused    = "paused"
	dataExportOutcomeResumed   = "resumed"
	dataExportOutcomeUnchanged = "unchanged"

	// dataExportLastDeliveryNotRecorded is the only value last_delivery can
	// take today. The export relays count delivered and dropped records in
	// aggregate metrics, but nothing persists a per-route delivery time, so
	// the result says so rather than implying a time it does not know.
	dataExportLastDeliveryNotRecorded = "not_recorded"
)

var (
	ErrDataExportToggleInvalid  = errors.New("invalid data export pause or resume request")
	ErrDataExportToggleMissing  = errors.New("data export route not found")
	ErrDataExportToggleRefused  = errors.New("data export route cannot be resumed")
	ErrDataExportToggleConflict = errors.New("data export pause or resume idempotency conflict")
)

// DataExportToggleError is safe to map into a tool refusal: its message names
// what the caller can do next and never carries database detail.
type DataExportToggleError struct {
	Code    string
	Message string
	Cause   error
}

func (e *DataExportToggleError) Error() string { return e.Message }
func (e *DataExportToggleError) Unwrap() error { return e.Cause }

type ToggleDataExportRouteInput struct {
	ProjectID      string `json:"project_id" jsonschema:"project ID that owns the route, from list_data_exports"`
	RouteID        string `json:"route_id" jsonschema:"exact route ID from list_data_exports"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-chosen key that makes a retry of this exact change safe"`
	Confirmed      bool   `json:"confirmed" jsonschema:"true only after the user confirmed this exact project and route"`
}

// ToggleDataExportRouteOutput lets a caller tell the outcomes apart: the route
// changed state (paused or resumed), it was already in the requested state
// (unchanged), or the call was refused, which is returned as an error result
// and never as this struct.
type ToggleDataExportRouteOutput struct {
	Outcome string `json:"outcome"`
	// Route is a fresh read taken after the commit, so a caller reports the
	// committed state rather than the one it asked for. It is absent when
	// that read could not be taken; SnapshotScope says which.
	Route         *DataExportRoute `json:"route,omitempty"`
	SnapshotScope string           `json:"snapshot_scope"`
	// LastDelivery is "not_recorded": Gram does not keep a per-route time of
	// the last successful delivery.
	LastDelivery string `json:"last_delivery"`
	// WhilePaused states what happens to data produced while the route is
	// paused, so the caller never has to guess between dropped and buffered.
	WhilePaused string                  `json:"while_paused"`
	TakesEffect string                  `json:"takes_effect"`
	Receipt     RiskMutationToolReceipt `json:"receipt"`
}

const (
	// dataExportWhilePaused is the relay behaviour, stated once so the tool
	// descriptions and the result cannot disagree. The relays in
	// server/internal/otel resolve a route with GetActiveOtelRouteDestination,
	// which filters on enabled; a paused route resolves to no destination, and
	// every relay handler records those messages as dropped and acknowledges
	// them rather than failing them back for redelivery.
	dataExportWhilePaused = "Dropped, not buffered: data this route would have exported while it is paused is discarded, and resuming does not send any of it later. Only data produced after the resume reaches the destination."

	// dataExportTakesEffect reflects the relay's destination cache: a route
	// lookup, including "no active route", is reused for up to 60 seconds
	// unless the destination includes sensitive fields, which is never cached.
	dataExportTakesEffect = "Within about a minute. The export relays reuse a route lookup for up to 60 seconds, so data can keep flowing briefly after a pause and keep being dropped briefly after a resume; a destination that includes sensitive fields is looked up on every batch and changes immediately."
)

// DataExportRouteToggleService pauses and resumes one data export route. It
// writes through dataexports.RouteEnabledCore, which changes the enabled flag
// and nothing else.
type DataExportRouteToggleService struct {
	db      *pgxpool.Pool
	queries *platformrepo.Queries
	core    *dataexports.RouteEnabledCore
	// admin re-checks org:admin live, which is what the dashboard's route
	// update requires, before anything about the target is read.
	admin   Authorizer
	changes OperationBudget
	now     func() time.Time
}

func NewDataExportRouteToggleService(db *pgxpool.Pool, core *dataexports.RouteEnabledCore, admin Authorizer, changes OperationBudget) (*DataExportRouteToggleService, error) {
	if db == nil || core == nil || admin == nil || !changes.valid() {
		return nil, ErrDataExportToggleInvalid
	}
	return &DataExportRouteToggleService{db: db, queries: platformrepo.New(db), core: core, admin: admin, changes: changes, now: time.Now}, nil
}

func (s *DataExportRouteToggleService) valid() bool {
	return s != nil && s.db != nil && s.queries != nil && s.core != nil && s.admin != nil && s.changes.valid() && s.now != nil
}

func (s *DataExportRouteToggleService) Pause(ctx context.Context, principal Principal, input ToggleDataExportRouteInput) (ToggleDataExportRouteOutput, error) {
	return s.toggle(ctx, principal, operationPauseDataExport, false, input)
}

func (s *DataExportRouteToggleService) Resume(ctx context.Context, principal Principal, input ToggleDataExportRouteInput) (ToggleDataExportRouteOutput, error) {
	return s.toggle(ctx, principal, operationResumeDataExport, true, input)
}

type dataExportToggleReceipt struct {
	Outcome      string `json:"outcome"`
	RouteID      string `json:"route_id"`
	EnabledAfter bool   `json:"enabled_after"`
}

type dataExportToggleRequest struct {
	Operation string `json:"operation"`
	ProjectID string `json:"project_id"`
	RouteID   string `json:"route_id"`
}

func (s *DataExportRouteToggleService) toggle(ctx context.Context, principal Principal, operation string, enabled bool, input ToggleDataExportRouteInput) (ToggleDataExportRouteOutput, error) {
	if !s.valid() {
		return ToggleDataExportRouteOutput{}, dataExportToggleUnavailable(errors.New("data export pause service is not composed"))
	}
	projectID, routeID, key, err := validateDataExportToggle(input)
	if err != nil {
		return ToggleDataExportRouteOutput{}, err
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return ToggleDataExportRouteOutput{}, dataExportToggleInvalid("The request is missing its caller identity.")
	}
	// Authorized before the project or route is looked up, so a caller without
	// org:admin gets the same answer for a real route and an invented one.
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return ToggleDataExportRouteOutput{}, dataExportToggleAdminError(err)
	}
	row, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ToggleDataExportRouteOutput{}, dataExportToggleMissing()
	}
	if err != nil {
		return ToggleDataExportRouteOutput{}, fmt.Errorf("resolve data export route project: %w", err)
	}
	project := ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}
	// Charged after validation and authorization and before the route row is
	// locked, matching the other Platform MCP mutations: a refused call does
	// not spend the allowance, and nothing is written until it is paid for.
	if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return ToggleDataExportRouteOutput{}, &DataExportToggleError{Code: "rate_limited", Message: "Pausing or resuming data exports was asked for too often just now. Try again shortly.", Cause: err}
		}
		return ToggleDataExportRouteOutput{}, dataExportToggleUnavailable(err)
	}

	payload, err := json.Marshal(dataExportToggleRequest{Operation: operation, ProjectID: project.ID.String(), RouteID: routeID.String()})
	if err != nil {
		return ToggleDataExportRouteOutput{}, dataExportToggleInvalid("The request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-data-export-toggle-v1\x00"), payload...))

	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[dataExportToggleReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation,
		IdempotencyKey: key, InputHash: hex.EncodeToString(digest[:]), Label: "data export " + operation,
		Invalid: func(error) error { return dataExportToggleInvalid("The request is invalid.") },
		Conflict: func(message string) error {
			return &DataExportToggleError{Code: "conflict", Message: message, Cause: ErrDataExportToggleConflict}
		},
		Unavailable: dataExportToggleUnavailable,
		ValidateReplay: func(stored []byte) bool {
			var result dataExportToggleReceipt
			if json.Unmarshal(stored, &result) != nil {
				return false
			}
			return result.Outcome == dataExportOutcomeUnchanged || result.Outcome == dataExportOutcomePaused || result.Outcome == dataExportOutcomeResumed
		},
		EncodeResult: func(result dataExportToggleReceipt) ([]byte, error) {
			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode data export toggle receipt: %w", err)
			}
			return encoded, nil
		},
		Mutate: func(ctx context.Context, tx pgx.Tx) (dataExportToggleReceipt, error) {
			changed, err := s.core.SetEnabled(ctx, tx, dataexports.SetRouteEnabledParams{
				OrganizationID:   principal.OrganizationID,
				ProjectID:        project.ID,
				RouteID:          routeID,
				Enabled:          enabled,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
				ActorDisplayName: nil,
			})
			if err != nil {
				return dataExportToggleReceipt{}, classifyDataExportToggleError(err)
			}
			result := dataExportToggleReceipt{Outcome: dataExportOutcomeUnchanged, RouteID: changed.After.ID.String(), EnabledAfter: changed.After.Enabled}
			if changed.Changed {
				result.Outcome = dataExportOutcomePaused
				if enabled {
					result.Outcome = dataExportOutcomeResumed
				}
			}
			return result, nil
		},
	})
	if err != nil {
		return ToggleDataExportRouteOutput{}, err
	}
	var stored dataExportToggleReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil {
		return ToggleDataExportRouteOutput{}, dataExportToggleUnavailable(err)
	}
	output := ToggleDataExportRouteOutput{
		Outcome:       stored.Outcome,
		Route:         nil,
		SnapshotScope: "verification_unavailable",
		LastDelivery:  dataExportLastDeliveryNotRecorded,
		WhilePaused:   dataExportWhilePaused,
		TakesEffect:   dataExportTakesEffect,
		Receipt:       riskMutationToolReceipt(receipt),
	}
	if route := s.readRoute(ctx, principal, project, routeID); route != nil {
		output.Route = route
		output.SnapshotScope = "fresh_read_after_commit"
	}
	return output, nil
}

// readRoute is the post-commit verification read, nil when it cannot be taken.
// A project holds at most one live route per data source, so listing them is
// a bounded read.
func (s *DataExportRouteToggleService) readRoute(ctx context.Context, principal Principal, project ResolvedProject, routeID uuid.UUID) *DataExportRoute {
	rows, err := dataexportsrepo.New(s.db).ListDataExportRoutes(ctx, dataexportsrepo.ListDataExportRoutesParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	if err != nil {
		return nil
	}
	for _, row := range rows {
		if row.ID != routeID {
			continue
		}
		return &DataExportRoute{
			ID:            row.ID.String(),
			ProjectID:     project.ID.String(),
			ProjectName:   project.Name,
			ProjectSlug:   project.Slug,
			DataSource:    row.DataSource,
			Enabled:       row.Enabled,
			DestinationID: uuidString(row.OtelDestinationID),
		}
	}
	return nil
}

func validateDataExportToggle(input ToggleDataExportRouteInput) (uuid.UUID, uuid.UUID, string, error) {
	if !input.Confirmed {
		return uuid.Nil, uuid.Nil, "", dataExportToggleInvalid("Confirm the exact project and route with the user before pausing or resuming it.")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" || len(key) > 128 {
		return uuid.Nil, uuid.Nil, "", dataExportToggleInvalid("Provide a stable idempotency key of at most 128 characters so retrying this exact change is safe.")
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", dataExportToggleInvalid("Provide the explicit project ID that owns the route.")
	}
	routeID, err := uuid.Parse(strings.TrimSpace(input.RouteID))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", dataExportToggleInvalid("Provide the exact route ID from the current data export list.")
	}
	return projectID, routeID, key, nil
}

func classifyDataExportToggleError(err error) error {
	switch {
	case errors.Is(err, dataexports.ErrRouteNotFound):
		return dataExportToggleMissing()
	case errors.Is(err, dataexports.ErrRouteDestinationRequired):
		return &DataExportToggleError{
			Code:    "no_destination",
			Message: "This route has no destination, so resuming it would have nowhere to send data. Nothing was changed. Choose a destination for the route in the dashboard first.",
			Cause:   errors.Join(ErrDataExportToggleRefused, err),
		}
	case errors.Is(err, dataexports.ErrRouteDestinationInactive):
		return &DataExportToggleError{
			Code:    "destination_deleted",
			Message: "This route's destination has been deleted, so resuming it would have nowhere to send data. Nothing was changed. Point the route at an active destination in the dashboard first.",
			Cause:   errors.Join(ErrDataExportToggleRefused, err),
		}
	default:
		return fmt.Errorf("set data export route enabled: %w", err)
	}
}

func dataExportToggleAdminError(err error) error {
	mapped := toolExposureAuthorizationError(err, authz.ScopeOrgAdmin)
	if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); ok {
		return mapped
	}
	return dataExportToggleUnavailable(err)
}

func dataExportToggleInvalid(message string) error {
	return &DataExportToggleError{Code: "invalid_request", Message: message, Cause: ErrDataExportToggleInvalid}
}

// dataExportToggleMissing is one message for a missing project and a missing
// route, so the refusal does not say which of the two ids was wrong.
func dataExportToggleMissing() error {
	return &DataExportToggleError{
		Code:    "not_found",
		Message: "No data export route with that ID exists in that project. Read the current data exports and use a route ID and project ID from that list.",
		Cause:   ErrDataExportToggleMissing,
	}
}

func dataExportToggleUnavailable(cause error) error {
	return &DataExportToggleError{
		Code:    unavailableCode,
		Message: "Pausing or resuming data exports is temporarily unavailable.",
		Cause:   errors.Join(ErrUnavailable, cause),
	}
}

// WithDataExportRouteToggle composes pause and resume. Without it both tools
// stay in the catalogue as stable refusals rather than disappearing from it.
func (r *PostgresReader) WithDataExportRouteToggle(service *DataExportRouteToggleService) *PostgresReader {
	if r != nil {
		r.dataExportRouteToggle = service
	}
	return r
}
