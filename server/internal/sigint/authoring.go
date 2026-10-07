package sigint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

// maxAuthoringResources bounds snapshot, preview, and shared-signal impact work.
const maxAuthoringResources = 1000

// maxBundleSignals bounds one atomic authoring operation.
const maxBundleSignals = 100

// SignalDefinition is an inline reusable signal authored with a sensor.
type SignalDefinition struct {
	// Name is the display name.
	Name string `json:"name"`
	// Slug is optional; the backend normalizes it or derives it from Name.
	Slug *string `json:"slug,omitempty"`
	// Description is optional authoring context.
	Description *string `json:"description,omitempty"`
	// Criteria describes the classifier's positive case.
	Criteria *string `json:"classifier_criteria,omitempty"`
}

// SignalReference selects exactly one existing signal or inline definition.
type SignalReference struct {
	// ID identifies an existing signal in this project.
	ID string `json:"existing_signal_id,omitempty"`
	// New creates one reusable signal atomically with the sensor.
	New *SignalDefinition `json:"new_signal,omitempty"`
}

// AuthoringInput is a complete bounded mutation proposal. Update operations
// preserve omitted optional fields; a supplied signals list replaces membership.
type AuthoringInput struct {
	// Operation selects create_sensor, create_signal, update_sensor, or update_signal.
	Operation string `json:"operation"`
	// ID is required for updates and absent for creation.
	ID string `json:"id,omitempty"`
	// Name is required for creation and optional for updates.
	Name *string `json:"name,omitempty"`
	// Slug is normalized by the owning service.
	Slug *string `json:"slug,omitempty"`
	// Description supplies optional authoring context.
	Description *string `json:"description,omitempty"`
	// Criteria applies only to signal operations.
	Criteria *string `json:"classifier_criteria,omitempty"`
	// Instructions applies only to sensor operations.
	Instructions *string `json:"instructions,omitempty"`
	// Mode applies only to sensor operations.
	Mode *string `json:"mode,omitempty"`
	// MatchExpression applies only to sensor operations.
	MatchExpression *string `json:"match_expression,omitempty"`
	// Signals is the complete ordered sensor membership when supplied.
	Signals *[]SignalReference `json:"signals,omitempty"`
}

// AuthoringState is a bounded live configuration snapshot. Version covers the
// project configuration, including shared definitions and ordered memberships.
type AuthoringState struct {
	// Version binds previews to the complete configuration read under the project lock.
	Version string `json:"version"`
	// Sensors includes all active sensors in UUID order.
	Sensors []*types.SigintSensor `json:"sensors"`
	// Signals includes all active signals in UUID order.
	Signals []*types.SigintSignal `json:"signals"`
}

// AuthoringResult describes the normalized proposed or committed target.
type AuthoringResult struct {
	// Sensor is present for sensor operations.
	Sensor *types.SigintSensor `json:"sensor,omitempty"`
	// Signal is present for signal operations.
	Signal *types.SigintSignal `json:"signal,omitempty"`
	// State expands references and shows the shared-signal impact.
	State AuthoringState `json:"state"`
}

