//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	ErrDataExportConfirmationRequired = errors.New("data export creation requires confirmation")
	ErrDataExportInvalidInput         = errors.New("invalid data export input")
	ErrDataExportRouteConflict        = errors.New("a data export route already exists for this source")
)

type dataExportMutationService struct {
	db           *pgxpool.Pool
	audit        *audit.Logger
	dashboardURL *url.URL
}

// WithDataExportMutations enables creation after the read dependencies have
// been configured. Secret headers remain dashboard-only.
func (r *PostgresReader) WithDataExportMutations(auditLogger *audit.Logger, dashboardURL *url.URL) *PostgresReader {
	if r != nil && r.db != nil && auditLogger != nil && validDashboardURL(dashboardURL) {
		copyURL := *dashboardURL
		r.dataExportMutations = &dataExportMutationService{db: r.db, audit: auditLogger, dashboardURL: &copyURL}
	}
	return r
}

type CreateDataExportInput struct {
	ProjectID     string `json:"project_id,omitempty" jsonschema:"project ID that will export telemetry; supply exactly one project selector"`
	ProjectSlug   string `json:"project_slug,omitempty" jsonschema:"project slug that will export telemetry; supply exactly one project selector"`
	Name          string `json:"name" jsonschema:"display name for the OTEL destination"`
	EndpointURL   string `json:"endpoint_url" jsonschema:"HTTP or HTTPS OTEL collector endpoint without credentials, query parameters, or fragments"`
	DataSource    string `json:"data_source" jsonschema:"data to export: product_telemetry or risk_findings"`
	SensitiveData string `json:"sensitive_data,omitempty" jsonschema:"whether sensitive fields are included: exclude (default) or include"`
	Enabled       *bool  `json:"enabled,omitempty" jsonschema:"whether delivery starts immediately; defaults to true"`
	Confirmed     bool   `json:"confirmed" jsonschema:"true only after the user explicitly confirms the project, endpoint, data source, enabled state, and sensitive-data policy"`
}

type CreateDataExportOutput struct {
	Destination   DataExportDestination `json:"destination"`
	Route         DataExportRoute       `json:"route"`
	ManagementURL string                `json:"management_url"`
}

func (r *PostgresReader) CreateDataExport(ctx context.Context, principal Principal, input CreateDataExportInput) (CreateDataExportOutput, error) {
	if r == nil || r.reader == nil || r.dataExportMutations == nil {
		return CreateDataExportOutput{}, ErrUnavailable
	}
	if !input.Confirmed {
		return CreateDataExportOutput{}, ErrDataExportConfirmationRequired
	}
	if (input.ProjectID == "") == (input.ProjectSlug == "") {
		return CreateDataExportOutput{}, fmt.Errorf("%w: exactly one of project_id or project_slug is required", ErrDataExportInvalidInput)
	}
	if input.ProjectID != "" {
		if _, parseErr := uuid.Parse(input.ProjectID); parseErr != nil {
			return CreateDataExportOutput{}, fmt.Errorf("%w: invalid project_id: %w", ErrDataExportInvalidInput, parseErr)
		}
	}
	project, err := r.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: input.ProjectSlug})
	if err != nil {
		return CreateDataExportOutput{}, err
	}
	organization, err := organizationsrepo.New(r.dataExportMutations.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("resolve data export organization: %w", err)
	}

	sensitiveData := input.SensitiveData
	if sensitiveData == "" {
		sensitiveData = "exclude"
	}
	configuration, err := dataexports.NormalizeDestinationConfiguration(input.Name, input.EndpointURL, sensitiveData)
	if err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("%w: validate data export destination: %w", ErrDataExportInvalidInput, err)
	}
	dataSource, err := dataexports.NormalizeDataSource(input.DataSource)
	if err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("%w: validate data_source: %w", ErrDataExportInvalidInput, err)
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}

	tx, err := r.dataExportMutations.db.Begin(ctx)
	if err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("begin data export creation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := dataexportsrepo.New(tx)
	destination, err := queries.CreateOtelDestination(ctx, dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		Name:             configuration.Name,
		EndpointUrl:      configuration.EndpointURL,
		HeadersEncrypted: pgtype.Text{},
		SensitiveData:    pgtype.Text{String: configuration.SensitiveData, Valid: true},
	})
	if err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("create data export destination: %w", err)
	}
	route, err := queries.CreateDataExportRoute(ctx, dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    principal.OrganizationID,
		ProjectID:         project.ID,
		DataSource:        dataSource,
		Enabled:           enabled,
		OtelDestinationID: uuid.NullUUID{UUID: destination.ID, Valid: true},
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "data_export_routes_project_source_key" {
			return CreateDataExportOutput{}, ErrDataExportRouteConflict
		}
		return CreateDataExportOutput{}, fmt.Errorf("create data export route: %w", err)
	}
	actor := urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID)
	if err := r.dataExportMutations.audit.LogOtelDestinationCreate(ctx, tx, audit.LogOtelDestinationCreateEvent{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		Actor:            actor,
		ActorDisplayName: nil,
		ActorSlug:        nil,
		DestinationURN:   urn.NewOtelDestination(destination.ID),
		DestinationName:  destination.Name,
	}); err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("audit data export destination creation: %w", err)
	}
	if err := r.dataExportMutations.audit.LogDataExportRouteCreate(ctx, tx, audit.LogDataExportRouteCreateEvent{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		Actor:            actor,
		ActorDisplayName: nil,
		ActorSlug:        nil,
		RouteURN:         urn.NewDataExportRoute(route.ID),
		DataSource:       route.DataSource,
	}); err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("audit data export route creation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return CreateDataExportOutput{}, fmt.Errorf("commit data export creation: %w", err)
	}

	managementURL := r.dataExportMutations.dashboardURL.JoinPath(organization.Slug, "data", "exports").String()
	return CreateDataExportOutput{
		Destination: DataExportDestination{
			ID:            destination.ID.String(),
			ProjectID:     project.ID.String(),
			ProjectName:   project.Name,
			ProjectSlug:   project.Slug,
			Name:          destination.Name,
			Type:          "otel",
			EndpointURL:   destination.EndpointUrl,
			SensitiveData: configuration.SensitiveData,
			Headers:       []DataExportHeader{},
		},
		Route: DataExportRoute{
			ID:            route.ID.String(),
			ProjectID:     project.ID.String(),
			ProjectName:   project.Name,
			ProjectSlug:   project.Slug,
			DataSource:    route.DataSource,
			Enabled:       route.Enabled,
			DestinationID: destination.ID.String(),
		},
		ManagementURL: managementURL,
	}, nil
}

type dataExportMutationRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func registerDataExportMutationTool(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name:        "create_data_export",
		Title:       "Create a Data Export",
		Description: "Create one OTEL destination and connect one project's product telemetry or risk findings to it. Before calling, show the user the exact project, endpoint, data source, enabled state, and whether sensitive fields are included, then obtain explicit confirmation. Constraints: header secrets are never accepted in chat; the destination is created without headers and the returned management URL is where the user securely adds any required authentication. One route per project and data source is allowed.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreateDataExportInput) (*mcp.CallToolResult, CreateDataExportOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, CreateDataExportOutput{}, err
		}
		output, err := reader.CreateDataExport(ctx, principal, input)
		if err != nil {
			if result, ok := dataExportMutationToolResult(err); ok {
				return result, CreateDataExportOutput{}, nil
			}
			return nil, CreateDataExportOutput{}, err
		}
		return nil, output, nil
	})
}

func registerUnavailableDataExportMutationTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "create_data_export",
		Title:       "Create a Data Export",
		Description: "Create an OTEL data export after explicit confirmation. This is not switched on for your organization yet.",
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("data_export_mutations"))
}

func dataExportMutationToolResult(err error) (*mcp.CallToolResult, bool) {
	var refusal dataExportMutationRefusal
	switch {
	case errors.Is(err, ErrDataExportConfirmationRequired):
		refusal = dataExportMutationRefusal{Code: "confirmation_required", Message: "Show the exact project, endpoint, data source, enabled state, and sensitive-data policy, then ask the user to confirm before creating the export."}
	case errors.Is(err, ErrDataExportRouteConflict):
		refusal = dataExportMutationRefusal{Code: "conflict", Message: "This project already has an export route for that data source. Read the current exports and manage the existing route instead."}
	case errors.Is(err, ErrDataExportInvalidInput):
		refusal = dataExportMutationRefusal{Code: "invalid_input", Message: "Choose exactly one existing project, provide a valid HTTP or HTTPS endpoint without credentials, query parameters, or fragments, and use a supported data source and sensitive-data policy."}
	case errors.Is(err, ErrForbidden):
		refusal = dataExportMutationRefusal{Code: "project_not_found", Message: "No accessible project matched that project ID or slug. Read the project inventory and choose one of the returned projects."}
	default:
		return nil, false
	}
	content, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}, IsError: true}, true
}
