package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	operationSetDataExportMinSeverity = "set_data_export_min_severity"

	dataExportLevelFeature = "data_export_min_severity"

	dataExportOutcomeLevelChanged = "changed"

	// The relays reuse a route lookup, level included, for up to 60 seconds, except for
	// destinations that include sensitive fields, which are never cached.
	dataExportLevelTakesEffect = "Within about a minute. The export relays reuse a route lookup for up to 60 seconds, so records can keep going out at the old level briefly; for a destination that includes sensitive fields it applies on the next batch."
)

type SetDataExportMinSeverityInput struct {
	ProjectID      string `json:"project_id" jsonschema:"project ID that owns the route, from list_data_exports"`
	RouteID        string `json:"route_id" jsonschema:"exact mcp_connection_logs route ID from list_data_exports"`
	MinSeverity    string `json:"min_severity" jsonschema:"lowest level the route sends: error (failed connections), warn (adds blocked connections), or info (adds declined connections)"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"caller-chosen key that makes a retry of this exact change safe"`
	Confirmed      bool   `json:"confirmed" jsonschema:"true only after the user confirmed this exact project, route, and new level"`
}

// SetDataExportMinSeverityOutput reports changed or unchanged; a refusal is an
// error result, never this struct.
type SetDataExportMinSeverityOutput struct {
	Outcome             string                  `json:"outcome"`
	PreviousMinSeverity string                  `json:"previous_min_severity"`
	Route               *DataExportRoute        `json:"route,omitempty"`
	SnapshotScope       string                  `json:"snapshot_scope"`
	TakesEffect         string                  `json:"takes_effect"`
	Receipt             RiskMutationToolReceipt `json:"receipt"`
}

type dataExportLevelReceipt struct {
	Outcome     string `json:"outcome"`
	RouteID     string `json:"route_id"`
	LevelBefore string `json:"min_severity_before"`
	LevelAfter  string `json:"min_severity_after"`
}

type dataExportLevelRequest struct {
	Operation   string `json:"operation"`
	ProjectID   string `json:"project_id"`
	RouteID     string `json:"route_id"`
	MinSeverity string `json:"min_severity"`
}

// WithRouteLevel composes changing an MCP connection logs route's level.
// Without it the tool stays in the catalogue as a stable refusal.
func (s *DataExportRouteToggleService) WithRouteLevel(core *dataexports.RouteLevelCore) *DataExportRouteToggleService {
	if s != nil {
		s.level = core
	}
	return s
}

func (s *DataExportRouteToggleService) levelValid() bool {
	return s.valid() && s.level != nil
}

func (s *DataExportRouteToggleService) SetMinSeverity(ctx context.Context, principal Principal, input SetDataExportMinSeverityInput) (SetDataExportMinSeverityOutput, error) {
	output, err := s.setMinSeverity(ctx, principal, input)
	return output, s.levelBoundaryError(ctx, err)
}

func (s *DataExportRouteToggleService) setMinSeverity(ctx context.Context, principal Principal, input SetDataExportMinSeverityInput) (SetDataExportMinSeverityOutput, error) {
	if !s.levelValid() {
		return SetDataExportMinSeverityOutput{}, dataExportLevelUnavailable(errors.New("data export level service is not composed"))
	}
	projectID, routeID, level, key, err := validateDataExportLevel(input)
	if err != nil {
		return SetDataExportMinSeverityOutput{}, err
	}
	if principal.OrganizationID == "" || principal.UserID == "" {
		return SetDataExportMinSeverityOutput{}, dataExportLevelInvalid("The request is missing its caller identity.")
	}
	// Authorized before any lookup, so a non-admin cannot tell real routes from invented ones.
	if err := s.admin.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return SetDataExportMinSeverityOutput{}, dataExportAdminError(err, dataExportLevelUnavailable)
	}
	row, err := s.queries.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return SetDataExportMinSeverityOutput{}, dataExportRouteMissing()
	}
	if err != nil {
		return SetDataExportMinSeverityOutput{}, dataExportLevelUnavailable(fmt.Errorf("resolve data export route project: %w", err))
	}
	project := ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}

	payload, err := json.Marshal(dataExportLevelRequest{Operation: operationSetDataExportMinSeverity, ProjectID: project.ID.String(), RouteID: routeID.String(), MinSeverity: level})
	if err != nil {
		return SetDataExportMinSeverityOutput{}, dataExportLevelInvalid("The request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-data-export-level-v1\x00"), payload...))
	inputHash := hex.EncodeToString(digest[:])

	charge := func(ctx context.Context) error {
		if err := s.changes.AllowConnectionOrOrganization(ctx, principal); err != nil {
			if errors.Is(err, ErrOperationRateLimited) {
				return &DataExportRefusalError{Code: "rate_limited", Message: "Changing data export levels was asked for too often just now. Try again shortly.", Cause: err}
			}
			return dataExportLevelUnavailable(err)
		}
		return nil
	}
	receipt, err := executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[dataExportLevelReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operationSetDataExportMinSeverity,
		IdempotencyKey: key, InputHash: inputHash, Label: "data export " + operationSetDataExportMinSeverity,
		Invalid: func(error) error { return dataExportLevelInvalid("The request is invalid.") },
		Conflict: func(message string) error {
			return &DataExportRefusalError{Code: "conflict", Message: message, Cause: ErrDataExportToggleConflict}
		},
		Unavailable:    dataExportLevelUnavailable,
		ValidateReplay: validDataExportLevelReceipt,
		EncodeResult: func(result dataExportLevelReceipt) ([]byte, error) {
			encoded, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("encode data export level receipt: %w", err)
			}
			return encoded, nil
		},
		Mutate: func(ctx context.Context, tx pgx.Tx) (dataExportLevelReceipt, error) {
			changed, err := s.level.SetMinSeverity(ctx, tx, dataexports.SetRouteMinSeverityParams{
				OrganizationID:   principal.OrganizationID,
				ProjectID:        project.ID,
				RouteID:          routeID,
				MinSeverity:      level,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
				ActorDisplayName: nil,
			})
			if err != nil {
				return dataExportLevelReceipt{}, classifyDataExportLevelError(err)
			}
			result := dataExportLevelReceipt{Outcome: dataExportOutcomeUnchanged, RouteID: changed.Route.ID.String(), LevelBefore: changed.Before, LevelAfter: changed.After}
			if changed.Changed {
				result.Outcome = dataExportOutcomeLevelChanged
			}
			return result, nil
		},
	})
	if err != nil {
		return SetDataExportMinSeverityOutput{}, err
	}
	var stored dataExportLevelReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil {
		return SetDataExportMinSeverityOutput{}, dataExportLevelUnavailable(err)
	}
	output := SetDataExportMinSeverityOutput{
		Outcome:             stored.Outcome,
		PreviousMinSeverity: stored.LevelBefore,
		Route:               nil,
		SnapshotScope:       "verification_unavailable",
		TakesEffect:         dataExportLevelTakesEffect,
		Receipt:             riskMutationToolReceipt(receipt),
	}
	if route := s.readRoute(ctx, principal, project, routeID); route != nil {
		output.Route = route
		output.SnapshotScope = "fresh_read_after_commit"
	}
	return output, nil
}

