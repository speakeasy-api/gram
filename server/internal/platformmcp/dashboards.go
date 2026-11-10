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

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dashboards"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

// The versions a write that has no project dashboard row to lock is
// previewed and confirmed against: a new dashboard, or a Speakeasy-built one,
// which is code and changes only with a release.
const (
	dashboardNewVersion        = "new"
	dashboardBuiltInVersionTag = "built_in:"
)

var (
	// ErrDashboardsNotEnabled marks a call for an organization that has
	// Explore switched off.
	ErrDashboardsNotEnabled = errors.New("platform mcp dashboards not enabled")

	// errDashboardBuiltInReadOnly marks a write that names a Speakeasy-built
	// dashboard, which only duplicate_dashboard may use.
	errDashboardBuiltInReadOnly = errors.New("built-in dashboard is read only")
)

// DashboardsService serves a project's dashboards to the Platform MCP
// through the dashboards service, which stays the source of truth for
// authorization, validation and audit. Only external users are admitted: the
// audit names a user.
type DashboardsService struct {
	logger        *slog.Logger
	management    dashboardsManagement
	projects      *PostgresReader
	engine        *authz.Engine
	flags         feature.Provider
	organizations OrganizationSlugResolver
	db            *pgxpool.Pool
	key           []byte
	now           func() time.Time
}

// dashboardsManagement is the dashboards service's read and transaction
// cores, each authorizing against the scoped auth context.
type dashboardsManagement interface {
	ListDashboards(ctx context.Context, payload *gen.ListDashboardsPayload) (*gen.ListDashboardsResult, error)
	ReadDashboard(ctx context.Context, id string) (dashboards.Detail, error)
	ReadBuiltInDashboard(ctx context.Context, slug string) (*gen.BuiltInDashboard, error)
	CardWidgetsInTx(ctx context.Context, dbtx pgx.Tx, id string) (map[string]dashboards.CardWidget, error)
	CreateDashboardInTx(ctx context.Context, dbtx pgx.Tx, name string, description *string) (*gen.Dashboard, error)
	UpdateDashboardInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion, name string, description *string) (*gen.Dashboard, error)
	SaveDashboardFiltersInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion string, filters *gen.DashboardFilters) (*gen.Dashboard, error)
	EditDashboardLayoutInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion string, operations []dashboards.LayoutOperation) (*gen.Dashboard, error)
	DuplicateDashboardInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion string) (*gen.Dashboard, error)
	DuplicateBuiltInDashboardInTx(ctx context.Context, dbtx pgx.Tx, slug string) (*gen.Dashboard, error)
	DeleteDashboardInTx(ctx context.Context, dbtx pgx.Tx, id, expectedVersion string) error
}

// NewDashboardsService returns nil when a dependency is missing, so the
// tools are served as stubs.
func NewDashboardsService(logger *slog.Logger, management *dashboards.Service, projects *PostgresReader, engine *authz.Engine, flags feature.Provider, organizations OrganizationSlugResolver, key string) *DashboardsService {
	var db *pgxpool.Pool
	if projects != nil {
		db = projects.db
	}
	s := &DashboardsService{logger: logger, management: management, projects: projects, engine: engine, flags: flags, organizations: organizations, db: db, key: []byte(key), now: time.Now}
	if !s.valid() {
		return nil
	}
	return s
}

func (s *DashboardsService) valid() bool {
	return s != nil && s.logger != nil && s.management != nil && s.projects != nil && s.engine != nil && s.flags != nil && s.organizations != nil && s.db != nil && len(s.key) > 0 && s.now != nil
}

// dashboardScope is the exact project a call runs in, and the organization
// slug its dashboard paths start with.
type dashboardScope struct {
	project          ResolvedProject
	organizationSlug string
}

