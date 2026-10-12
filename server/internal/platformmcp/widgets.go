package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	genwidgets "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

const (
	// widgetsFeature names the capability in refusals.
	widgetsFeature = "widgets"

	operationCreateWidget    = "create_widget"
	operationUpdateWidget    = "update_widget"
	operationDuplicateWidget = "duplicate_widget"
	operationDeleteWidget    = "delete_widget"

	// newWidgetVersion is the version a create preview returns and its
	// confirmation names: a widget that does not exist yet has no updated_at.
	newWidgetVersion = "new"

	// maxWidgetProposalBytes bounds the query and visualization a caller can
	// send, and with them the preview token's hashing work. A real widget is
	// a few hundred bytes.
	maxWidgetProposalBytes = 64 << 10 // 64 KiB

	// maxListedWidgets bounds list_widgets. A project holds far fewer widgets
	// than this; past it the result says it was cut short.
	maxListedWidgets = 200

	widgetSnapshotPreview                 = "preview"
	widgetSnapshotCurrent                 = "current"
	widgetSnapshotDeleted                 = "deleted"
	widgetSnapshotVerificationUnavailable = "verification_unavailable"
)

// ErrWidgetsNotEnabled marks a call for an organization that has Explore
// switched off.
var ErrWidgetsNotEnabled = errors.New("platform mcp widgets not enabled")

// WidgetToolError is safe to return to the caller: its message says what to
// do next and never carries database or driver detail.
type WidgetToolError struct {
	Code    string
	Field   string
	Message string
	Cause   error
}

func (e *WidgetToolError) Error() string { return e.Message }
func (e *WidgetToolError) Unwrap() error { return e.Cause }

// widgetManagement is the widgets service as the tools use it: every read
// and write goes through its authorization, validation and audit.
type widgetManagement interface {
	ReadAll(ctx context.Context) ([]widgets.Snapshot, error)
	Read(ctx context.Context, id string) (widgets.Snapshot, error)
	Check(ctx context.Context, input widgets.Input) error
	CreateInTx(ctx context.Context, tx pgx.Tx, input widgets.Input) (widgets.Snapshot, error)
	UpdateInTx(ctx context.Context, tx pgx.Tx, id string, input widgets.Input, expectedVersion string) (widgets.Snapshot, error)
	DuplicateInTx(ctx context.Context, tx pgx.Tx, id string, expectedVersion string) (widgets.Snapshot, error)
	DeleteInTx(ctx context.Context, tx pgx.Tx, id string, expectedVersion string) ([]*genwidgets.WidgetDashboard, error)
}

// WidgetService lets an external user read and manage a project's Explore
// widgets. It checks the exact project and the Explore flag, then calls the
// widgets service, whose authorization, validation and audit stay the single
// source of truth. Writes run inside a Platform MCP receipt transaction.
type WidgetService struct {
	logger        *slog.Logger
	widgets       widgetManagement
	projects      *PostgresReader
	engine        *authz.Engine
	flags         feature.Provider
	organizations OrganizationSlugResolver
	db            *pgxpool.Pool
	key           []byte
	now           func() time.Time
}

// NewWidgetService returns nil when a dependency is missing, so the tools are
// served as stable refusals.
func NewWidgetService(logger *slog.Logger, management *widgets.Service, projects *PostgresReader, engine *authz.Engine, flags feature.Provider, key string) *WidgetService {
	if logger == nil || management == nil || projects == nil || projects.db == nil || engine == nil || flags == nil || key == "" {
		return nil
	}
	return &WidgetService{
		logger:        logger,
		widgets:       management,
		projects:      projects,
		engine:        engine,
		flags:         flags,
		organizations: NewPostgresOrganizationSlugResolver(projects.db),
		db:            projects.db,
		key:           []byte(key),
		now:           time.Now,
	}
}

func (s *WidgetService) valid() bool {
	return s != nil && s.logger != nil && s.widgets != nil && s.projects != nil && s.engine != nil && s.flags != nil && s.organizations != nil && s.db != nil && len(s.key) > 0 && s.now != nil
}

