package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
)

// maxSignalProposalBytes bounds author-controlled preview and receipt hashing work.
const maxSignalProposalBytes = 64 << 10 // 64 KiB

// SignalAuthoringService composes the authorized owning service with Platform
// receipt and exact-project controls. External users are the only admitted actors.
type SignalAuthoringService struct {
	management *sigint.Service
	projects   *PostgresReader
	engine     *authz.Engine
	db         *pgxpool.Pool
	key        []byte
}

// NewSignalAuthoringService retains the normal management authorization and audit path.
func NewSignalAuthoringService(management *sigint.Service, projects *PostgresReader, engine *authz.Engine, key string) *SignalAuthoringService {
	return &SignalAuthoringService{management: management, projects: projects, engine: engine, db: projects.db, key: []byte(key)}
}

type signalConfiguration struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	Criteria    *string `json:"classifier_criteria,omitempty"`
	SensorCount int     `json:"sensor_count"`
}

type sensorConfiguration struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Slug            string                `json:"slug"`
	Description     *string               `json:"description,omitempty"`
	Instructions    *string               `json:"instructions,omitempty"`
	Mode            string                `json:"mode"`
	MatchExpression string                `json:"match_expression"`
	Signals         []signalConfiguration `json:"signals"`
	Ready           bool                  `json:"configuration_ready"`
	Issues          []string              `json:"configuration_issues"`
}

type signalAuthoringInput struct {
	ProjectID       string                `json:"project_id" jsonschema:"exact project UUID"`
	Proposal        sigint.AuthoringInput `json:"proposal" jsonschema:"complete desired proposal; operation must match the tool name"`
	Confirmed       bool                  `json:"confirmed" jsonschema:"false previews without persisting; true only after explicit confirmation of the returned preview"`
	ExpectedVersion string                `json:"expected_version,omitempty" jsonschema:"project configuration version returned by the preview"`
	PreviewToken    string                `json:"preview_token,omitempty" jsonschema:"token binding the preview to this exact proposal, actor and project"`
	IdempotencyKey  string                `json:"idempotency_key,omitempty" jsonschema:"required for confirmation; reuse only for retries of this exact confirmed request"`
}

type signalAuthoringOutput struct {
	ProjectID       string               `json:"project_id"`
	Preview         bool                 `json:"preview"`
	Version         string               `json:"version"`
	PreviewToken    string               `json:"preview_token,omitempty"`
	Sensor          *sensorConfiguration `json:"sensor,omitempty"`
	Signal          *signalConfiguration `json:"signal,omitempty"`
	AffectedSensors []sensorImpact       `json:"affected_sensors"`
	ReceiptID       string               `json:"receipt_id,omitempty"`
	Replayed        bool                 `json:"replayed"`
	TargetAvailable bool                 `json:"target_available"`
	DashboardPath   string               `json:"dashboard_path"`
}

type sensorImpact struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// signalReceipt stores identifiers only, never authored instructions or predicates.
type signalReceipt struct {
	SensorID string `json:"sensor_id,omitempty"`
	SignalID string `json:"signal_id,omitempty"`
}

func (s *SignalAuthoringService) scope(ctx context.Context, principal Principal, projectID string, write bool) (context.Context, ResolvedProject, sigint.AuthoringState, error) {
	var project ResolvedProject
	var state sigint.AuthoringState
	if s == nil || s.management == nil || s.projects == nil || s.engine == nil || len(s.key) == 0 {
		return ctx, project, state, ErrUnavailable
	}

	id, err := uuid.Parse(projectID)
	if err != nil || id == uuid.Nil {
		return ctx, project, state, oops.C(oops.CodeBadRequest)
	}

	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || principal.UserID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID || principal.surface() != SurfacePlatformMCP {
		return ctx, project, state, ErrForbidden
	}

	project, err = s.projects.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: id.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return ctx, project, state, err
	}

	if project.ID != id {
		return ctx, project, state, ErrForbidden
	}
	scoped := *auth

	scoped.OrganizationSlug, err = NewPostgresOrganizationSlugResolver(s.db).OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil {
		return ctx, project, state, err
	}

	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, &scoped)
	if write {
		if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
			return ctx, project, state, err
		}
	}

	// This independently checks entitlement and project read access, including
	// before receipt replay. The mutation rechecks write access under its lock.
	state, err = s.management.ReadAuthoringState(ctx)
	if err != nil {
		return ctx, project, state, fmt.Errorf("read signal authoring state: %w", err)
	}

	return ctx, project, state, nil
}

