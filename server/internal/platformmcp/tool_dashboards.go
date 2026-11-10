package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/widgets"
)

const (
	listDashboardsToolName       = "list_dashboards"
	getDashboardToolName         = "get_dashboard"
	saveDashboardToolName        = "save_dashboard"
	duplicateDashboardToolName   = "duplicate_dashboard"
	deleteDashboardToolName      = "delete_dashboard"
	editDashboardLayoutToolName  = "edit_dashboard_layout"
	saveDashboardFiltersToolName = "save_dashboard_filters"

	dashboardsFeature = "dashboards"
)

type listDashboardsInput struct {
	ProjectID string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
}

type listDashboardsOutput struct {
	ProjectID  string             `json:"project_id"`
	Dashboards []dashboardSummary `json:"dashboards"`
}

// dashboardSummary is one dashboard in a list: a project dashboard by id,
// or a Speakeasy-built one by slug.
type dashboardSummary struct {
	ID              string  `json:"id,omitempty"`
	Slug            string  `json:"slug,omitempty"`
	BuiltIn         bool    `json:"built_in"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	CardCount       int     `json:"card_count"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`
	DashboardPath   string  `json:"dashboard_path,omitempty"`
}

type getDashboardInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string `json:"dashboard_id" jsonschema:"a project dashboard id, or a Speakeasy-built dashboard slug, as list_dashboards returns them"`
}

// dashboardView is one dashboard with its cards and saved filters.
type dashboardView struct {
	ID              string  `json:"id,omitempty"`
	Slug            string  `json:"slug,omitempty"`
	BuiltIn         bool    `json:"built_in"`
	Name            string  `json:"name"`
	Description     *string `json:"description,omitempty"`
	CreatedByUserID *string `json:"created_by_user_id,omitempty"`
	UpdatedAt       string  `json:"updated_at,omitempty"`

	// Version is what a write's expected_version must match.
	Version       string            `json:"version,omitempty"`
	Filters       *dashboardFilters `json:"filters,omitempty"`
	Cards         []dashboardCard   `json:"cards"`
	DashboardPath string            `json:"dashboard_path,omitempty"`
}

// dashboardCard is one card: the saved widget it shows and where it sits on
// the 12-column grid. A Speakeasy-built card is not a saved widget, so it has
// no ids.
type dashboardCard struct {
	PlacementID string `json:"placement_id,omitempty"`
	WidgetID    string `json:"widget_id,omitempty"`
	WidgetName  string `json:"widget_name,omitempty"`
	Dataset     string `json:"dataset,omitempty"`
	ChartType   string `json:"chart_type,omitempty"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	W           int    `json:"w"`
	H           int    `json:"h"`
}

// dashboardFilters is the date range and filter values a dashboard opens on.
type dashboardFilters struct {
	Range  *dashboardRange     `json:"range,omitempty" jsonschema:"the date range; omit to open on the page default"`
	Values map[string][]string `json:"values,omitempty" jsonschema:"values picked per catalog dimension, by dimension name from describe_analytics_catalog; at most 20 dimensions and 100 values each, each value at most 500 characters"`
}

// dashboardRange is a relative window or an absolute from-to range.
type dashboardRange struct {
	Preset *string `json:"preset,omitempty" jsonschema:"a relative window such as 24h, 7d or 30d; not with from and to"`
	From   *string `json:"from,omitempty" jsonschema:"start of an absolute range, RFC 3339"`
	To     *string `json:"to,omitempty" jsonschema:"end of an absolute range, RFC 3339"`
	Label  *string `json:"label,omitempty" jsonschema:"a name for an absolute range, at most 200 characters"`
}

// dashboardConfirmation is what every dashboard write carries to move from
// preview to commit.
type dashboardConfirmation struct {
	Confirmed       bool   `json:"confirmed" jsonschema:"false previews without saving; true only after the user confirmed the returned preview"`
	ExpectedVersion string `json:"expected_version,omitempty" jsonschema:"version returned by the preview; required to confirm"`
	PreviewToken    string `json:"preview_token,omitempty" jsonschema:"token returned by the preview for this exact proposal; required to confirm"`
	IdempotencyKey  string `json:"idempotency_key,omitempty" jsonschema:"required to confirm, at most 128 characters; reuse it only to retry this exact confirmed request"`
}