// scope holds the caller to project:read on the exact project before any row
// is read, so a denied caller cannot probe what exists, then checks the
// Explore flag. Who may change a dashboard is the dashboards service's rule.
// write loads the actor's email for the audit.
func (s *DashboardsService) scope(ctx context.Context, principal Principal, projectID string, write bool) (context.Context, dashboardScope, error) {
	var zero dashboardScope
	if !s.valid() {
		return ctx, zero, ErrUnavailable
	}

	id, err := uuid.Parse(projectID)
	if err != nil || id == uuid.Nil {
		return ctx, zero, oops.E(oops.CodeBadRequest, nil, "name exactly one project by its id, as list_projects returns it")
	}

	auth, ok := contextvalues.GetAuthContext(ctx)
	if !ok || auth == nil || principal.UserID == "" || auth.UserID != principal.UserID || auth.ActiveOrganizationID != principal.OrganizationID || principal.surface() != SurfacePlatformMCP {
		return ctx, zero, ErrForbidden
	}

	if err := s.engine.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: id.String(), Dimensions: nil}); err != nil {
		return ctx, zero, ErrForbidden
	}

	project, err := s.projects.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: id.String(), ProjectSlug: "", Query: "", Cursor: "", Limit: 0, Readiness: ""})
	if err != nil {
		return ctx, zero, err
	}
	if project.ID != id {
		return ctx, zero, ErrForbidden
	}

	// The external connection already loaded the organization's slug.
	organizationSlug := auth.OrganizationSlug
	if organizationSlug == "" {
		organizationSlug, err = s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
		if err != nil {
			return ctx, zero, fmt.Errorf("%w: resolve organization slug for dashboards: %w", ErrUnavailable, err)
		}
	}
	enabled, err := exploreEnabled(ctx, s.flags, principal.OrganizationID, organizationSlug, project.Slug)
	if err != nil {
		return ctx, zero, err
	}
	if !enabled {
		return ctx, zero, ErrDashboardsNotEnabled
	}

	scoped := *auth
	scoped.OrganizationSlug = organizationSlug
	scoped.ProjectID, scoped.ProjectSlug = &project.ID, &project.Slug
	if write && scoped.Email == nil {
		// The audit shows the actor's email as the dashboard does; a user with
		// no stored profile is audited by id.
		user, err := usersrepo.New(s.db).GetUser(ctx, principal.UserID)
		switch {
		case err == nil:
			scoped.Email = &user.Email
		case !errors.Is(err, pgx.ErrNoRows):
			return ctx, zero, fmt.Errorf("load dashboard actor: %w", err)
		}
	}

	return contextvalues.SetAuthContext(ctx, &scoped), dashboardScope{project: project, organizationSlug: organizationSlug}, nil
}