// WithWidgets attaches the widget tools. A nil service leaves them served as
// stable refusals.
func (r *PostgresReader) WithWidgets(service *WidgetService) *PostgresReader {
	if r != nil && service.valid() {
		r.widgets = service
	}
	return r
}

// WidgetDashboardRef is a dashboard a widget is placed on.
type WidgetDashboardRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WidgetSummary is a widget as list_widgets shows it.
type WidgetSummary struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Dataset     string  `json:"dataset"`
	// ChartType is the visualization's type, when it names one.
	ChartType string `json:"chart_type,omitempty"`
	// CreatedByUserID is who saved the widget, when known.
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	UpdatedAt       string  `json:"updated_at"`
	// Version is what a write to this widget names as expected_version.
	Version string `json:"version"`
	// InvalidReason is present when the widget no longer works.
	InvalidReason *string              `json:"invalid_reason,omitempty"`
	Dashboards    []WidgetDashboardRef `json:"dashboards"`
}

// WidgetDefinition is a widget's full definition.
type WidgetDefinition struct {
	ID              string               `json:"id,omitempty"`
	Name            string               `json:"name"`
	Description     *string              `json:"description,omitempty"`
	Dataset         string               `json:"dataset"`
	Query           map[string]any       `json:"query"`
	Visualization   map[string]any       `json:"visualization"`
	CreatedByUserID *string              `json:"created_by_user_id,omitempty"`
	CreatedAt       string               `json:"created_at,omitempty"`
	UpdatedAt       string               `json:"updated_at,omitempty"`
	Version         string               `json:"version,omitempty"`
	InvalidReason   *string              `json:"invalid_reason,omitempty"`
	Dashboards      []WidgetDashboardRef `json:"dashboards"`
}

type ListWidgetsInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
}

type ListWidgetsOutput struct {
	Project AnalyticsProject `json:"project"`
	Widgets []WidgetSummary  `json:"widgets"`
	// Truncated says the project holds more widgets than were returned.
	Truncated bool `json:"truncated"`
}

type GetWidgetInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	WidgetID  string `json:"widget_id" jsonschema:"exact widget UUID from list_widgets"`
}

type GetWidgetOutput struct {
	Project AnalyticsProject `json:"project"`
	Widget  WidgetDefinition `json:"widget"`
}