type saveDashboardInput struct {
	ProjectID   string  `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string  `json:"dashboard_id,omitempty" jsonschema:"the project dashboard to rename or re-describe; omit to create a new empty dashboard"`
	Name        string  `json:"name" jsonschema:"display name, at most 200 characters"`
	Description *string `json:"description,omitempty" jsonschema:"what the dashboard is for, at most 2000 characters; omit or leave empty for none"`
	dashboardConfirmation
}

type duplicateDashboardInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string `json:"dashboard_id" jsonschema:"the project dashboard id or Speakeasy-built dashboard slug to copy"`
	dashboardConfirmation
}

type deleteDashboardInput struct {
	ProjectID   string `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string `json:"dashboard_id" jsonschema:"the project dashboard to delete"`
	dashboardConfirmation
}

type editDashboardLayoutInput struct {
	ProjectID   string                     `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string                     `json:"dashboard_id" jsonschema:"the project dashboard whose cards change"`
	Operations  []dashboardLayoutOperation `json:"operations" jsonschema:"one to 100 card changes, applied in order and checked as one final layout"`
	dashboardConfirmation
}

// dashboardLayoutOperation is one card change. Its schema says what each
// kind needs, so an agent learns it there rather than from a refusal.
type dashboardLayoutOperation struct {
	Op          string `json:"op" jsonschema:"add_card (widget_id, with x, y, w and h optional), move_card (placement_id with all four of x, y, w and h) or remove_card (placement_id)"`
	WidgetID    string `json:"widget_id,omitempty" jsonschema:"add_card: the saved widget of this project the new card shows"`
	PlacementID string `json:"placement_id,omitempty" jsonschema:"move_card and remove_card: the card, as get_dashboard returns it"`
	X           *int   `json:"x,omitempty" jsonschema:"column the card starts at, 0 to 11; move_card needs all four of x, y, w and h; add_card defaults to 0"`
	Y           *int   `json:"y,omitempty" jsonschema:"row the card starts at, from 0; move_card needs all four of x, y, w and h; add_card defaults to below every card"`
	W           *int   `json:"w,omitempty" jsonschema:"width in columns, up to 12; move_card needs all four of x, y, w and h; add_card defaults to the chart type's opening size"`
	H           *int   `json:"h,omitempty" jsonschema:"height in rows; move_card needs all four of x, y, w and h; add_card defaults to the chart type's opening size"`
}

type saveDashboardFiltersInput struct {
	ProjectID   string           `json:"project_id" jsonschema:"exact project UUID, as list_projects returns it"`
	DashboardID string           `json:"dashboard_id" jsonschema:"the project dashboard whose saved filters change"`
	Filters     dashboardFilters `json:"filters" jsonschema:"the complete filters the dashboard opens on for everyone; empty clears them"`
	dashboardConfirmation
}

// dashboardWriteOutput is a preview, or the committed result with a fresh
// read of the dashboard.
type dashboardWriteOutput struct {
	ProjectID string `json:"project_id"`
	Operation string `json:"operation"`
	Preview   bool   `json:"preview"`

	// Version is, in a preview, the expected_version to confirm with; after
	// a commit, the dashboard's current version.
	Version      string `json:"version,omitempty"`
	PreviewToken string `json:"preview_token,omitempty"`
	DashboardID  string `json:"dashboard_id,omitempty"`
	Deleted      bool   `json:"deleted"`

	// Current is the dashboard as it stands: before the change in a preview,
	// the fresh read after a commit.
	Current *dashboardView `json:"current,omitempty"`

	// Proposed is what the change would leave, in a preview only. Ids of
	// cards and widgets a preview would create are left out.
	Proposed  *dashboardView `json:"proposed,omitempty"`
	ReceiptID string         `json:"receipt_id,omitempty"`
	Replayed  bool           `json:"replayed"`

	// SnapshotScope says what Current reflects after a commit.
	SnapshotScope dashboardSnapshotScope `json:"snapshot_scope"`
	DashboardPath string                 `json:"dashboard_path,omitempty"`
}

// dashboardHints are what a tool's annotations say about it. idempotent says
// whether confirming the same change again, under a new key after a new
// preview, leaves the project as the first confirmation did: a create, a copy
// or an added card does not, it makes another dashboard or card each time.
type dashboardHints struct {
	read, destructive, idempotent bool
}

func dashboardManifest(name, title, description string, hints dashboardHints) *mcp.Tool {
	annotations := &mcp.ToolAnnotations{Title: "", ReadOnlyHint: hints.read, DestructiveHint: new(hints.destructive), IdempotentHint: hints.idempotent, OpenWorldHint: new(false)}
	return &mcp.Tool{Meta: nil, Name: name, Title: title, Description: description, InputSchema: nil, OutputSchema: nil, Icons: nil, Annotations: annotations}
}

// dashboardSnapshotScope says what a write result reflects.
type dashboardSnapshotScope string

const (
	// dashboardSnapshotPreview is a proposal nothing has written yet.
	dashboardSnapshotPreview dashboardSnapshotScope = "preview"
	// dashboardSnapshotCurrent is a fresh read taken after the commit.
	dashboardSnapshotCurrent dashboardSnapshotScope = "current"
	// dashboardSnapshotDeleted is a fresh read that found the dashboard gone,
	// deleted by this write or by someone since.
	dashboardSnapshotDeleted dashboardSnapshotScope = "deleted"
	// dashboardSnapshotVerificationUnavailable is a committed write whose
	// fresh read failed; a retry with the same inputs replays it.
	dashboardSnapshotVerificationUnavailable dashboardSnapshotScope = "verification_unavailable"
)

const dashboardWriteSafeguards = " Call with confirmed:false to preview: it validates the change exactly as saving would and returns the current dashboard (none for a create), the proposed result (none for a delete), its version and a preview_token. Show the preview, get explicit confirmation, then resend the identical request with confirmed:true, expected_version, preview_token and a stable idempotency_key. A dashboard changed since the preview is refused as a conflict; read and preview again. Retrying with the same key replays the first result; a preview confirms once, and confirming it again under a new idempotency_key is refused as preview_used; a preview older than a day is refused as preview_expired. After a commit the result reports the dashboard from a fresh read; snapshot_scope deleted means it is gone, and verification_unavailable means the write committed but that read failed, so retry with the same inputs and idempotency_key to replay it. Speakeasy-built dashboards are read-only. Requires project:read, and changing a dashboard someone else made needs project:write; Explore must be switched on for the organization. External users only."

// registerDashboardTools registers the seven dashboard tools. A nil service
// keeps them listed with the same contract, each answering unavailable.
func registerDashboardTools(reg *Registrar, service *DashboardsService) {
	meta := ToolMeta{Authorization: ExternalAuthorizationMember, Audiences: externalOnly, ProjectScope: ProjectScopeExplicit, DiscoveryScopes: discoveryProjectRead}
	var logger *slog.Logger
	if service.valid() {
		logger = service.logger
	}

	addTool(reg, dashboardManifest(listDashboardsToolName, "List Dashboards", "List a project's dashboards with the Speakeasy-built ones, marked built_in: each with its name, description, card count, creator and when it last changed. A project dashboard is named by id, a Speakeasy-built one by slug; use get_dashboard for its cards. Requires project:read and Explore switched on for the organization. External users only.", dashboardHints{read: true, destructive: false, idempotent: true}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input listDashboardsInput) (*mcp.CallToolResult, listDashboardsOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (listDashboardsOutput, error) {
			return service.List(ctx, principal, input)
		})
	})
	addTool(reg, dashboardManifest(getDashboardToolName, "Read a Dashboard", "Read one dashboard by project dashboard id or Speakeasy-built slug: its cards with each card's placement on the 12-column grid (x, y, w, h) and the saved widget it shows (id, name, dataset, chart type), its saved filters, and its version, which a write's expected_version must match. Requires project:read and Explore switched on for the organization. External users only.", dashboardHints{read: true, destructive: false, idempotent: true}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input getDashboardInput) (*mcp.CallToolResult, dashboardView, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardView, error) {
			return service.Get(ctx, principal, input)
		})
	})

	addTool(reg, dashboardManifest(saveDashboardToolName, "Save a Dashboard", "Create an empty project dashboard when dashboard_id is omitted, or rename and re-describe the project dashboard named. Cards are placed with edit_dashboard_layout."+dashboardWriteSafeguards, dashboardHints{read: false, destructive: true, idempotent: false}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input saveDashboardInput) (*mcp.CallToolResult, dashboardWriteOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardWriteOutput, error) {
			return service.save(ctx, principal, input)
		})
	})
	addTool(reg, dashboardManifest(duplicateDashboardToolName, "Duplicate a Dashboard", "Copy a project dashboard by id, or a Speakeasy-built dashboard by slug, into a new project dashboard the caller owns. Every card of the copy shows a new saved widget, so the copy changes independently of the original. A dashboard with widgets over a dataset only organization administrators may read, such as risk_findings, can only be copied by them, and anyone else is refused as dataset_restricted."+dashboardWriteSafeguards, dashboardHints{read: false, destructive: false, idempotent: false}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input duplicateDashboardInput) (*mcp.CallToolResult, dashboardWriteOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardWriteOutput, error) {
			return service.duplicate(ctx, principal, input)
		})
	})
	addTool(reg, dashboardManifest(deleteDashboardToolName, "Delete a Dashboard", "Delete a project dashboard and its cards. The saved widgets its cards showed stay saved."+dashboardWriteSafeguards, dashboardHints{read: false, destructive: true, idempotent: true}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input deleteDashboardInput) (*mcp.CallToolResult, dashboardWriteOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardWriteOutput, error) {
			return service.delete(ctx, principal, input)
		})
	})
	addTool(reg, dashboardManifest(editDashboardLayoutToolName, "Edit a Dashboard's Layout", "Change a project dashboard's cards in one call carrying a list of operations, applied in order: add_card places a saved widget of the project as a new card, move_card moves or resizes a card, remove_card takes a card off (its widget stays). The final layout follows the grid's rules and is checked as a whole: 12 columns, a minimum size per chart type (a number card 2 by 2, any other 4 by 3), no overlapping cards, at most 100 cards. Create the widgets first with the widget tools."+dashboardWriteSafeguards, dashboardHints{read: false, destructive: true, idempotent: false}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input editDashboardLayoutInput) (*mcp.CallToolResult, dashboardWriteOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardWriteOutput, error) {
			return service.editLayout(ctx, principal, input)
		})
	})
	addTool(reg, dashboardManifest(saveDashboardFiltersToolName, "Save a Dashboard's Filters", "Replace the filters a project dashboard opens on for everyone: a default date range (a relative preset, or an absolute from and to) and values per catalog dimension."+dashboardWriteSafeguards, dashboardHints{read: false, destructive: true, idempotent: true}), meta, func(ctx context.Context, _ *mcp.CallToolRequest, input saveDashboardFiltersInput) (*mcp.CallToolResult, dashboardWriteOutput, error) {
		return principalToolCall(ctx, dashboardToolError(ctx, logger), func(principal Principal) (dashboardWriteOutput, error) {
			return service.saveFilters(ctx, principal, input)
		})
	})
}

func (s *DashboardsService) save(ctx context.Context, principal Principal, input saveDashboardInput) (dashboardWriteOutput, error) {
	proposal := struct {
		DashboardID string  `json:"dashboard_id"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
	}{DashboardID: input.DashboardID, Name: input.Name, Description: input.Description}
	if input.DashboardID == "" {
		return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: saveDashboardToolName, target: "", builtIn: "", creates: true, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, _ string) (*gen.Dashboard, error) {
			return s.management.CreateDashboardInTx(ctx, dbtx, input.Name, input.Description)
		}})
	}
	target, err := projectDashboardTarget(input.DashboardID)
	if err != nil {
		return dashboardWriteOutput{}, err
	}
	return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: saveDashboardToolName, target: target, builtIn: "", creates: false, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, version string) (*gen.Dashboard, error) {
		return s.management.UpdateDashboardInTx(ctx, dbtx, target, version, input.Name, input.Description)
	}})
}