func validDataExportLevelReceipt(stored []byte) bool {
	var result dataExportLevelReceipt
	if json.Unmarshal(stored, &result) != nil {
		return false
	}
	return result.Outcome == dataExportOutcomeUnchanged || result.Outcome == dataExportOutcomeLevelChanged
}

func validateDataExportLevel(input SetDataExportMinSeverityInput) (uuid.UUID, uuid.UUID, string, string, error) {
	if !input.Confirmed {
		return uuid.Nil, uuid.Nil, "", "", &DataExportRefusalError{Code: "confirmation_required", Message: "Show the user the exact project, route, its current level, and the new level, then ask them to confirm before changing it.", Cause: ErrDataExportRequestInvalid}
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if key == "" || len(key) > 128 {
		return uuid.Nil, uuid.Nil, "", "", dataExportLevelInvalid("Provide a stable idempotency key of at most 128 characters so retrying this exact change is safe.")
	}
	projectID, err := uuid.Parse(strings.TrimSpace(input.ProjectID))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", "", dataExportLevelInvalid("Provide the explicit project ID that owns the route.")
	}
	routeID, err := uuid.Parse(strings.TrimSpace(input.RouteID))
	if err != nil {
		return uuid.Nil, uuid.Nil, "", "", dataExportLevelInvalid("Provide the exact route ID from the current data export list.")
	}
	level, err := dataexports.NormalizeMinSeverity(dataexports.DataSourceMCPConnectionLogs, input.MinSeverity)
	if err != nil {
		return uuid.Nil, uuid.Nil, "", "", dataExportLevelInvalid("min_severity must be info, warn, or error.")
	}
	return projectID, routeID, level, key, nil
}

func classifyDataExportLevelError(err error) error {
	switch {
	case errors.Is(err, dataexports.ErrRouteNotFound):
		return dataExportRouteMissing()
	case errors.Is(err, dataexports.ErrRouteMinSeverityInvalid):
		return dataExportLevelInvalid("Only mcp_connection_logs routes have a level. Nothing was changed. Choose an mcp_connection_logs route from the current data export list.")
	default:
		return dataExportLevelUnavailable(fmt.Errorf("set data export route level: %w", err))
	}
}

func dataExportLevelInvalid(message string) error {
	return &DataExportRefusalError{Code: "invalid_input", Message: message, Cause: ErrDataExportRequestInvalid}
}

const dataExportLevelUnavailableMessage = "Changing data export levels is temporarily unavailable."

func dataExportLevelUnavailable(cause error) error {
	return &DataExportRefusalError{Code: unavailableCode, Message: dataExportLevelUnavailableMessage, Cause: errors.Join(ErrUnavailable, cause)}
}

// levelBoundaryError keeps database text from reaching the caller, like boundaryError.
func (s *DataExportRouteToggleService) levelBoundaryError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	refusal, ok := errors.AsType[*DataExportRefusalError](err)
	if ok && refusal.Code != unavailableCode {
		return err
	}
	cause := err
	if ok && refusal.Cause != nil {
		cause = refusal.Cause
	}
	if s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "platform mcp data export "+operationSetDataExportMinSeverity+" failed", attr.SlogError(cause))
	}
	if ok {
		return &DataExportRefusalError{Code: unavailableCode, Message: dataExportLevelUnavailableMessage, Cause: refusal.Cause}
	}
	return dataExportLevelUnavailable(err)
}