// List lists the project's dashboards and the Speakeasy-built ones.
func (s *DashboardsService) List(ctx context.Context, principal Principal, input listDashboardsInput) (listDashboardsOutput, error) {
	var zero listDashboardsOutput
	ctx, scope, err := s.scope(ctx, principal, input.ProjectID, false)
	if err != nil {
		return zero, err
	}
	result, err := s.management.ListDashboards(ctx, &gen.ListDashboardsPayload{SessionToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return zero, fmt.Errorf("list dashboards: %w", err)
	}

	out := listDashboardsOutput{ProjectID: scope.project.ID.String(), Dashboards: make([]dashboardSummary, 0, len(result.BuiltIn)+len(result.Dashboards))}
	for _, page := range result.BuiltIn {
		description := page.Description
		out.Dashboards = append(out.Dashboards, dashboardSummary{ID: "", Slug: page.Slug, BuiltIn: true, Name: page.Name, Description: &description, CardCount: len(page.Cards), CreatedByUserID: nil, UpdatedAt: "", DashboardPath: scope.builtInPath(page.Slug)})
	}
	for _, dashboard := range result.Dashboards {
		out.Dashboards = append(out.Dashboards, dashboardSummary{ID: dashboard.ID, Slug: "", BuiltIn: false, Name: dashboard.Name, Description: dashboard.Description, CardCount: len(dashboard.Widgets), CreatedByUserID: dashboard.CreatedByUserID, UpdatedAt: dashboard.UpdatedAt, DashboardPath: scope.dashboardPath(dashboard.ID)})
	}
	return out, nil
}

// Get reads one dashboard, by id or built-in slug.
func (s *DashboardsService) Get(ctx context.Context, principal Principal, input getDashboardInput) (dashboardView, error) {
	var zero dashboardView
	ctx, scope, err := s.scope(ctx, principal, input.ProjectID, false)
	if err != nil {
		return zero, err
	}
	if _, err := uuid.Parse(input.DashboardID); err != nil {
		page, err := s.management.ReadBuiltInDashboard(ctx, input.DashboardID)
		if isOopsCode(err, oops.CodeNotFound) {
			return zero, oops.E(oops.CodeNotFound, nil, "no dashboard or Speakeasy-built dashboard has that id or slug")
		}
		if err != nil {
			return zero, fmt.Errorf("read built-in dashboard: %w", err)
		}
		return scope.builtInView(page), nil
	}
	detail, err := s.management.ReadDashboard(ctx, input.DashboardID)
	if err != nil {
		return zero, fmt.Errorf("read dashboard: %w", err)
	}
	return scope.detailView(detail), nil
}

// dashboardWrite is one write tool's request: the dashboard it targets, the
// normalized proposal the preview token binds, and the core that applies it
// inside a transaction.
type dashboardWrite struct {
	// operation is the tool name, which also keys idempotency receipts.
	operation string

	// target is the dashboard id the write changes or copies; empty for a new
	// dashboard.
	target string

	// builtIn is the Speakeasy-built dashboard slug a duplicate copies.
	builtIn string

	// creates is true when the write makes a new dashboard, whose ids a
	// rolled-back preview cannot show.
	creates bool

	// proposal is the normalized request the preview token binds.
	proposal any

	// apply runs the change inside the caller's transaction, refusing it when
	// the dashboard is no longer at the version given. A write that makes a
	// dashboard from nothing or from a Speakeasy-built one has no row to
	// version and ignores it; the preview token binds the constant version
	// its preview returned.
	apply func(ctx context.Context, dbtx pgx.Tx, expectedVersion string) (*gen.Dashboard, error)
}

// dashboardReceipt is what a committed write records for replay: the
// dashboard it left, and whether it deleted it.
type dashboardReceipt struct {
	DashboardID string `json:"dashboard_id"`
	Deleted     bool   `json:"deleted,omitempty"`
}

// write previews a change with confirmed:false and applies it once confirmed,
// through a receipt so a retry with the same key replays the first result. A
// preview commits under one key only: a create or duplicate could otherwise
// be confirmed again under a new key, making a dashboard each time.
func (s *DashboardsService) write(ctx context.Context, principal Principal, projectID string, confirm dashboardConfirmation, request dashboardWrite) (dashboardWriteOutput, error) {
	var zero dashboardWriteOutput

	// The proposal hashed here is bounded by the transport's MaxBodyBytes;
	// the dashboards service bounds every field of it as it validates.
	encoded, err := json.Marshal(request.proposal)
	if err != nil {
		return zero, fmt.Errorf("encode dashboard proposal: %w", err)
	}

	ctx, scope, err := s.scope(ctx, principal, projectID, true)
	if err != nil {
		return zero, err
	}

	if !confirm.Confirmed {
		return s.preview(ctx, scope, principal, request, encoded)
	}

	key := strings.TrimSpace(confirm.IdempotencyKey)
	if key == "" || len(key) > maxIdempotencyKeyLength || confirm.ExpectedVersion == "" || confirm.PreviewToken == "" {
		return zero, oops.E(oops.CodeBadRequest, nil, "confirming needs idempotency_key (at most %d characters), expected_version and preview_token from the preview", maxIdempotencyKeyLength)
	}
	if err := verifyPreviewToken(confirm.PreviewToken, s.now(), func(nonce string) (string, error) {
		return s.previewToken(nonce, principal, scope.project, request.operation, confirm.ExpectedVersion, encoded), nil
	}); err != nil {
		return zero, err
	}

	// The token binds actor, project, operation, version, nonce and proposal,
	// so it is the receipt's input hash.
	receipt, err := executeSingleUseMutationReceipt(ctx, mutationReceiptExecution[dashboardReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: scope.project, Operation: request.operation, IdempotencyKey: key, InputHash: confirm.PreviewToken, Label: "dashboard " + request.operation,
		Invalid:     func(err error) error { return oops.E(oops.CodeBadRequest, err, "invalid dashboard receipt") },
		Conflict:    func(message string) error { return oops.E(oops.CodeConflict, nil, "%s", message) },
		Unavailable: func(err error) error { return fmt.Errorf("%w: %w", ErrUnavailable, err) },
		ValidateReplay: func(data []byte) bool {
			var value dashboardReceipt
			if json.Unmarshal(data, &value) != nil {
				return false
			}
			id, err := uuid.Parse(value.DashboardID)
			return err == nil && id != uuid.Nil
		},
		EncodeResult: func(value dashboardReceipt) ([]byte, error) { return json.Marshal(value) },
		Mutate: func(ctx context.Context, dbtx pgx.Tx) (dashboardReceipt, error) {
			view, err := request.apply(ctx, dbtx, confirm.ExpectedVersion)
			if err != nil {
				return dashboardReceipt{}, err
			}
			if view == nil {
				return dashboardReceipt{DashboardID: request.target, Deleted: true}, nil
			}
			return dashboardReceipt{DashboardID: view.ID, Deleted: false}, nil
		},
	}, func() error { return errPreviewUsed })
	if err != nil {
		return zero, err
	}

	var result dashboardReceipt
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return zero, ErrUnavailable
	}

	out := dashboardWriteOutput{
		ProjectID: scope.project.ID.String(), Operation: request.operation, Preview: false, PreviewToken: "", Version: "",
		DashboardID: result.DashboardID, Deleted: result.Deleted, Current: nil, Proposed: nil,
		ReceiptID: receipt.ID.String(), Replayed: receipt.Replayed, SnapshotScope: dashboardSnapshotCurrent, DashboardPath: "",
	}
	detail, err := s.management.ReadDashboard(ctx, result.DashboardID)
	switch {
	case err == nil:
		view := scope.detailView(detail)
		out.Current, out.Version, out.DashboardPath = &view, detail.Version, view.DashboardPath
	case isOopsCode(err, oops.CodeNotFound):
		out.SnapshotScope = dashboardSnapshotDeleted
	default:
		s.logger.WarnContext(ctx, "verify platform mcp dashboard write", attr.SlogError(err), attr.SlogToolName(request.operation))
		out.SnapshotScope = dashboardSnapshotVerificationUnavailable
	}
	return out, nil
}