func (s *SignalAuthoringService) previewToken(principal Principal, project ResolvedProject, version string, proposal sigint.AuthoringInput) (string, error) {
	data, err := json.Marshal(struct {
		Organization string                `json:"organization"`
		User         string                `json:"user"`
		Project      string                `json:"project"`
		Version      string                `json:"version"`
		Proposal     sigint.AuthoringInput `json:"proposal"`
	}{Organization: principal.OrganizationID, User: principal.UserID, Project: project.ID.String(), Version: version, Proposal: proposal})
	if err != nil {
		return "", fmt.Errorf("encode sensor proposal: %w", err)
	}

	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("platform-signal-authoring-v1\x00"))
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (s *SignalAuthoringService) author(ctx context.Context, principal Principal, operation string, input signalAuthoringInput) (signalAuthoringOutput, error) {
	var zero signalAuthoringOutput

	if input.Proposal.Operation != "" && input.Proposal.Operation != operation {
		return zero, oops.C(oops.CodeBadRequest)
	}

	input.Proposal.Operation = operation

	encoded, err := json.Marshal(input.Proposal)
	if err != nil || len(encoded) > maxSignalProposalBytes {
		return zero, oops.C(oops.CodeBadRequest)
	}

	ctx, project, _, err := s.scope(ctx, principal, input.ProjectID, true)
	if err != nil {
		return zero, err
	}

	if !input.Confirmed {
		result, version, err := s.management.PreviewAuthoring(ctx, input.Proposal)
		if err != nil {
			return zero, fmt.Errorf("preview signal authoring: %w", err)
		}

		out := projectAuthoringResult(ctx, project, result)
		out.Preview, out.Version = true, version
		out.PreviewToken, err = s.previewToken(principal, project, version, input.Proposal)
		return out, err
	}

	if input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 || input.ExpectedVersion == "" {
		return zero, oops.C(oops.CodeBadRequest)
	}

	token, err := s.previewToken(principal, project, input.ExpectedVersion, input.Proposal)
	if err != nil {
		return zero, err
	}

	if !hmac.Equal([]byte(token), []byte(input.PreviewToken)) {
		return zero, oops.E(oops.CodeConflict, nil, "preview does not match proposal")
	}

	// The token already binds actor, scope, version and normalized operation.
	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[signalReceipt]{
		DB: s.db, Now: time.Now, Principal: principal, Project: project, Operation: operation, IdempotencyKey: input.IdempotencyKey, InputHash: token, Label: "sensor authoring",
		Invalid:     func(err error) error { return oops.E(oops.CodeBadRequest, err, "invalid authoring receipt") },
		Conflict:    func(message string) error { return oops.E(oops.CodeConflict, nil, "%s", message) },
		Unavailable: func(err error) error { return fmt.Errorf("%w: %w", ErrUnavailable, err) },
		ValidateReplay: func(data []byte) bool {
			var value signalReceipt
			return json.Unmarshal(data, &value) == nil && validSignalReceipt(value)
		},
		EncodeResult: func(value signalReceipt) ([]byte, error) { return json.Marshal(value) },
		Mutate: func(ctx context.Context, tx pgx.Tx) (signalReceipt, error) {
			result, err := s.management.AuthorInTransaction(ctx, tx, input.Proposal, input.ExpectedVersion)
			if err != nil {
				return signalReceipt{}, fmt.Errorf("apply signal authoring: %w", err)
			}

			var value signalReceipt
			if result.Sensor != nil {
				value.SensorID = result.Sensor.ID
			}
			if result.Signal != nil {
				value.SignalID = result.Signal.ID
			}
			return value, nil
		},
	})
	if err != nil {
		return zero, err
	}

	var target signalReceipt

	if err := json.Unmarshal(receipt.ResultPayload, &target); err != nil {
		return zero, ErrUnavailable
	}

	state, err := s.management.ReadAuthoringState(ctx)
	if err != nil {
		return zero, fmt.Errorf("verify signal authoring: %w", err)
	}

	result := sigint.AuthoringResult{State: state, Sensor: nil, Signal: nil}

	for _, sensor := range state.Sensors {
		if sensor.ID == target.SensorID {
			result.Sensor = sensor
		}
	}

	for _, signal := range state.Signals {
		if signal.ID == target.SignalID {
			result.Signal = signal
		}
	}

	out := projectAuthoringResult(ctx, project, result)
	out.ReceiptID, out.Replayed = receipt.ID.String(), receipt.Replayed
	return out, nil
}

func validSignalReceipt(value signalReceipt) bool {
	if (value.SensorID == "") == (value.SignalID == "") {
		return false
	}
	id := value.SensorID
	if id == "" {
		id = value.SignalID
	}
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil
}

