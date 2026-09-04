//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/dataexports"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// DataExportReadService owns the dependencies needed to project data export
// configuration without exposing stored header values.
type DataExportReadService struct {
	db           *pgxpool.Pool
	encryption   *encryption.Client
	dashboardURL *url.URL
}

func newDataExportReadService(db *pgxpool.Pool, encryptionClient *encryption.Client, dashboardURL *url.URL) *DataExportReadService {
	if db == nil || encryptionClient == nil || !validDashboardURL(dashboardURL) {
		return nil
	}
	copyURL := *dashboardURL
	return &DataExportReadService{db: db, encryption: encryptionClient, dashboardURL: &copyURL}
}

func validDashboardURL(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.Host != "" && value.User == nil
}

// WithDataExports enables the safe data export inventory on this reader.
func (r *PostgresReader) WithDataExports(encryptionClient *encryption.Client, dashboardURL *url.URL) *PostgresReader {
	if r != nil {
		r.dataExports = newDataExportReadService(r.db, encryptionClient, dashboardURL)
	}
	return r
}

type ListDataExportsInput struct {
	ProjectID   string `json:"project_id,omitempty" jsonschema:"optional project ID; omit both project selectors to list the organization's exports"`
	ProjectSlug string `json:"project_slug,omitempty" jsonschema:"optional project slug; omit both project selectors to list the organization's exports"`
	Limit       int    `json:"limit,omitempty" jsonschema:"maximum number of destinations and routes to return in each collection; server clamps this to 100"`
}

type DataExportHeader struct {
	Name     string `json:"name"`
	HasValue bool   `json:"has_value"`
}

type DataExportDestination struct {
	ID            string             `json:"id"`
	ProjectID     string             `json:"project_id"`
	ProjectName   string             `json:"project_name"`
	ProjectSlug   string             `json:"project_slug"`
	Name          string             `json:"name"`
	Type          string             `json:"type"`
	EndpointURL   string             `json:"endpoint_url"`
	SensitiveData string             `json:"sensitive_data"`
	Headers       []DataExportHeader `json:"headers"`
}

type DataExportRoute struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	ProjectName   string `json:"project_name"`
	ProjectSlug   string `json:"project_slug"`
	DataSource    string `json:"data_source"`
	Enabled       bool   `json:"enabled"`
	DestinationID string `json:"destination_id,omitempty"`
}

type ListDataExportsOutput struct {
	Destinations  []DataExportDestination `json:"destinations"`
	Routes        []DataExportRoute       `json:"routes"`
	ManagementURL string                  `json:"management_url"`
	Truncated     bool                    `json:"truncated"`
}

