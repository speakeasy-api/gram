package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

const (
	widgetsFeature = "widgets"

	// newWidgetVersion stands in for a create's version: a new widget has no updated_at.
	newWidgetVersion = "new"

	// maxWidgetProposalBytes bounds what the preview token hashes; a real widget is a few hundred bytes.
	maxWidgetProposalBytes = 64 << 10 // 64 KiB

	// maxListedWidgets bounds list_widgets. A project holds far fewer widgets
	// than this; past it the result is marked truncated.
	maxListedWidgets = 200
)

// widgetOwnerOrWriteRequired refuses a change by anyone but the widget's creator or a project writer.
const widgetOwnerOrWriteRequired = "owner_or_write_required"

// widgetOperation also names the operation its receipts are stored under.
type widgetOperation string

const (
	operationCreateWidget    widgetOperation = "create_widget"
	operationUpdateWidget    widgetOperation = "update_widget"
	operationDuplicateWidget widgetOperation = "duplicate_widget"
	operationDeleteWidget    widgetOperation = "delete_widget"
)

// widgetSnapshotScope says what a write result's widget reflects.
type widgetSnapshotScope string

const (
	widgetSnapshotPreview                 widgetSnapshotScope = "preview"
	widgetSnapshotCurrent                 widgetSnapshotScope = "current"
	widgetSnapshotDeleted                 widgetSnapshotScope = "deleted"
	widgetSnapshotVerificationUnavailable widgetSnapshotScope = "verification_unavailable"
)

// widgetToolError is safe to return to the caller: its message says what to
// do next and never carries database or driver detail.
type widgetToolError struct {
	Code    string
	Field   string
	Message string
	Cause   error
}

func (e *widgetToolError) Error() string { return e.Message }
func (e *widgetToolError) Unwrap() error { return e.Cause }

// widgetManagement is the widgets service as the tools use it: every read
// and write goes through its authorization, validation and audit.
type widgetManagement interface {
	ReadRecent(ctx context.Context, limit int32) ([]widgets.Snapshot, error)
	ReadForChange(ctx context.Context, id string) (widgets.Snapshot, error)
	Read(ctx context.Context, id string) (widgets.Snapshot, error)
	Check(ctx context.Context, input widgets.Input) error
	Authorize(ctx context.Context) (widgets.Caller, error)
	AuthorizeChange(ctx context.Context, id string) (widgets.Change, error)
	CreateInTx(ctx context.Context, tx pgx.Tx, caller widgets.Caller, input widgets.Input) (widgets.Snapshot, error)
	UpdateInTx(ctx context.Context, tx pgx.Tx, change widgets.Change, input widgets.Input, expectedVersion string) (widgets.Snapshot, error)
	DuplicateInTx(ctx context.Context, tx pgx.Tx, caller widgets.Caller, id string, expectedVersion string) (widgets.Snapshot, error)
	DeleteInTx(ctx context.Context, tx pgx.Tx, change widgets.Change, expectedVersion string) ([]*genwidgets.WidgetDashboard, error)
}

// WidgetService lets an external user read and manage a project's Explore
// widgets through the widgets service, which stays the source of truth for
// authorization, validation and audit.
type WidgetService struct {
	logger        *slog.Logger
	widgets       widgetManagement
	projects      *PostgresReader
	engine        *authz.Engine
	organizations OrganizationSlugResolver
	db            *pgxpool.Pool
	key           []byte
	now           func() time.Time
}

// NewWidgetService returns nil when a dependency is missing, so the tools are
// served as stable refusals.
func NewWidgetService(logger *slog.Logger, management *widgets.Service, projects *PostgresReader, engine *authz.Engine, organizations OrganizationSlugResolver, key string) *WidgetService {
	if logger == nil || management == nil || projects == nil || projects.db == nil || engine == nil || organizations == nil || key == "" {
		return nil
	}
	return &WidgetService{
		logger:        logger,
		widgets:       management,
		projects:      projects,
		engine:        engine,
		organizations: organizations,
		db:            projects.db,
		key:           []byte(key),
		now:           time.Now,
	}
}