func signalProjections(state sigint.AuthoringState) map[string]signalConfiguration {
	counts := make(map[string]int, len(state.Signals))
	for _, sensor := range state.Sensors {
		for _, id := range sensor.SignalIds {
			counts[id]++
		}
	}
	result := make(map[string]signalConfiguration, len(state.Signals))
	for _, signal := range state.Signals {
		result[signal.ID] = signalConfiguration{ID: signal.ID, Name: signal.Name, Slug: string(signal.Slug), Description: signal.Description, Criteria: signal.ClassifierCriteria, SensorCount: counts[signal.ID]}
	}
	return result
}

func projectSensor(sensor *types.SigintSensor, signals map[string]signalConfiguration) sensorConfiguration {
	result := sensorConfiguration{ID: sensor.ID, Name: sensor.Name, Slug: string(sensor.Slug), Description: sensor.Description, Instructions: sensor.Instructions, Mode: string(sensor.Mode), MatchExpression: sensor.MatchExpression, Signals: []signalConfiguration{}, Ready: true, Issues: []string{}}
	for _, id := range sensor.SignalIds {
		if signal, ok := signals[id]; ok {
			result.Signals = append(result.Signals, signal)
		}
	}
	if sensor.Instructions == nil {
		result.Issues = append(result.Issues, "missing_instructions")
	}
	if len(result.Signals) == 0 {
		result.Issues = append(result.Issues, "missing_signals")
	}
	if len(result.Signals) != len(sensor.SignalIds) {
		result.Issues = append(result.Issues, "missing_signal_definition")
	}
	if sensor.Mode == "exclusive" && len(result.Signals) > 255 {
		result.Issues = append(result.Issues, "too_many_options")
	}
	if sensor.Mode == "ordered_score" {
		if len(result.Signals) < 2 || len(result.Signals) > 10 {
			result.Issues = append(result.Issues, "ordered_score_requires_2_to_10_levels")
		}
		for _, signal := range result.Signals {
			if signal.Criteria == nil {
				result.Issues = append(result.Issues, "ordered_score_requires_level_criteria")
				break
			}
		}
	}

	if err := matching.Validate(sensor.MatchExpression); err != nil {
		result.Issues = append(result.Issues, "invalid_match_expression")
	}

	result.Ready = len(result.Issues) == 0
	return result
}

func projectAuthoringResult(ctx context.Context, project ResolvedProject, result sigint.AuthoringResult) signalAuthoringOutput {
	var out signalAuthoringOutput
	signals := signalProjections(result.State)
	out.ProjectID, out.Version = project.ID.String(), result.State.Version
	out.AffectedSensors = []sensorImpact{}

	if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil && auth.OrganizationSlug != "" {
		out.DashboardPath = "/" + auth.OrganizationSlug + "/projects/" + project.Slug + "/signals-intelligence"
	}

	if result.Sensor != nil {
		value := projectSensor(result.Sensor, signals)
		out.Sensor = &value
	}
	if result.Signal != nil {
		value := signals[result.Signal.ID]
		out.Signal = &value
		for _, sensor := range result.State.Sensors {
			if slices.Contains(sensor.SignalIds, result.Signal.ID) {
				out.AffectedSensors = append(out.AffectedSensors, sensorImpact{ID: sensor.ID, Name: sensor.Name, Slug: string(sensor.Slug)})
			}
		}
	}
	out.TargetAvailable = out.Sensor != nil || out.Signal != nil
	return out
}

// signalToolError keeps internal query errors and authored content out of refusals.
func signalToolError(err error) (*mcp.CallToolResult, bool) {
	if result, ok := externalAuthorizationToolResult(err); ok {
		return result, true
	}

	code, message := "unavailable", "Signals intelligence authoring is unavailable."

	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			code, message = "invalid_request", "Check the bounded proposal, exact target, signal references, mode and boolean matching expression."
		case oops.CodeConflict:
			code, message = "conflict", "The proposal conflicts with current configuration or the idempotency key. Read and preview again; do not change input when retrying a committed request."
		case oops.CodeForbidden, oops.CodeUnauthorized:
			code, message = "permission_denied", "Signals intelligence entitlement and the required project permission are needed."
		case oops.CodeNotFound:
			code, message = "not_found", "The requested configuration is not available."
		default:
			// Retain the bounded unavailable result for internal failures.
		}
	} else if errors.Is(err, ErrForbidden) {
		code, message = "permission_denied", "An authorized external user and exact project are required."
	}

	data, _ := json.Marshal(featureUnavailableResult{Code: code, Feature: "signals_intelligence", Message: message})
	return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(data)}}, IsError: true}, true
}