// SaveWidgetInput creates a widget when widget_id is omitted and replaces
// every field of an existing one when it is given.
type SaveWidgetInput struct {
	ProjectID       string         `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	WidgetID        string         `json:"widget_id,omitempty" jsonschema:"widget to replace, from list_widgets; omit to create a new widget"`
	Name            string         `json:"name" jsonschema:"display name, 1 to 200 characters"`
	Description     *string        `json:"description,omitempty" jsonschema:"what the widget is for, at most 2000 characters"`
	Dataset         string         `json:"dataset" jsonschema:"dataset the widget asks, by name from describe_analytics_catalog"`
	Query           map[string]any `json:"query" jsonschema:"the question in run_analytics_query's vocabulary: dimensions, measures, filters, grain, order_by, limit and the window"`
	Visualization   map[string]any `json:"visualization" jsonschema:"how the question is drawn: an object whose type names the chart, plus its options"`
	Confirmed       bool           `json:"confirmed" jsonschema:"false validates and previews without saving; true only after the user confirmed the returned preview"`
	ExpectedVersion string         `json:"expected_version,omitempty" jsonschema:"version the preview returned; required to confirm"`
	PreviewToken    string         `json:"preview_token,omitempty" jsonschema:"token the preview returned; binds the confirmation to this exact proposal"`
	IdempotencyKey  string         `json:"idempotency_key,omitempty" jsonschema:"required to confirm; reuse it only to retry this exact confirmed request"`
}

// WidgetTargetInput names one existing widget to duplicate or delete.
type WidgetTargetInput struct {
	ProjectID       string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	WidgetID        string `json:"widget_id" jsonschema:"exact widget UUID from list_widgets"`
	Confirmed       bool   `json:"confirmed" jsonschema:"false previews without changing anything; true only after the user confirmed the returned preview"`
	ExpectedVersion string `json:"expected_version,omitempty" jsonschema:"version the preview returned; required to confirm"`
	PreviewToken    string `json:"preview_token,omitempty" jsonschema:"token the preview returned; binds the confirmation to this exact widget"`
	IdempotencyKey  string `json:"idempotency_key,omitempty" jsonschema:"required to confirm; reuse it only to retry this exact confirmed request"`
}

// WidgetWriteOutput is a preview or a committed write.
type WidgetWriteOutput struct {
	Project AnalyticsProject `json:"project"`
	// Operation is create_widget, update_widget, duplicate_widget or
	// delete_widget.
	Operation string `json:"operation"`
	Preview   bool   `json:"preview"`
	// Version is what a preview's confirmation names as expected_version, or
	// the written widget's new version after a commit.
	Version      string `json:"version,omitempty"`
	PreviewToken string `json:"preview_token,omitempty"`
	// Widget is the proposed widget in a save preview, the widget the call
	// acts on in a duplicate or delete preview, and a fresh read after a
	// committed save or duplicate.
	Widget *WidgetDefinition `json:"widget,omitempty"`
	// Current is the widget as it stands, in an update preview.
	Current *WidgetDefinition `json:"current,omitempty"`
	// Dashboards are the dashboards an update reaches, or the ones a delete
	// takes the widget off.
	Dashboards []WidgetDashboardRef `json:"dashboards"`
	ReceiptID  string               `json:"receipt_id,omitempty"`
	Replayed   bool                 `json:"replayed"`
	// SnapshotScope is preview, current (a fresh read after the commit),
	// deleted (a fresh read found the widget gone), or verification_unavailable
	// (the write committed but the fresh read failed).
	SnapshotScope string `json:"snapshot_scope"`
}

// widgetReceipt is what a receipt stores: identifiers and dashboard names,
// never the query.
type widgetReceipt struct {
	WidgetID    string               `json:"widget_id"`
	RemovedFrom []WidgetDashboardRef `json:"removed_from,omitempty"`
}

// List returns the project's widgets, most recently updated first.
func (s *WidgetService) List(ctx context.Context, principal Principal, input ListWidgetsInput) (ListWidgetsOutput, error) {
	ctx, project, err := s.scope(ctx, principal, input.ProjectID, false)
	if err != nil {
		return ListWidgetsOutput{}, s.boundaryError(ctx, "list", err)
	}
	snapshots, err := s.widgets.ReadAll(ctx)
	if err != nil {
		return ListWidgetsOutput{}, s.boundaryError(ctx, "list", err)
	}
	out := ListWidgetsOutput{Project: analyticsProject(project), Widgets: make([]WidgetSummary, 0, min(len(snapshots), maxListedWidgets)), Truncated: len(snapshots) > maxListedWidgets}
	for _, snapshot := range snapshots[:min(len(snapshots), maxListedWidgets)] {
		out.Widgets = append(out.Widgets, widgetSummary(snapshot))
	}
	return out, nil
}

// Get returns one widget's full definition.
func (s *WidgetService) Get(ctx context.Context, principal Principal, input GetWidgetInput) (GetWidgetOutput, error) {
	if err := validateWidgetID(input.WidgetID); err != nil {
		return GetWidgetOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID, false)
	if err != nil {
		return GetWidgetOutput{}, s.boundaryError(ctx, "get", err)
	}
	snapshot, err := s.widgets.Read(ctx, input.WidgetID)
	if err != nil {
		return GetWidgetOutput{}, s.boundaryError(ctx, "get", err)
	}
	return GetWidgetOutput{Project: analyticsProject(project), Widget: widgetDefinition(snapshot)}, nil
}

// Save previews or commits a create (no widget_id) or a full update.
func (s *WidgetService) Save(ctx context.Context, principal Principal, input SaveWidgetInput) (WidgetWriteOutput, error) {
	operation := operationCreateWidget
	if input.WidgetID != "" {
		operation = operationUpdateWidget
	}
	output, err := s.save(ctx, principal, operation, input)
	return output, s.boundaryError(ctx, operation, err)
}

// widgetTokenProposal is what a preview token binds besides the actor,
// project, operation and version: the target widget and, for a save, the
// whole proposed widget.
type widgetTokenProposal struct {
	WidgetID string         `json:"widget_id"`
	Widget   *widgets.Input `json:"widget,omitempty"`
}

func (s *WidgetService) save(ctx context.Context, principal Principal, operation string, input SaveWidgetInput) (WidgetWriteOutput, error) {
	proposal, err := validateSaveWidget(input)
	if err != nil {
		return WidgetWriteOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID, true)
	if err != nil {
		return WidgetWriteOutput{}, err
	}
	out := newWidgetWriteOutput(project, operation)
	token := widgetTokenProposal{WidgetID: input.WidgetID, Widget: &proposal}

	if !input.Confirmed {
		if err := s.widgets.Check(ctx, proposal); err != nil {
			return out, fmt.Errorf("check widget: %w", err)
		}
		proposed := WidgetDefinition{ID: input.WidgetID, Name: proposal.Name, Description: proposal.Description, Dataset: proposal.Dataset, Query: proposal.Query, Visualization: proposal.Visualization, CreatedByUserID: nil, CreatedAt: "", UpdatedAt: "", Version: "", InvalidReason: nil, Dashboards: []WidgetDashboardRef{}}
		out.Version = newWidgetVersion
		if operation == operationUpdateWidget {
			current, err := s.widgets.Read(ctx, input.WidgetID)
			if err != nil {
				return out, fmt.Errorf("read widget to update: %w", err)
			}
			definition := widgetDefinition(current)
			out.Current, out.Version, out.Dashboards = &definition, current.Version, definition.Dashboards
			proposed.Dashboards = definition.Dashboards
		}
		out.Widget = &proposed
		return s.preview(principal, project, out, token)
	}

	receipt, err := s.confirm(ctx, principal, project, operation, confirmation{Version: input.ExpectedVersion, Token: input.PreviewToken, Key: input.IdempotencyKey}, token, func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
		var snapshot widgets.Snapshot
		var err error
		if operation == operationCreateWidget {
			snapshot, err = s.widgets.CreateInTx(ctx, tx, proposal)
		} else {
			snapshot, err = s.widgets.UpdateInTx(ctx, tx, input.WidgetID, proposal, input.ExpectedVersion)
		}
		if err != nil {
			return widgetReceipt{}, fmt.Errorf("save widget: %w", err)
		}
		return widgetReceipt{WidgetID: snapshot.Widget.ID, RemovedFrom: nil}, nil
	})
	if err != nil {
		return out, err
	}
	return s.finish(ctx, out, receipt)
}

// Duplicate previews or commits a copy of a widget the caller then owns.
func (s *WidgetService) Duplicate(ctx context.Context, principal Principal, input WidgetTargetInput) (WidgetWriteOutput, error) {
	output, err := s.target(ctx, principal, operationDuplicateWidget, input, func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
		snapshot, err := s.widgets.DuplicateInTx(ctx, tx, input.WidgetID, input.ExpectedVersion)
		if err != nil {
			return widgetReceipt{}, fmt.Errorf("duplicate widget: %w", err)
		}
		return widgetReceipt{WidgetID: snapshot.Widget.ID, RemovedFrom: nil}, nil
	})
	return output, s.boundaryError(ctx, operationDuplicateWidget, err)
}

// Delete previews or commits deleting a widget, which takes it off every
// dashboard it is on.
func (s *WidgetService) Delete(ctx context.Context, principal Principal, input WidgetTargetInput) (WidgetWriteOutput, error) {
	output, err := s.target(ctx, principal, operationDeleteWidget, input, func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
		removed, err := s.widgets.DeleteInTx(ctx, tx, input.WidgetID, input.ExpectedVersion)
		if err != nil {
			return widgetReceipt{}, fmt.Errorf("delete widget: %w", err)
		}
		return widgetReceipt{WidgetID: input.WidgetID, RemovedFrom: dashboardRefs(removed)}, nil
	})
	return output, s.boundaryError(ctx, operationDeleteWidget, err)
}

// target previews or commits a write to one existing widget: a duplicate or
// a delete. The preview shows the widget as it stands and its version.
func (s *WidgetService) target(ctx context.Context, principal Principal, operation string, input WidgetTargetInput, mutate func(context.Context, pgx.Tx) (widgetReceipt, error)) (WidgetWriteOutput, error) {
	if err := validateWidgetID(input.WidgetID); err != nil {
		return WidgetWriteOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID, true)
	if err != nil {
		return WidgetWriteOutput{}, err
	}
	out := newWidgetWriteOutput(project, operation)
	token := widgetTokenProposal{WidgetID: input.WidgetID, Widget: nil}

	if !input.Confirmed {
		current, err := s.widgets.Read(ctx, input.WidgetID)
		if err != nil {
			return out, fmt.Errorf("read widget: %w", err)
		}
		if operation == operationDuplicateWidget && current.Widget.InvalidReason != nil {
			return out, &WidgetToolError{Code: "invalid_request", Field: "widget_id", Message: "This widget cannot be duplicated until it is fixed: " + *current.Widget.InvalidReason + ". Save a corrected version with save_widget first.", Cause: nil}
		}
		definition := widgetDefinition(current)
		out.Widget, out.Version = &definition, current.Version
		if operation == operationDeleteWidget {
			out.Dashboards = definition.Dashboards
		}
		return s.preview(principal, project, out, token)
	}

	receipt, err := s.confirm(ctx, principal, project, operation, confirmation{Version: input.ExpectedVersion, Token: input.PreviewToken, Key: input.IdempotencyKey}, token, mutate)
	if err != nil {
		return out, err
	}
	return s.finish(ctx, out, receipt)
}

// finish reports a committed or replayed write from a fresh read of the
// widget it wrote. A delete reports the dashboards from its receipt and is
// verified by the widget reading as gone.
func (s *WidgetService) finish(ctx context.Context, out WidgetWriteOutput, receipt OperationReceipt) (WidgetWriteOutput, error) {
	stored, err := decodeWidgetReceipt(receipt.ResultPayload)
	if err != nil {
		return out, err
	}
	out.ReceiptID, out.Replayed = receipt.ID.String(), receipt.Replayed
	deleted := out.Operation == operationDeleteWidget
	if deleted {
		out.Dashboards = append([]WidgetDashboardRef{}, stored.RemovedFrom...)
	}

	snapshot, err := s.widgets.Read(ctx, stored.WidgetID)
	shareable, isShareable := errors.AsType[*oops.ShareableError](err)
	switch {
	case err == nil:
		definition := widgetDefinition(snapshot)
		out.Widget, out.Version, out.SnapshotScope = &definition, snapshot.Version, widgetSnapshotCurrent
		if !deleted {
			out.Dashboards = definition.Dashboards
		}
	case deleted && isShareable && shareable.Code == oops.CodeNotFound:
		out.SnapshotScope = widgetSnapshotDeleted
	default:
		out.SnapshotScope = widgetSnapshotVerificationUnavailable
	}
	return out, nil
}

// scope admits a caller to one exact project: project:read, and project:write
// for a write, checked before any row is looked up so a denied caller cannot
// probe what exists; then the Explore flag. It returns a context whose auth
// names that project, which is what the widgets service authorizes against.
func (s *WidgetService) scope(ctx context.Context, principal Principal, projectID string, write bool) (context.Context, ResolvedProject, error) {
	var project ResolvedProject
	if !s.valid() {
		return ctx, project, ErrUnavailable
	}

	id, err := uuid.Parse(projectID)
	if err != nil || id == uuid.Nil {
		return ctx, project, widgetInvalid("project_id", "Name exactly one project by its ID, as list_projects returns it.")
	}

	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || principal.UserID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID || principal.surface() != SurfacePlatformMCP {
		return ctx, project, ErrForbidden
	}

	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
		return ctx, project, ErrForbidden
	}
	if write {
		if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
			return ctx, project, ErrForbidden
		}
	}

	project, err = s.projects.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: id.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return ctx, project, err
	}
	if project.ID != id {
		return ctx, project, ErrForbidden
	}

	// The Platform MCP authenticator puts the slug on the auth context; the
	// lookup covers a context that arrived without one.
	organizationSlug := auth.OrganizationSlug
	if organizationSlug == "" {
		organizationSlug, err = s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
		if err != nil {
			return ctx, project, fmt.Errorf("%w: resolve organization slug for widgets: %w", ErrUnavailable, err)
		}
	}
	enabled, err := exploreEnabled(ctx, s.flags, principal.OrganizationID, organizationSlug, project.Slug)
	if err != nil {
		return ctx, project, err
	}
	if !enabled {
		return ctx, project, ErrWidgetsNotEnabled
	}

	scoped := *auth
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.SetAuthContext(ctx, &scoped), project, nil
}

// confirmation is what a confirmed write carries back from its preview.
type confirmation struct {
	Version string
	Token   string
	Key     string
}

// preview signs a preview so its confirmation can only commit exactly what
// was shown, at exactly the version that was read.
func (s *WidgetService) preview(principal Principal, project ResolvedProject, out WidgetWriteOutput, proposal widgetTokenProposal) (WidgetWriteOutput, error) {
	token, err := s.previewToken(principal, project, out.Operation, out.Version, proposal)
	if err != nil {
		return WidgetWriteOutput{}, err
	}
	out.Preview, out.PreviewToken, out.SnapshotScope = true, token, widgetSnapshotPreview
	return out, nil
}

func (s *WidgetService) previewToken(principal Principal, project ResolvedProject, operation, version string, proposal widgetTokenProposal) (string, error) {
	data, err := json.Marshal(struct {
		Organization string              `json:"organization"`
		User         string              `json:"user"`
		Project      string              `json:"project"`
		Operation    string              `json:"operation"`
		Version      string              `json:"version"`
		Proposal     widgetTokenProposal `json:"proposal"`
	}{Organization: principal.OrganizationID, User: principal.UserID, Project: project.ID.String(), Operation: operation, Version: version, Proposal: proposal})
	if err != nil {
		return "", fmt.Errorf("encode widget proposal: %w", err)
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("platform-widget-authoring-v1\x00"))
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// confirm checks a confirmation against its preview and runs the write in
// the receipt's transaction, so a retry with the same key replays the stored
// result instead of writing twice.
func (s *WidgetService) confirm(ctx context.Context, principal Principal, project ResolvedProject, operation string, confirmed confirmation, proposal widgetTokenProposal, mutate func(context.Context, pgx.Tx) (widgetReceipt, error)) (OperationReceipt, error) {
	key := strings.TrimSpace(confirmed.Key)
	if key == "" || len(key) > maxIdempotencyKeyLength {
		return OperationReceipt{}, widgetInvalid("idempotency_key", "Provide a stable idempotency_key of at most 128 characters to confirm, and reuse it only to retry this exact request.")
	}
	if confirmed.Version == "" {
		return OperationReceipt{}, widgetInvalid("expected_version", "Provide expected_version exactly as the preview returned it.")
	}
	if confirmed.Token == "" {
		return OperationReceipt{}, widgetInvalid("preview_token", "Preview the change with confirmed:false first and confirm with the preview_token it returns.")
	}
	token, err := s.previewToken(principal, project, operation, confirmed.Version, proposal)
	if err != nil {
		return OperationReceipt{}, err
	}
	if !hmac.Equal([]byte(token), []byte(confirmed.Token)) {
		return OperationReceipt{}, &WidgetToolError{Code: "preview_mismatch", Field: "preview_token", Message: "The confirmation does not match its preview: the proposal, the version or the widget differs from what was previewed. Preview again and confirm what that preview shows.", Cause: nil}
	}

	// The token binds actor, project, operation, version and proposal, so it
	// is the receipt's input hash.
	return executeMutationReceipt(ctx, mutationReceiptExecution[widgetReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation, IdempotencyKey: key, InputHash: token, Label: "widget " + operation,
		Invalid: widgetInvalidCause,
		Conflict: func(message string) error {
			return &WidgetToolError{Code: "conflict", Field: "idempotency_key", Message: message + " Use a new idempotency_key for a different request.", Cause: nil}
		},
		Unavailable: func(err error) error { return fmt.Errorf("%w: %w", ErrUnavailable, err) },
		ValidateReplay: func(data []byte) bool {
			_, err := decodeWidgetReceipt(data)
			return err == nil
		},
		EncodeResult: func(value widgetReceipt) ([]byte, error) {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("encode widget receipt: %w", err)
			}
			return encoded, nil
		},
		Mutate: mutate,
	})
}

func decodeWidgetReceipt(data []byte) (widgetReceipt, error) {
	var value widgetReceipt
	if err := json.Unmarshal(data, &value); err != nil {
		return widgetReceipt{}, fmt.Errorf("%w: decode widget receipt: %w", ErrUnavailable, err)
	}
	if parsed, err := uuid.Parse(value.WidgetID); err != nil || parsed == uuid.Nil {
		return widgetReceipt{}, fmt.Errorf("%w: widget receipt names no widget", ErrUnavailable)
	}
	return value, nil
}

// boundaryError is the last step before an error reaches the caller. Named
// refusals pass through; an error from the widgets service is mapped by its
// code; anything else is logged with its cause and replaced by the generic
// unavailable refusal, so no SQL or driver text reaches the caller.
func (s *WidgetService) boundaryError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	if refusal, ok := errors.AsType[*WidgetToolError](err); ok {
		return refusal
	}
	switch {
	case errors.Is(err, ErrWidgetsNotEnabled):
		return &WidgetToolError{Code: analyticsNotEnabledCode, Field: "", Message: "Widgets are not switched on for your organization yet. They arrive with Explore; ask your Speakeasy contact to turn it on.", Cause: err}
	case errors.Is(err, ErrForbidden):
		return &WidgetToolError{Code: "permission_denied", Field: "", Message: "This needs read access to the project, and write access to change its widgets.", Cause: err}
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			// The widgets service's message names what is wrong with the
			// widget, in the words the dashboard shows.
			return &WidgetToolError{Code: "invalid_request", Field: "", Message: shareable.Error() + ". Correct it from describe_analytics_catalog rather than retrying the same request.", Cause: err}
		case oops.CodeConflict:
			if errors.Is(err, widgets.ErrVersionChanged) {
				return &WidgetToolError{Code: "version_changed", Field: "expected_version", Message: "The widget changed after it was read, so nothing was changed. Read it again, show the user what it is now, and preview the change again.", Cause: err}
			}
		case oops.CodeForbidden, oops.CodeUnauthorized:
			return &WidgetToolError{Code: "permission_denied", Field: "", Message: "Only the widget's creator, or someone with write access to the project, can change it.", Cause: err}
		case oops.CodeNotFound:
			return &WidgetToolError{Code: "not_found", Field: "widget_id", Message: "No widget with that ID exists in that project. Use an ID from list_widgets.", Cause: err}
		default:
		}
	}
	if s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "platform mcp widget "+operation+" failed", attr.SlogError(err))
	}
	return &WidgetToolError{Code: unavailableCode, Field: "", Message: "Widgets are temporarily unavailable. Try again shortly.", Cause: errors.Join(ErrUnavailable, err)}
}

func newWidgetWriteOutput(project ResolvedProject, operation string) WidgetWriteOutput {
	return WidgetWriteOutput{Project: analyticsProject(project), Operation: operation, Preview: false, Version: "", PreviewToken: "", Widget: nil, Current: nil, Dashboards: []WidgetDashboardRef{}, ReceiptID: "", Replayed: false, SnapshotScope: ""}
}

func validateSaveWidget(input SaveWidgetInput) (widgets.Input, error) {
	if input.WidgetID != "" {
		if err := validateWidgetID(input.WidgetID); err != nil {
			return widgets.Input{}, err
		}
	}
	if input.Query == nil {
		return widgets.Input{}, widgetInvalid("query", "Provide the question as an object in run_analytics_query's vocabulary.")
	}
	if input.Visualization == nil {
		return widgets.Input{}, widgetInvalid("visualization", "Provide the visualization as an object whose type names the chart.")
	}
	// The widgets service validates every field; this only bounds what is
	// hashed into the preview token before it does.
	proposal := widgets.Input{Name: input.Name, Description: input.Description, Dataset: input.Dataset, Query: input.Query, Visualization: input.Visualization}
	encoded, err := json.Marshal(proposal)
	if err != nil || len(encoded) > maxWidgetProposalBytes {
		return widgets.Input{}, widgetInvalid("query", "The widget, query and visualization included, must be JSON of at most 64 KiB.")
	}
	return proposal, nil
}

func validateWidgetID(id string) error {
	if parsed, err := uuid.Parse(id); err != nil || parsed == uuid.Nil {
		return widgetInvalid("widget_id", "Name the widget by its exact ID from list_widgets.")
	}
	return nil
}

func widgetInvalid(field, message string) *WidgetToolError {
	return &WidgetToolError{Code: "invalid_request", Field: field, Message: message, Cause: nil}
}

func widgetInvalidCause(cause error) error {
	return &WidgetToolError{Code: "invalid_request", Field: "", Message: "The request is missing its caller identity.", Cause: cause}
}

func widgetSummary(snapshot widgets.Snapshot) WidgetSummary {
	widget := snapshot.Widget
	chartType, _ := widget.Visualization["type"].(string)
	return WidgetSummary{
		ID:              widget.ID,
		Name:            widget.Name,
		Description:     widget.Description,
		Dataset:         widget.Dataset,
		ChartType:       chartType,
		CreatedByUserID: widget.CreatedByUserID,
		UpdatedAt:       widget.UpdatedAt,
		Version:         snapshot.Version,
		InvalidReason:   widget.InvalidReason,
		Dashboards:      dashboardRefs(widget.Dashboards),
	}
}

func widgetDefinition(snapshot widgets.Snapshot) WidgetDefinition {
	widget := snapshot.Widget
	return WidgetDefinition{
		ID:              widget.ID,
		Name:            widget.Name,
		Description:     widget.Description,
		Dataset:         widget.Dataset,
		Query:           widget.Query,
		Visualization:   widget.Visualization,
		CreatedByUserID: widget.CreatedByUserID,
		CreatedAt:       widget.CreatedAt,
		UpdatedAt:       widget.UpdatedAt,
		Version:         snapshot.Version,
		InvalidReason:   widget.InvalidReason,
		Dashboards:      dashboardRefs(widget.Dashboards),
	}
}

func dashboardRefs(dashboards []*genwidgets.WidgetDashboard) []WidgetDashboardRef {
	out := make([]WidgetDashboardRef, 0, len(dashboards))
	for _, dashboard := range dashboards {
		out = append(out, WidgetDashboardRef{ID: dashboard.ID, Name: dashboard.Name})
	}
	return out
}