func (s *DashboardsService) duplicate(ctx context.Context, principal Principal, input duplicateDashboardInput) (dashboardWriteOutput, error) {
	proposal := struct {
		DashboardID string `json:"dashboard_id"`
	}{DashboardID: input.DashboardID}
	target, err := projectDashboardTarget(input.DashboardID)
	if errors.Is(err, errDashboardBuiltInReadOnly) {
		slug := input.DashboardID
		return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: duplicateDashboardToolName, target: "", builtIn: slug, creates: true, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, _ string) (*gen.Dashboard, error) {
			return s.management.DuplicateBuiltInDashboardInTx(ctx, dbtx, slug)
		}})
	}
	if err != nil {
		return dashboardWriteOutput{}, err
	}
	return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: duplicateDashboardToolName, target: target, builtIn: "", creates: true, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, version string) (*gen.Dashboard, error) {
		return s.management.DuplicateDashboardInTx(ctx, dbtx, target, version)
	}})
}

func (s *DashboardsService) delete(ctx context.Context, principal Principal, input deleteDashboardInput) (dashboardWriteOutput, error) {
	target, err := projectDashboardTarget(input.DashboardID)
	if err != nil {
		return dashboardWriteOutput{}, err
	}
	proposal := struct {
		DashboardID string `json:"dashboard_id"`
	}{DashboardID: target}
	return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: deleteDashboardToolName, target: target, builtIn: "", creates: false, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, version string) (*gen.Dashboard, error) {
		return nil, s.management.DeleteDashboardInTx(ctx, dbtx, target, version)
	}})
}