func (r *PostgresReader) ListDataExports(ctx context.Context, principal Principal, input ListDataExportsInput) (ListDataExportsOutput, error) {
	if r == nil || r.reader == nil || r.dataExports == nil {
		return ListDataExportsOutput{}, ErrUnavailable
	}
	if input.ProjectID != "" && input.ProjectSlug != "" {
		return ListDataExportsOutput{}, fmt.Errorf("only one of project_id or project_slug may be supplied")
	}

	organization, err := organizationsrepo.New(r.dataExports.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return ListDataExportsOutput{}, fmt.Errorf("resolve data export organization: %w", err)
	}
	projectID := uuid.NullUUID{}
	if input.ProjectID != "" || input.ProjectSlug != "" {
		selectedProject, resolveErr := r.resolveInventoryProject(ctx, principal.OrganizationID, FindMCPInput{ProjectID: input.ProjectID, ProjectSlug: input.ProjectSlug})
		if resolveErr != nil {
			return ListDataExportsOutput{}, resolveErr
		}
		projectID = uuid.NullUUID{UUID: selectedProject.ID, Valid: true}
	}

	limit := boundedLimit(input.Limit)
	query := dataexportsrepo.New(r.dataExports.db)
	destinations, err := query.ListOtelDestinationsWithProjectMetadata(ctx, dataexportsrepo.ListOtelDestinationsWithProjectMetadataParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		ResultLimit:    int32(limit + 1),
	})
	if err != nil {
		return ListDataExportsOutput{}, fmt.Errorf("list platform data export destinations: %w", err)
	}
	routes, err := query.ListDataExportRoutesWithProjectMetadata(ctx, dataexportsrepo.ListDataExportRoutesWithProjectMetadataParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		ResultLimit:    int32(limit + 1),
	})
	if err != nil {
		return ListDataExportsOutput{}, fmt.Errorf("list platform data export routes: %w", err)
	}

	destinations, destinationsTruncated := boundedRows(destinations, limit)
	routes, routesTruncated := boundedRows(routes, limit)
	output := ListDataExportsOutput{
		Destinations:  make([]DataExportDestination, 0, len(destinations)),
		Routes:        make([]DataExportRoute, 0, len(routes)),
		ManagementURL: r.dataExports.dashboardURL.JoinPath(organization.Slug, "data", "exports").String(),
		Truncated:     destinationsTruncated || routesTruncated,
	}
	for _, row := range destinations {
		headers, decodeErr := dataexports.DecodeDestinationHeaderMetadata(r.dataExports.encryption, row.HeadersEncrypted)
		if decodeErr != nil {
			return ListDataExportsOutput{}, fmt.Errorf("decode data export destination headers: %w", decodeErr)
		}
		policy, policyErr := dataexports.DestinationSensitiveDataPolicy(row.SensitiveData)
		if policyErr != nil {
			return ListDataExportsOutput{}, fmt.Errorf("decode data export sensitive-data policy: %w", policyErr)
		}
		headerOutput := make([]DataExportHeader, 0, len(headers))
		for _, header := range headers {
			headerOutput = append(headerOutput, DataExportHeader{Name: header.Name, HasValue: header.HasValue})
		}
		output.Destinations = append(output.Destinations, DataExportDestination{
			ID:            row.ID.String(),
			ProjectID:     row.ProjectID.String(),
			ProjectName:   row.ProjectName,
			ProjectSlug:   row.ProjectSlug,
			Name:          row.Name,
			Type:          "otel",
			EndpointURL:   row.EndpointUrl,
			SensitiveData: policy,
			Headers:       headerOutput,
		})
	}
	for _, row := range routes {
		output.Routes = append(output.Routes, DataExportRoute{
			ID:            row.ID.String(),
			ProjectID:     row.ProjectID.String(),
			ProjectName:   row.ProjectName,
			ProjectSlug:   row.ProjectSlug,
			DataSource:    row.DataSource,
			Enabled:       row.Enabled,
			DestinationID: uuidString(row.OtelDestinationID),
		})
	}
	return output, nil
}

func registerDataExportTools(reg *Registrar, reader *PostgresReader) {
	addTool(reg, &mcp.Tool{
		Name:        "list_data_exports",
		Title:       "List Data Exports",
		Description: "List the organization's configured OpenTelemetry data exports, optionally narrowed to one project. Returns destinations, routes, enabled state, endpoint URLs, the sensitive-data policy, and only header names plus whether each has a value; secret header values are never returned. The structured route-to-destination relationships can be rendered as a Mermaid diagram. The management URL opens the dashboard where exports can be changed.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListDataExportsInput) (*mcp.CallToolResult, ListDataExportsOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ListDataExportsOutput{}, err
		}
		output, err := reader.ListDataExports(ctx, principal, input)
		return nil, output, err
	})
}

func registerUnavailableDataExportTools(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "list_data_exports",
		Title:       "List Data Exports",
		Description: "List configured OpenTelemetry data exports and the route-to-destination relationships. This is not switched on for your organization yet.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: externalOnly, ProjectScope: ProjectScopeExplicit}, unavailableTool("data_exports"))
}