func (s *WidgetService) valid() bool {
	return s != nil && s.logger != nil && s.widgets != nil && s.projects != nil && s.engine != nil && s.organizations != nil && s.db != nil && len(s.key) > 0 && s.now != nil
}

// WithWidgets attaches the widget tools. A nil service leaves them served as
// stable refusals.
func (r *PostgresReader) WithWidgets(service *WidgetService) *PostgresReader {
	if r != nil && service.valid() {
		r.widgets = service
	}
	return r
}

type widgetDashboardRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type widgetSummary struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	Dataset         string  `json:"dataset"`
	ChartType       string  `json:"chart_type,omitempty"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	UpdatedAt       string  `json:"updated_at"`
	// Version is what a write to this widget names as expected_version.
	Version       string               `json:"version"`
	InvalidReason *string              `json:"invalid_reason,omitempty"`
	Dashboards    []widgetDashboardRef `json:"dashboards"`
}

type widgetDefinition struct {
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
	Dashboards      []widgetDashboardRef `json:"dashboards"`
}

type listWidgetsInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
}

type listWidgetsOutput struct {
	Project   AnalyticsProject `json:"project"`
	Widgets   []widgetSummary  `json:"widgets"`
	Truncated bool             `json:"truncated"`
}

type getWidgetInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	WidgetID  string `json:"widget_id" jsonschema:"exact widget UUID from list_widgets"`
}

type getWidgetOutput struct {
	Project AnalyticsProject `json:"project"`
	Widget  widgetDefinition `json:"widget"`
}

// saveWidgetInput creates a widget when widget_id is omitted and replaces
// every field of an existing one when it is given.
type saveWidgetInput struct {
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

type widgetTargetInput struct {
	ProjectID       string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	WidgetID        string `json:"widget_id" jsonschema:"exact widget UUID from list_widgets"`
	Confirmed       bool   `json:"confirmed" jsonschema:"false previews without changing anything; true only after the user confirmed the returned preview"`
	ExpectedVersion string `json:"expected_version,omitempty" jsonschema:"version the preview returned; required to confirm"`
	PreviewToken    string `json:"preview_token,omitempty" jsonschema:"token the preview returned; binds the confirmation to this exact widget"`
	IdempotencyKey  string `json:"idempotency_key,omitempty" jsonschema:"required to confirm; reuse it only to retry this exact confirmed request"`
}

type widgetWriteOutput struct {
	Project   AnalyticsProject `json:"project"`
	Operation widgetOperation  `json:"operation"`
	Preview   bool             `json:"preview"`
	// Version is what a preview's confirmation names as expected_version, or
	// the written widget's new version after a commit.
	Version      string `json:"version,omitempty"`
	PreviewToken string `json:"preview_token,omitempty"`
	// Widget is the proposal or target in a preview, and a fresh read after a commit.
	Widget *widgetDefinition `json:"widget,omitempty"`
	// Current is the widget as it stands, in an update preview.
	Current *widgetDefinition `json:"current,omitempty"`
	// Dashboards are the dashboards an update reaches, or the ones a delete
	// takes the widget off.
	Dashboards []widgetDashboardRef `json:"dashboards"`
	ReceiptID  string               `json:"receipt_id,omitempty"`
	Replayed   bool                 `json:"replayed"`
	// SnapshotScope verification_unavailable means the write committed but the fresh read failed.
	SnapshotScope widgetSnapshotScope `json:"snapshot_scope"`
}

// widgetReceipt is what a receipt stores: identifiers and dashboard names,
// never the query.
type widgetReceipt struct {
	WidgetID    string               `json:"widget_id"`
	RemovedFrom []widgetDashboardRef `json:"removed_from,omitempty"`
}

// List returns the project's widgets, most recently updated first.
func (s *WidgetService) List(ctx context.Context, principal Principal, input listWidgetsInput) (listWidgetsOutput, error) {
	ctx, project, err := s.scope(ctx, principal, input.ProjectID)
	if err != nil {
		return listWidgetsOutput{}, s.boundaryError(ctx, listWidgetsToolName, err)
	}
	snapshots, err := s.widgets.ReadRecent(ctx, maxListedWidgets+1)
	if err != nil {
		return listWidgetsOutput{}, s.boundaryError(ctx, listWidgetsToolName, err)
	}
	out := listWidgetsOutput{Project: analyticsProject(project), Widgets: make([]widgetSummary, 0, min(len(snapshots), maxListedWidgets)), Truncated: len(snapshots) > maxListedWidgets}
	for _, snapshot := range snapshots[:min(len(snapshots), maxListedWidgets)] {
		out.Widgets = append(out.Widgets, summarizeWidget(snapshot))
	}
	return out, nil
}

// Get returns one widget's full definition.
func (s *WidgetService) Get(ctx context.Context, principal Principal, input getWidgetInput) (getWidgetOutput, error) {
	if err := validateWidgetID(input.WidgetID); err != nil {
		return getWidgetOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID)
	if err != nil {
		return getWidgetOutput{}, s.boundaryError(ctx, getWidgetToolName, err)
	}
	snapshot, err := s.widgets.Read(ctx, input.WidgetID)
	if err != nil {
		return getWidgetOutput{}, s.boundaryError(ctx, getWidgetToolName, err)
	}
	return getWidgetOutput{Project: analyticsProject(project), Widget: defineWidget(snapshot)}, nil
}

// Save previews or commits a create (no widget_id) or a full update.
func (s *WidgetService) Save(ctx context.Context, principal Principal, input saveWidgetInput) (widgetWriteOutput, error) {
	operation := operationCreateWidget
	if input.WidgetID != "" {
		operation = operationUpdateWidget
	}
	output, err := s.save(ctx, principal, operation, input)
	return output, s.boundaryError(ctx, saveWidgetToolName, err)
}

// widgetTokenProposal is what a preview token binds besides actor, project,
// operation and version.
type widgetTokenProposal struct {
	WidgetID string         `json:"widget_id"`
	Widget   *widgets.Input `json:"widget,omitempty"`
}

func (s *WidgetService) save(ctx context.Context, principal Principal, operation widgetOperation, input saveWidgetInput) (widgetWriteOutput, error) {
	proposal, err := validateSaveWidget(input)
	if err != nil {
		return widgetWriteOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID)
	if err != nil {
		return widgetWriteOutput{}, err
	}
	out := newWidgetWriteOutput(project, operation)
	token := widgetTokenProposal{WidgetID: input.WidgetID, Widget: &proposal}

	if !input.Confirmed {
		if err := s.widgets.Check(ctx, proposal); err != nil {
			return out, fmt.Errorf("check widget: %w", err)
		}
		proposed := widgetDefinition{ID: input.WidgetID, Name: proposal.Name, Description: proposal.Description, Dataset: proposal.Dataset, Query: proposal.Query, Visualization: proposal.Visualization, CreatedByUserID: nil, CreatedAt: "", UpdatedAt: "", Version: "", InvalidReason: nil, Dashboards: []widgetDashboardRef{}}
		out.Version = newWidgetVersion
		if operation == operationUpdateWidget {
			current, err := s.widgets.ReadForChange(ctx, input.WidgetID)
			if err != nil {
				return out, fmt.Errorf("read widget to update: %w", err)
			}
			definition := defineWidget(current)
			out.Current, out.Version, out.Dashboards = &definition, current.Version, definition.Dashboards
			proposed.Dashboards = definition.Dashboards
		}
		out.Widget = &proposed
		return s.preview(principal, project, out, token)
	}

	receipt, err := s.confirm(ctx, principal, project, operation, confirmation{Version: input.ExpectedVersion, Token: input.PreviewToken, Key: input.IdempotencyKey}, token, func(ctx context.Context) (widgetMutation, error) {
		if operation == operationCreateWidget {
			caller, err := s.widgets.Authorize(ctx)
			if err != nil {
				return nil, fmt.Errorf("authorize widget creation: %w", err)
			}
			return func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
				return savedWidget(s.widgets.CreateInTx(ctx, tx, caller, proposal))
			}, nil
		}
		change, err := s.widgets.AuthorizeChange(ctx, input.WidgetID)
		if err != nil {
			return nil, fmt.Errorf("authorize widget update: %w", err)
		}
		return func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
			return savedWidget(s.widgets.UpdateInTx(ctx, tx, change, proposal, input.ExpectedVersion))
		}, nil
	})
	if err != nil {
		return out, err
	}
	return s.finish(ctx, out, receipt)
}

func savedWidget(snapshot widgets.Snapshot, err error) (widgetReceipt, error) {
	if err != nil {
		return widgetReceipt{}, fmt.Errorf("save widget: %w", err)
	}
	return widgetReceipt{WidgetID: snapshot.Widget.ID, RemovedFrom: nil}, nil
}

// Duplicate previews or commits a copy of a widget the caller then owns.
func (s *WidgetService) Duplicate(ctx context.Context, principal Principal, input widgetTargetInput) (widgetWriteOutput, error) {
	output, err := s.target(ctx, principal, operationDuplicateWidget, input, func(ctx context.Context) (widgetMutation, error) {
		caller, err := s.widgets.Authorize(ctx)
		if err != nil {
			return nil, fmt.Errorf("authorize widget duplication: %w", err)
		}
		return func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
			snapshot, err := s.widgets.DuplicateInTx(ctx, tx, caller, input.WidgetID, input.ExpectedVersion)
			if err != nil {
				return widgetReceipt{}, fmt.Errorf("duplicate widget: %w", err)
			}
			return widgetReceipt{WidgetID: snapshot.Widget.ID, RemovedFrom: nil}, nil
		}, nil
	})
	return output, s.boundaryError(ctx, duplicateWidgetToolName, err)
}

// Delete previews or commits deleting a widget, taking it off every dashboard.
func (s *WidgetService) Delete(ctx context.Context, principal Principal, input widgetTargetInput) (widgetWriteOutput, error) {
	output, err := s.target(ctx, principal, operationDeleteWidget, input, func(ctx context.Context) (widgetMutation, error) {
		change, err := s.widgets.AuthorizeChange(ctx, input.WidgetID)
		if err != nil {
			return nil, fmt.Errorf("authorize widget deletion: %w", err)
		}
		return func(ctx context.Context, tx pgx.Tx) (widgetReceipt, error) {
			removed, err := s.widgets.DeleteInTx(ctx, tx, change, input.ExpectedVersion)
			if err != nil {
				return widgetReceipt{}, fmt.Errorf("delete widget: %w", err)
			}
			return widgetReceipt{WidgetID: input.WidgetID, RemovedFrom: dashboardRefs(removed)}, nil
		}, nil
	})
	return output, s.boundaryError(ctx, deleteWidgetToolName, err)
}

// widgetMutation is a confirmed write, run inside its receipt's transaction.
type widgetMutation func(context.Context, pgx.Tx) (widgetReceipt, error)

// widgetAuthorization authorizes a confirmed write before its receipt's
// transaction opens and returns the write.
type widgetAuthorization func(context.Context) (widgetMutation, error)

// target previews or commits a duplicate or delete of one existing widget.
func (s *WidgetService) target(ctx context.Context, principal Principal, operation widgetOperation, input widgetTargetInput, authorize widgetAuthorization) (widgetWriteOutput, error) {
	if err := validateWidgetID(input.WidgetID); err != nil {
		return widgetWriteOutput{}, err
	}
	ctx, project, err := s.scope(ctx, principal, input.ProjectID)
	if err != nil {
		return widgetWriteOutput{}, err
	}
	out := newWidgetWriteOutput(project, operation)
	token := widgetTokenProposal{WidgetID: input.WidgetID, Widget: nil}

	if !input.Confirmed {
		read := s.widgets.ReadForChange
		if operation == operationDuplicateWidget {
			// Any member may copy a widget; only changing one is restricted.
			read = s.widgets.Read
		}
		current, err := read(ctx, input.WidgetID)
		if err != nil {
			return out, fmt.Errorf("read widget: %w", err)
		}
		if operation == operationDuplicateWidget && current.Widget.InvalidReason != nil {
			return out, &widgetToolError{Code: "invalid_request", Field: "widget_id", Message: "This widget cannot be duplicated until it is fixed: " + *current.Widget.InvalidReason + ". Save a corrected version with save_widget first.", Cause: nil}
		}
		definition := defineWidget(current)
		out.Widget, out.Version = &definition, current.Version
		if operation == operationDeleteWidget {
			out.Dashboards = definition.Dashboards
		}
		return s.preview(principal, project, out, token)
	}

	receipt, err := s.confirm(ctx, principal, project, operation, confirmation{Version: input.ExpectedVersion, Token: input.PreviewToken, Key: input.IdempotencyKey}, token, authorize)
	if err != nil {
		return out, err
	}
	return s.finish(ctx, out, receipt)
}

// finish reports a committed or replayed write from a fresh read. A widget
// that reads as gone, deleted by this write or by someone since, is reported
// as deleted.
func (s *WidgetService) finish(ctx context.Context, out widgetWriteOutput, receipt OperationReceipt) (widgetWriteOutput, error) {
	stored, err := decodeWidgetReceipt(receipt.ResultPayload)
	if err != nil {
		return out, err
	}
	out.ReceiptID, out.Replayed = receipt.ID.String(), receipt.Replayed
	deleted := out.Operation == operationDeleteWidget
	if deleted {
		out.Dashboards = append([]widgetDashboardRef{}, stored.RemovedFrom...)
	}

	snapshot, err := s.widgets.Read(ctx, stored.WidgetID)
	shareable, isShareable := errors.AsType[*oops.ShareableError](err)
	switch {
	case err == nil:
		definition := defineWidget(snapshot)
		out.Widget, out.Version, out.SnapshotScope = &definition, snapshot.Version, widgetSnapshotCurrent
		if !deleted {
			out.Dashboards = definition.Dashboards
		}
	case isShareable && shareable.Code == oops.CodeNotFound:
		out.SnapshotScope = widgetSnapshotDeleted
	default:
		out.SnapshotScope = widgetSnapshotVerificationUnavailable
	}
	return out, nil
}

// scope admits a caller to one exact project. project:read is checked before
// any row is looked up, so a denied caller cannot probe what exists; who may
// change a given widget is left to the widgets service's creator-or-write rule.
func (s *WidgetService) scope(ctx context.Context, principal Principal, projectID string) (context.Context, ResolvedProject, error) {
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

	project, err = s.projects.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: id.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return ctx, project, err
	}
	if project.ID != id {
		return ctx, project, ErrForbidden
	}

	// The authenticator usually sets the slug; look it up when it did not.
	organizationSlug := auth.OrganizationSlug
	if organizationSlug == "" {
		organizationSlug, err = s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
		if err != nil {
			return ctx, project, fmt.Errorf("%w: resolve organization slug for widgets: %w", ErrUnavailable, err)
		}
	}
	if organizationSlug == "" {
		return ctx, project, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}

	scoped := *auth
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	return contextvalues.SetAuthContext(ctx, &scoped), project, nil
}

type confirmation struct {
	Version string
	Token   string
	Key     string
}

// preview signs a preview so its confirmation can only commit exactly what
// was shown, at exactly the version that was read.
func (s *WidgetService) preview(principal Principal, project ResolvedProject, out widgetWriteOutput, proposal widgetTokenProposal) (widgetWriteOutput, error) {
	token, err := s.previewToken(previewNonce(s.now()), principal, project, out.Operation, out.Version, proposal)
	if err != nil {
		return widgetWriteOutput{}, err
	}
	out.Preview, out.PreviewToken, out.SnapshotScope = true, token, widgetSnapshotPreview
	return out, nil
}

// previewToken signs a preview. The nonce, its issue time and a random part,
// makes every preview distinct, so a confirmed preview can be held to one
// idempotency key.
func (s *WidgetService) previewToken(nonce string, principal Principal, project ResolvedProject, operation widgetOperation, version string, proposal widgetTokenProposal) (string, error) {
	data, err := json.Marshal(struct {
		Nonce        string              `json:"nonce"`
		Organization string              `json:"organization"`
		User         string              `json:"user"`
		Project      string              `json:"project"`
		Operation    widgetOperation     `json:"operation"`
		Version      string              `json:"version"`
		Proposal     widgetTokenProposal `json:"proposal"`
	}{Nonce: nonce, Organization: principal.OrganizationID, User: principal.UserID, Project: project.ID.String(), Operation: operation, Version: version, Proposal: proposal})
	if err != nil {
		return "", fmt.Errorf("encode widget proposal: %w", err)
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("platform-widget-authoring-v1\x00"))
	_, _ = mac.Write(data)
	return joinPreviewToken(nonce, mac.Sum(nil)), nil
}

// confirm checks a confirmation against its preview, authorizes it, and runs
// the write in the receipt's transaction, so a retry with the same key replays
// the stored result instead of writing twice.
func (s *WidgetService) confirm(ctx context.Context, principal Principal, project ResolvedProject, operation widgetOperation, confirmed confirmation, proposal widgetTokenProposal, authorize widgetAuthorization) (OperationReceipt, error) {
	key := strings.TrimSpace(confirmed.Key)
	if key == "" || len(key) > maxIdempotencyKeyLength {
		return OperationReceipt{}, widgetInvalid("idempotency_key", fmt.Sprintf("Provide a stable idempotency_key of at most %d characters to confirm, and reuse it only to retry this exact request.", maxIdempotencyKeyLength))
	}
	if confirmed.Version == "" {
		return OperationReceipt{}, widgetInvalid("expected_version", "Provide expected_version exactly as the preview returned it.")
	}
	if confirmed.Token == "" {
		return OperationReceipt{}, widgetInvalid("preview_token", "Preview the change with confirmed:false first and confirm with the preview_token it returns.")
	}
	if err := verifyPreviewToken(confirmed.Token, s.now(), func(nonce string) (string, error) {
		return s.previewToken(nonce, principal, project, operation, confirmed.Version, proposal)
	}); err != nil {
		return OperationReceipt{}, err
	}

	mutate, err := authorize(ctx)
	if err != nil {
		return OperationReceipt{}, err
	}

	// The token binds actor, project, operation, version and proposal, so it
	// is the receipt's input hash. A preview commits under one key only: a
	// create or duplicate could otherwise be confirmed again under a new key.
	return executeSingleUseMutationReceipt(ctx, mutationReceiptExecution[widgetReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: string(operation), IdempotencyKey: key, InputHash: confirmed.Token, Label: "widget " + string(operation),
		Invalid: widgetInvalidCause,
		Conflict: func(message string) error {
			return &widgetToolError{Code: "conflict", Field: "idempotency_key", Message: message + " Use a new idempotency_key for a different request.", Cause: nil}
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
	}, func() error { return errPreviewUsed })
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

// boundaryError maps an error to a caller-safe refusal; anything unrecognized is
// logged and replaced, so no SQL or driver text reaches the caller.
func (s *WidgetService) boundaryError(ctx context.Context, tool string, err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	if refusal, ok := errors.AsType[*widgetToolError](err); ok {
		return refusal
	}
	switch {
	case errors.Is(err, ErrForbidden):
		return &widgetToolError{Code: "permission_denied", Field: "", Message: "This needs read access to the project.", Cause: err}
	case errors.Is(err, errPreviewMismatch):
		return &widgetToolError{Code: "preview_mismatch", Field: "preview_token", Message: "The confirmation does not match its preview: the proposal, the version or the widget differs from what was previewed. Preview again and confirm what that preview shows.", Cause: err}
	case errors.Is(err, errPreviewExpired):
		return &widgetToolError{Code: previewExpiredCode, Field: "preview_token", Message: previewExpiredMessage, Cause: err}
	case errors.Is(err, errPreviewUsed):
		return &widgetToolError{Code: previewUsedCode, Field: "preview_token", Message: previewUsedMessage, Cause: err}
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			// The service's message names the problem in the dashboard's words.
			return &widgetToolError{Code: "invalid_request", Field: "", Message: shareable.Error() + ". Correct it from describe_analytics_catalog rather than retrying the same request.", Cause: err}
		case oops.CodeConflict:
			if errors.Is(err, widgets.ErrVersionChanged) {
				return &widgetToolError{Code: "version_changed", Field: "expected_version", Message: "The widget changed after it was read, so nothing was changed. Read it again, show the user what it is now, and preview the change again.", Cause: err}
			}
		case oops.CodeForbidden:
			return &widgetToolError{Code: widgetOwnerOrWriteRequired, Field: "widget_id", Message: "Only the widget's creator, or someone with write access to the project, can change it. Duplicate it to change a copy instead.", Cause: err}
		case oops.CodeUnauthorized:
			return &widgetToolError{Code: "permission_denied", Field: "", Message: "Saving a widget needs a signed-in user; this connection has no user identity.", Cause: err}
		case oops.CodeNotFound:
			return &widgetToolError{Code: "not_found", Field: "widget_id", Message: "No widget with that ID exists in that project. Use an ID from list_widgets.", Cause: err}
		default:
		}
	}
	if s != nil && s.logger != nil {
		s.logger.ErrorContext(ctx, "platform mcp widget tool failed", attr.SlogError(err), attr.SlogToolName(tool))
	}
	return &widgetToolError{Code: unavailableCode, Field: "", Message: "Widgets are temporarily unavailable. Try again shortly.", Cause: errors.Join(ErrUnavailable, err)}
}

func newWidgetWriteOutput(project ResolvedProject, operation widgetOperation) widgetWriteOutput {
	return widgetWriteOutput{Project: analyticsProject(project), Operation: operation, Preview: false, Version: "", PreviewToken: "", Widget: nil, Current: nil, Dashboards: []widgetDashboardRef{}, ReceiptID: "", Replayed: false, SnapshotScope: ""}
}

func validateSaveWidget(input saveWidgetInput) (widgets.Input, error) {
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
	// Only bounds what the preview token hashes; the widgets service validates fields.
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

func widgetInvalid(field, message string) *widgetToolError {
	return &widgetToolError{Code: "invalid_request", Field: field, Message: message, Cause: nil}
}

func widgetInvalidCause(cause error) error {
	return &widgetToolError{Code: "invalid_request", Field: "", Message: "The request is missing its caller identity.", Cause: cause}
}

func summarizeWidget(snapshot widgets.Snapshot) widgetSummary {
	widget := snapshot.Widget
	chartType, _ := widget.Visualization["type"].(string)
	return widgetSummary{
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

func defineWidget(snapshot widgets.Snapshot) widgetDefinition {
	widget := snapshot.Widget
	return widgetDefinition{
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

func dashboardRefs(dashboards []*genwidgets.WidgetDashboard) []widgetDashboardRef {
	out := make([]widgetDashboardRef, 0, len(dashboards))
	for _, dashboard := range dashboards {
		out = append(out, widgetDashboardRef{ID: dashboard.ID, Name: dashboard.Name})
	}
	return out
}