func (s *DashboardsService) editLayout(ctx context.Context, principal Principal, input editDashboardLayoutInput) (dashboardWriteOutput, error) {
	target, err := projectDashboardTarget(input.DashboardID)
	if err != nil {
		return dashboardWriteOutput{}, err
	}
	operations := make([]dashboards.LayoutOperation, 0, len(input.Operations))
	for _, operation := range input.Operations {
		operations = append(operations, dashboards.LayoutOperation{Kind: dashboards.LayoutOperationKind(operation.Op), WidgetID: operation.WidgetID, PlacementID: operation.PlacementID, X: operation.X, Y: operation.Y, W: operation.W, H: operation.H})
	}
	proposal := struct {
		DashboardID string                     `json:"dashboard_id"`
		Operations  []dashboardLayoutOperation `json:"operations"`
	}{DashboardID: target, Operations: input.Operations}
	return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: editDashboardLayoutToolName, target: target, builtIn: "", creates: false, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, version string) (*gen.Dashboard, error) {
		return s.management.EditDashboardLayoutInTx(ctx, dbtx, target, version, operations)
	}})
}

func (s *DashboardsService) saveFilters(ctx context.Context, principal Principal, input saveDashboardFiltersInput) (dashboardWriteOutput, error) {
	target, err := projectDashboardTarget(input.DashboardID)
	if err != nil {
		return dashboardWriteOutput{}, err
	}
	proposal := struct {
		DashboardID string           `json:"dashboard_id"`
		Filters     dashboardFilters `json:"filters"`
	}{DashboardID: target, Filters: input.Filters}
	return s.write(ctx, principal, input.ProjectID, input.dashboardConfirmation, dashboardWrite{operation: saveDashboardFiltersToolName, target: target, builtIn: "", creates: false, proposal: proposal, apply: func(ctx context.Context, dbtx pgx.Tx, version string) (*gen.Dashboard, error) {
		// The token already hashed the request, so checkFilters may
		// normalize it in place.
		filters := &gen.DashboardFilters{Range: nil, Values: input.Filters.Values}
		if r := input.Filters.Range; r != nil {
			filters.Range = &gen.DashboardRange{Preset: r.Preset, From: r.From, To: r.To, Label: r.Label}
		}
		return s.management.SaveDashboardFiltersInTx(ctx, dbtx, target, version, filters)
	}})
}