// preview applies the change in a transaction it then rolls back, so the
// owning service validates it exactly as it would commit it, and returns the
// current state beside what the change would leave.
func (s *DashboardsService) preview(ctx context.Context, scope dashboardScope, principal Principal, request dashboardWrite, encoded []byte) (dashboardWriteOutput, error) {
	var zero dashboardWriteOutput

	out := dashboardWriteOutput{
		ProjectID: scope.project.ID.String(), Operation: request.operation, Preview: true, Version: "", PreviewToken: "",
		DashboardID: request.target, Deleted: false, Current: nil, Proposed: nil,
		ReceiptID: "", Replayed: false, SnapshotScope: dashboardSnapshotPreview, DashboardPath: "",
	}
	switch {
	case request.builtIn != "":
		page, err := s.management.ReadBuiltInDashboard(ctx, request.builtIn)
		if isOopsCode(err, oops.CodeNotFound) {
			return zero, oops.E(oops.CodeNotFound, nil, "no Speakeasy-built dashboard has that slug")
		}
		if err != nil {
			return zero, fmt.Errorf("read built-in dashboard: %w", err)
		}
		view := scope.builtInView(page)
		out.Current, out.Version, out.DashboardPath = &view, view.Version, view.DashboardPath
	case request.target == "":
		out.Version = dashboardNewVersion
	default:
		detail, err := s.management.ReadDashboard(ctx, request.target)
		if err != nil {
			return zero, fmt.Errorf("read dashboard for preview: %w", err)
		}
		view := scope.detailView(detail)
		out.Current, out.Version, out.DashboardPath = &view, detail.Version, view.DashboardPath
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return zero, fmt.Errorf("begin dashboard preview: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	proposed, err := request.apply(ctx, dbtx, out.Version)
	if err != nil {
		return zero, err
	}
	if proposed != nil {
		// Read inside the rolled-back transaction, so cards the change adds,
		// and the widgets a copy makes, are named too.
		widgets, err := s.management.CardWidgetsInTx(ctx, dbtx, proposed.ID)
		if err != nil {
			return zero, fmt.Errorf("read previewed dashboard widgets: %w", err)
		}
		view := scope.genView(proposed, widgets)
		// Ids the rolled-back preview made are not real yet: any card the
		// change places, and a new dashboard with its widgets.
		existing := map[string]bool{}
		if out.Current != nil {
			for _, card := range out.Current.Cards {
				existing[card.PlacementID] = true
			}
		}
		for i := range view.Cards {
			if !existing[view.Cards[i].PlacementID] {
				view.Cards[i].PlacementID = ""
			}
			if request.creates {
				view.Cards[i].WidgetID = ""
			}
		}
		if request.creates {
			view.ID, view.DashboardPath = "", ""
		}
		out.Proposed = &view
	}

	out.PreviewToken = s.previewToken(previewNonce(s.now()), principal, scope.project, request.operation, out.Version, encoded)
	return out, nil
}

// previewToken binds a preview to its nonce, the organization, user, project,
// operation, version and exact normalized proposal. The nonce makes every
// preview distinct, so a confirmed preview can be held to one idempotency key.
func (s *DashboardsService) previewToken(nonce string, principal Principal, project ResolvedProject, operation, version string, proposal []byte) string {
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte("platform-dashboard-write-v1\x00"))
	for _, part := range []string{nonce, principal.OrganizationID, principal.UserID, project.ID.String(), operation, version} {
		_, _ = mac.Write([]byte(part))
		_, _ = mac.Write([]byte{0})
	}
	_, _ = mac.Write(proposal)
	return joinPreviewToken(nonce, mac.Sum(nil))
}

// projectDashboardTarget returns the dashboard id a write that changes a
// project dashboard names, refusing a Speakeasy-built slug readably.
func projectDashboardTarget(id string) (string, error) {
	if _, err := uuid.Parse(id); err == nil {
		return id, nil
	}
	if _, ok := dashboards.BuiltInDashboard(id); ok {
		return "", errDashboardBuiltInReadOnly
	}
	return "", oops.E(oops.CodeBadRequest, nil, "dashboard_id is not a dashboard id; use the id list_dashboards returns")
}

func isOopsCode(err error, code oops.Code) bool {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	return ok && shareable.Code == code
}

func (scope dashboardScope) dashboardPath(id string) string {
	if scope.organizationSlug == "" || id == "" {
		return ""
	}
	return "/" + scope.organizationSlug + "/projects/" + scope.project.Slug + "/dashboards/" + id
}

func (scope dashboardScope) builtInPath(slug string) string {
	return scope.dashboardPath("builtin/" + slug)
}

func (scope dashboardScope) detailView(detail dashboards.Detail) dashboardView {
	view := scope.genView(detail.Dashboard, detail.Widgets)
	view.Version = detail.Version
	return view
}

func (scope dashboardScope) genView(dashboard *gen.Dashboard, widgets map[string]dashboards.CardWidget) dashboardView {
	view := dashboardView{
		ID: dashboard.ID, Slug: "", BuiltIn: false, Name: dashboard.Name, Description: dashboard.Description,
		CreatedByUserID: dashboard.CreatedByUserID, UpdatedAt: dashboard.UpdatedAt, Version: "",
		Filters: dashboardFiltersView(dashboard.Filters), Cards: make([]dashboardCard, 0, len(dashboard.Widgets)), DashboardPath: scope.dashboardPath(dashboard.ID),
	}
	for _, card := range dashboard.Widgets {
		widget := widgets[card.WidgetID]
		view.Cards = append(view.Cards, dashboardCard{PlacementID: card.ID, WidgetID: card.WidgetID, WidgetName: widget.Name, Dataset: widget.Dataset, ChartType: widget.ChartType, X: card.X, Y: card.Y, W: card.W, H: card.H})
	}
	return view
}

func (scope dashboardScope) builtInView(page *gen.BuiltInDashboard) dashboardView {
	description := page.Description
	view := dashboardView{
		ID: "", Slug: page.Slug, BuiltIn: true, Name: page.Name, Description: &description,
		CreatedByUserID: nil, UpdatedAt: "", Version: dashboardBuiltInVersionTag + page.Slug,
		Filters: nil, Cards: make([]dashboardCard, 0, len(page.Cards)), DashboardPath: scope.builtInPath(page.Slug),
	}
	for _, card := range page.Cards {
		view.Cards = append(view.Cards, dashboardCard{PlacementID: "", WidgetID: "", WidgetName: card.Name, Dataset: card.Dataset, ChartType: dashboards.BuiltInChartType(card), X: card.X, Y: card.Y, W: card.W, H: card.H})
	}
	return view
}

func dashboardFiltersView(filters *gen.DashboardFilters) *dashboardFilters {
	if filters == nil || (filters.Range == nil && len(filters.Values) == 0) {
		return nil
	}
	out := &dashboardFilters{Range: nil, Values: filters.Values}
	if r := filters.Range; r != nil {
		out.Range = &dashboardRange{Preset: r.Preset, From: r.From, To: r.To, Label: r.Label}
	}
	return out
}