// ReadAuthoringState checks entitlement and project read access before returning
// a consistent bounded snapshot. It never reveals another project's resources.
func (s *Service) ReadAuthoringState(ctx context.Context) (AuthoringState, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return AuthoringState{}, fmt.Errorf("begin authoring read: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	return s.authoringState(ctx, tx, authz.ScopeProjectRead)
}

func (s *Service) authoringState(ctx context.Context, tx pgx.Tx, scope authz.Scope) (AuthoringState, error) {
	var zero AuthoringState
	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || auth.ProjectID == nil {
		return zero, oops.C(oops.CodeUnauthorized)
	}

	if err := s.requireAccess(ctx, auth.ActiveOrganizationID, authz.Check{Scope: scope, ResourceKind: "", ResourceID: auth.ProjectID.String(), Dimensions: nil}); err != nil {
		return zero, err
	}

	q := repo.New(tx)

	if err := q.LockSigintProject(ctx, auth.ProjectID.String()); err != nil {
		return zero, fmt.Errorf("lock authoring state: %w", err)
	}

	sensors, err := q.ListSensors(ctx, repo.ListSensorsParams{ProjectID: *auth.ProjectID, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: maxAuthoringResources + 1})
	if err != nil {
		return zero, fmt.Errorf("read authoring sensors: %w", err)
	}

	signals, err := q.ListSignals(ctx, repo.ListSignalsParams{ProjectID: *auth.ProjectID, Cursor: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, LimitValue: maxAuthoringResources + 1})
	if err != nil {
		return zero, fmt.Errorf("read authoring signals: %w", err)
	}

	if len(sensors) > maxAuthoringResources || len(signals) > maxAuthoringResources {
		return zero, oops.E(oops.CodeBadRequest, nil, "project exceeds bounded authoring snapshot limit")
	}
	state := AuthoringState{Version: "", Sensors: mv.BuildSigintSensorListView(sensors), Signals: mv.BuildSigintSignalListView(signals)}
	// Use database timestamps at full precision; API timestamps are second-granular.
	versions := [][]string{{auth.ActiveOrganizationID, auth.ProjectID.String()}}
	for _, sensor := range sensors {
		row := []string{"sensor", sensor.ID.String(), sensor.UpdatedAt.Time.Format(time.RFC3339Nano)}
		for _, id := range sensor.SignalIds {
			row = append(row, id.String())
		}
		versions = append(versions, row)
	}
	for _, signal := range signals {
		versions = append(versions, []string{"signal", signal.ID.String(), signal.UpdatedAt.Time.Format(time.RFC3339Nano)})
	}

	data, err := json.Marshal(versions)
	if err != nil {
		return zero, fmt.Errorf("encode authoring state: %w", err)
	}

	hash := sha256.Sum256(data)
	state.Version = hex.EncodeToString(hash[:])
	return state, nil
}

// PreviewAuthoring validates through the actual mutation path in a rolled-back
// transaction, including slug conflicts, memberships, and per-row audit writes.
// Returned creation IDs are provisional and must never be used as committed IDs.
func (s *Service) PreviewAuthoring(ctx context.Context, input AuthoringInput) (AuthoringResult, string, error) {
	var zero AuthoringResult

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, "", fmt.Errorf("begin authoring preview: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	before, err := s.authoringState(ctx, tx, authz.ScopeProjectWrite)
	if err != nil {
		return zero, "", err
	}

	result, err := s.AuthorInTransaction(ctx, tx, input, before.Version)
	return result, before.Version, err
}

// AuthorInTransaction atomically applies a version-bound operation in the caller's
// receipt transaction. The caller owns final commit or rollback. Existing service
// mutations use nested savepoints, preserving authorization and audit behavior.
func (s *Service) AuthorInTransaction(ctx context.Context, tx pgx.Tx, input AuthoringInput, expectedVersion string) (AuthoringResult, error) {
	var result AuthoringResult

	before, err := s.authoringState(ctx, tx, authz.ScopeProjectWrite)
	if err != nil {
		return result, err
	}

	if expectedVersion == "" || expectedVersion != before.Version {
		return result, oops.E(oops.CodeConflict, nil, "configuration changed; preview again")
	}

	if err := validateAuthoringInput(input); err != nil {
		return result, err
	}

	bound := *s
	bound.db = tx
	var ids []string
	if input.Signals != nil {
		ids = make([]string, 0, len(*input.Signals))
		for _, ref := range *input.Signals {
			id := ref.ID
			if ref.New != nil {
				created, err := bound.CreateSignal(ctx, &gen.CreateSignalPayload{Name: ref.New.Name, Slug: slugPointer(ref.New.Slug), Description: ref.New.Description, ClassifierCriteria: ref.New.Criteria, SessionToken: nil, ProjectSlugInput: nil, ApikeyToken: nil})
				if err != nil {
					return result, err
				}

				id = created.ID
			}
			ids = append(ids, id)
		}
	}
	switch input.Operation {
	case "create_signal":
		result.Signal, err = bound.CreateSignal(ctx, &gen.CreateSignalPayload{Name: *input.Name, Slug: slugPointer(input.Slug), Description: input.Description, ClassifierCriteria: input.Criteria, SessionToken: nil, ProjectSlugInput: nil, ApikeyToken: nil})
	case "update_signal":
		result.Signal, err = bound.UpdateSignal(ctx, &gen.UpdateSignalPayload{ID: input.ID, Name: input.Name, Slug: slugPointer(input.Slug), Description: input.Description, ClassifierCriteria: input.Criteria, SessionToken: nil, ProjectSlugInput: nil, ApikeyToken: nil})
	case "create_sensor":
		result.Sensor, err = bound.CreateSensor(ctx, &gen.CreateSensorPayload{Name: *input.Name, Slug: slugPointer(input.Slug), Description: input.Description, Instructions: input.Instructions, Mode: types.SigintSensorMode(*input.Mode), MatchExpression: input.MatchExpression, SignalIds: ids, SessionToken: nil, ProjectSlugInput: nil, ApikeyToken: nil})
	case "update_sensor":
		var mode *types.SigintSensorMode
		if input.Mode != nil {
			mode = new(types.SigintSensorMode(*input.Mode))
		}
		result.Sensor, err = bound.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: input.ID, Name: input.Name, Slug: slugPointer(input.Slug), Description: input.Description, Instructions: input.Instructions, Mode: mode, MatchExpression: input.MatchExpression, SignalIds: ids, SessionToken: nil, ProjectSlugInput: nil, ApikeyToken: nil})
	}
	if err != nil {
		return result, err
	}
	result.State, err = s.authoringState(ctx, tx, authz.ScopeProjectWrite)
	return result, err
}

func slugPointer(value *string) *types.Slug {
	if value == nil {
		return nil
	}
	return new(types.Slug(*value))
}

func validateAuthoringInput(input AuthoringInput) error {
	invalid := func() error { return oops.E(oops.CodeBadRequest, nil, "invalid sensor authoring proposal") }
	switch input.Operation {
	case "create_signal", "create_sensor":
		if input.ID != "" || input.Name == nil {
			return invalid()
		}
	case "update_signal", "update_sensor":
		if input.ID == "" {
			return invalid()
		}
	default:
		return invalid()
	}
	if input.Operation == "create_sensor" && input.Mode == nil {
		return invalid()
	}
	if input.Operation == "create_signal" || input.Operation == "update_signal" {
		if input.Signals != nil || input.Instructions != nil || input.Mode != nil || input.MatchExpression != nil {
			return invalid()
		}
	} else if input.Criteria != nil {
		return invalid()
	}
	if input.Signals != nil {
		if len(*input.Signals) > maxBundleSignals {
			return invalid()
		}
		for _, ref := range *input.Signals {
			if (ref.ID == "") == (ref.New == nil) {
				return invalid()
			}
		}
	}
	return nil
}