// dashboardRefusal is what a dashboard tool says when it will not act.
type dashboardRefusal struct {
	Code    string `json:"code"`
	Feature string `json:"feature"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message"`
}

// dashboardToolError turns every refusal into a readable result. A failure
// the tools did not expect is logged and reads as unavailable, so its text
// never reaches the agent.
func dashboardToolError(ctx context.Context, logger *slog.Logger) func(error) (*mcp.CallToolResult, bool) {
	return func(err error) (*mcp.CallToolResult, bool) {
		if result, ok := externalAuthorizationToolResult(err); ok {
			return result, true
		}
		refusal := dashboardRefusal{Code: unavailableCode, Feature: dashboardsFeature, Reason: "", Message: "Dashboards are temporarily unavailable. Try again shortly."}
		shareable, isShareable := errors.AsType[*oops.ShareableError](err)
		switch {
		case errors.Is(err, ErrDashboardsNotEnabled):
			refusal.Code, refusal.Message = analyticsNotEnabledCode, "Dashboards are not switched on for your organization yet. They arrive with Explore; ask your Speakeasy contact to turn it on."
		case errors.Is(err, errDashboardBuiltInReadOnly):
			refusal.Code, refusal.Message = "built_in_read_only", "Speakeasy-built dashboards are read-only. Use duplicate_dashboard to make a project copy, then change the copy."
		case errors.Is(err, dashboards.ErrStaleVersion):
			refusal.Code, refusal.Message = "conflict", "The dashboard changed since it was read. Call get_dashboard again, preview again and confirm the new version."
		case errors.Is(err, errPreviewMismatch):
			refusal.Code, refusal.Message = "conflict", "The preview token does not match this proposal and version. Preview again and confirm the identical proposal."
		case errors.Is(err, errPreviewUsed):
			refusal.Code, refusal.Message = previewUsedCode, previewUsedMessage
		case errors.Is(err, errPreviewExpired):
			refusal.Code, refusal.Message = previewExpiredCode, previewExpiredMessage
		case errors.Is(err, ErrForbidden):
			refusal.Code, refusal.Message = "permission_denied", "An authorized external user with project:read on the exact project is required."
		case errors.Is(err, widgets.ErrDatasetRestricted):
			refusal.Code, refusal.Message = widgetDatasetRestricted, "This dashboard has widgets over a dataset only organization administrators can read, so only they can copy it."
		case isShareable && (shareable.Code == oops.CodeBadRequest || shareable.Code == oops.CodeInvalid):
			refusal.Code, refusal.Reason, refusal.Message = "invalid_request", shareable.Error(), "Correct what the reason names and preview again."
		case isShareable && shareable.Code == oops.CodeConflict:
			refusal.Code, refusal.Reason, refusal.Message = "conflict", shareable.Error(), "Preview again; do not change the input when retrying a confirmed request."
		case isShareable && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized):
			refusal.Code, refusal.Message = widgetOwnerOrWriteRequired, "Only the dashboard's creator, or someone with project:write, may change it."
		case isShareable && shareable.Code == oops.CodeNotFound:
			refusal.Code, refusal.Reason, refusal.Message = "not_found", shareable.Error(), "Use the ids list_dashboards and get_dashboard return."
		case errors.Is(err, ErrUnavailable):
		default:
			if logger != nil {
				logger.ErrorContext(ctx, "platform mcp dashboards call failed", attr.SlogError(err))
			}
		}
		data, marshalErr := json.Marshal(refusal)
		if marshalErr != nil {
			return nil, false
		}
		return &mcp.CallToolResult{Meta: nil, StructuredContent: nil, InputRequests: nil, RequestState: "", Content: []mcp.Content{&mcp.TextContent{Meta: nil, Annotations: nil, Text: string(data)}}, IsError: true}, true
	}
}
