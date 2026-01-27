package toolsets

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"
	deploymentsRepo "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	externalmcpRepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/functions"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	resourcesRepo "github.com/speakeasy-api/gram/server/internal/resources/repo"
	templatesRepo "github.com/speakeasy-api/gram/server/internal/templates/repo"
	toolsRepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/security"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo            *repo.Queries
	toolsRepo       *toolsRepo.Queries
	templatesRepo   *templatesRepo.Queries
	resourcesRepo   *resourcesRepo.Queries
	projects        *projectsRepo.Queries
	externalmcpRepo *externalmcpRepo.Queries
	deploymentsRepo *deploymentsRepo.Queries
}

func NewToolsets(tx repo.DBTX) *Toolsets {
	return &Toolsets{
		repo:            repo.New(tx),
		toolsRepo:       toolsRepo.New(tx),
		templatesRepo:   templatesRepo.New(tx),
		resourcesRepo:   resourcesRepo.New(tx),
		projects:        projectsRepo.New(tx),
		externalmcpRepo: externalmcpRepo.New(tx),
		deploymentsRepo: deploymentsRepo.New(tx),
	}
}

func (t *Toolsets) GetToolCallPlanByURN(ctx context.Context, toolUrn urn.Tool, projectID uuid.UUID) (*gateway.ToolCallPlan, error) {
	switch toolUrn.Kind {
	case urn.ToolKindHTTP:
		tool, err := t.toolsRepo.GetHTTPToolDefinitionByURN(ctx, toolsRepo.GetHTTPToolDefinitionByURNParams{
			ProjectID: projectID,
			Urn:       toolUrn,
		})
		if err != nil {
			return nil, fmt.Errorf("get http tool definition by urn: %w", err)
		}
		return t.extractHTTPToolCallPlan(ctx, tool)

	case urn.ToolKindFunction:
		tool, err := t.toolsRepo.GetFunctionToolByURN(ctx, toolsRepo.GetFunctionToolByURNParams{
			ProjectID: projectID,
			Urn:       toolUrn,
		})
		if err != nil {
			return nil, fmt.Errorf("get function tool definition by urn: %w", err)
		}
		return t.extractFunctionToolCallPlan(ctx, tool)

	case urn.ToolKindPrompt:
		tool, err := t.templatesRepo.GetTemplateByURN(ctx, templatesRepo.GetTemplateByURNParams{
			ProjectID: projectID,
			Urn:       toolUrn.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("get prompt template by urn: %w", err)
		}
		return t.extractPromptToolCallPlan(ctx, tool)

	case urn.ToolKindExternalMCP:
		tool, err := t.externalmcpRepo.GetExternalMCPToolDefinitionByURN(ctx, externalmcpRepo.GetExternalMCPToolDefinitionByURNParams{
			ToolUrn:   toolUrn.String(),
			ProjectID: projectID,
		})
		if err != nil {
			return nil, fmt.Errorf("get external mcp tool definition by urn: %w", err)
		}
		return t.extractExternalMCPToolCallPlan(ctx, tool, toolUrn, projectID)

	default:
		return nil, fmt.Errorf("unsupported tool kind: %s", toolUrn.Kind)
	}
}

func (t *Toolsets) extractHTTPToolCallPlan(ctx context.Context, tool toolsRepo.HttpToolDefinition) (*gateway.ToolCallPlan, error) {
	securityKeysMap := make(map[string]bool)
	securityKeys, securityScopes, err := security.ParseHTTPToolSecurityKeys(tool.Security)
	if err != nil {
		return nil, fmt.Errorf("parse http tool security keys: %w", err)
	}
	for _, key := range securityKeys {
		securityKeysMap[key] = true
	}

	securityEntries, err := t.repo.GetHTTPSecurityDefinitions(ctx, repo.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(securityKeysMap)),
		DeploymentIds: []uuid.UUID{tool.DeploymentID},
	})
	if err != nil {
		return nil, fmt.Errorf("get http security definitions: %w", err)
	}

	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, tool.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	sec := make([]*gateway.HTTPToolSecurity, 0, len(securityEntries))
	for _, entry := range securityEntries {
		sec = append(sec, &gateway.HTTPToolSecurity{
			ID:           entry.ID.String(),
			Key:          entry.Key,
			Type:         gateway.NullString{Valid: entry.Type.Valid, Value: entry.Type.String},
			Scheme:       gateway.NullString{Valid: entry.Scheme.Valid, Value: entry.Scheme.String},
			Name:         gateway.NullString{Valid: entry.Name.Valid, Value: entry.Name.String},
			Placement:    gateway.NullString{Valid: entry.InPlacement.Valid, Value: entry.InPlacement.String},
			OAuthTypes:   entry.OauthTypes,
			OAuthFlows:   entry.OauthFlows,
			EnvVariables: entry.EnvVariables,
		})
	}

	var filter *gateway.ResponseFilter
	if tool.ResponseFilter != nil {
		typ, err := gateway.NewFilterType(string(tool.ResponseFilter.Type))
		if err != nil {
			return nil, fmt.Errorf("invalid response filter type: %w", err)
		}

		filter = &gateway.ResponseFilter{
			Type:         typ,
			Schema:       tool.ResponseFilter.Schema,
			StatusCodes:  tool.ResponseFilter.StatusCodes,
			ContentTypes: tool.ResponseFilter.ContentTypes,
		}
	}

	pathParams, err := UnmarshalParameterSettings(tool.PathSettings)
	if err != nil {
		return nil, fmt.Errorf("parse path settings: %w", err)
	}

	queryParams, err := UnmarshalParameterSettings(tool.QuerySettings)
	if err != nil {
		return nil, fmt.Errorf("parse path settings: %w", err)
	}
	headerParams, err := UnmarshalParameterSettings(tool.HeaderSettings)
	if err != nil {
		return nil, fmt.Errorf("parse path settings: %w", err)
	}

	descriptor := &gateway.ToolDescriptor{
		ID:               tool.ID.String(),
		Name:             tool.Name,
		Description:      &tool.Description,
		DeploymentID:     tool.DeploymentID.String(),
		ProjectID:        tool.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		URN:              tool.ToolUrn,
	}
	plan := &gateway.HTTPToolCallPlan{
		DefaultServerUrl:   gateway.NullString{Valid: tool.DefaultServerUrl.Valid, Value: tool.DefaultServerUrl.String},
		ServerEnvVar:       tool.ServerEnvVar,
		Method:             tool.HttpMethod,
		Path:               trimFragment(tool.Path),
		Schema:             tool.Schema,
		PathParams:         pathParams,
		QueryParams:        queryParams,
		HeaderParams:       headerParams,
		RequestContentType: gateway.NullString{Valid: tool.RequestContentType.Valid, Value: tool.RequestContentType.String},
		Security:           sec,
		SecurityScopes:     securityScopes,
		ResponseFilter:     filter,
	}

	return gateway.NewHTTPToolCallPlan(descriptor, plan), nil
}

func (t *Toolsets) extractFunctionToolCallPlan(ctx context.Context, tool toolsRepo.GetFunctionToolByURNRow) (*gateway.ToolCallPlan, error) {
	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, tool.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	accessID := ""
	if tool.AccessID.Valid && tool.AccessID.UUID != uuid.Nil {
		accessID = tool.AccessID.UUID.String()
	} else {
		return nil, fmt.Errorf("missing function access credentials")
	}

	var envconfig map[string]*functions.ManifestVariableAttributeV0
	if len(tool.Variables) > 0 {
		if err := json.Unmarshal(tool.Variables, &envconfig); err != nil {
			return nil, fmt.Errorf("unmarshal function tool env vars: %w", err)
		}
	}

	var authInput functions.ManifestAuthInputAttributeV0
	if len(tool.AuthInput) > 0 {
		if err := json.Unmarshal(tool.AuthInput, &authInput); err != nil {
			return nil, fmt.Errorf("unmarshal function tool auth input: %w", err)
		}
	}

	descriptor := &gateway.ToolDescriptor{
		ID:               tool.ID.String(),
		Name:             tool.Name,
		Description:      &tool.Description,
		DeploymentID:     tool.DeploymentID.String(),
		ProjectID:        tool.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		URN:              tool.ToolUrn,
	}
	plan := &gateway.FunctionToolCallPlan{
		FunctionID:        tool.FunctionID.String(),
		FunctionsAccessID: accessID,
		Runtime:           tool.Runtime,
		InputSchema:       tool.InputSchema,
		Variables:         envconfig,
		AuthInput:         &authInput,
	}

	return gateway.NewFunctionToolCallPlan(descriptor, plan), nil
}

func (t *Toolsets) extractPromptToolCallPlan(ctx context.Context, tool templatesRepo.PromptTemplate) (*gateway.ToolCallPlan, error) {
	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, tool.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	var description *string
	if tool.Description.Valid {
		description = &tool.Description.String
	}

	descriptor := &gateway.ToolDescriptor{
		ID:               tool.ID.String(),
		Name:             tool.Name,
		Description:      description,
		ProjectID:        tool.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		URN:              tool.ToolUrn,
		DeploymentID:     "", // Prompts have no deployment ID
	}

	var engine string
	if tool.Engine.Valid {
		engine = tool.Engine.String
	}

	plan := &gateway.PromptToolCallPlan{
		TemplateID: tool.ID.String(),
		Engine:     engine,
		Prompt:     tool.Prompt,
		Kind:       tool.Kind.String,
	}
	return gateway.NewPromptToolCallPlan(descriptor, plan), nil
}

func (t *Toolsets) extractExternalMCPToolCallPlan(ctx context.Context, tool externalmcpRepo.GetExternalMCPToolDefinitionByURNRow, toolUrn urn.Tool, projectID uuid.UUID) (*gateway.ToolCallPlan, error) {
	// Get organization metadata
	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	description := fmt.Sprintf("External MCP proxy tool for %s", tool.Name.String)

	descriptor := &gateway.ToolDescriptor{
		ID:               tool.ID.String(),
		Name:             tool.Slug, // Use slug as name since this is the proxy identifier
		Description:      &description,
		DeploymentID:     tool.DeploymentID.String(),
		ProjectID:        projectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		URN:              toolUrn,
	}

	// Note: The ToolName field is "proxy" for proxy tools. Actual external tool names
	// are resolved at runtime when the tool is called (e.g., "notion:search" -> ToolName="search").
	plan := &gateway.ExternalMCPToolCallPlan{
		RemoteURL:     tool.RemoteUrl,
		TransportType: tool.TransportType,
		ToolName:      toolUrn.Name, // "proxy" for proxy tools, actual tool name for direct calls
		Slug:          tool.Slug,
		RequiresOAuth: tool.RequiresOauth,
	}

	return gateway.NewExternalMCPToolCallPlan(descriptor, plan), nil
}

func UnmarshalParameterSettings(settings []byte) (map[string]*gateway.HTTPParameter, error) {
	if len(settings) == 0 {
		return map[string]*gateway.HTTPParameter{}, nil
	}

	parsed := make(map[string]*openapi.OpenapiV3ParameterProxy)
	if err := json.Unmarshal(settings, &parsed); err != nil {
		return nil, fmt.Errorf("parse parameter settings: %w", err)
	}

	out := make(map[string]*gateway.HTTPParameter, len(parsed))
	for k, v := range parsed {
		out[k] = &gateway.HTTPParameter{
			Name:            v.Name,
			Style:           v.Style,
			Explode:         v.Explode,
			AllowEmptyValue: v.AllowEmptyValue,
		}
	}

	return out, nil
}

func (t *Toolsets) GetResourceCallPlanByURN(ctx context.Context, resourceUrn urn.Resource, projectID uuid.UUID) (*gateway.ResourceCallPlan, error) {
	switch resourceUrn.Kind {
	case urn.ResourceKindFunction:
		resource, err := t.resourcesRepo.GetFunctionResourceByURN(ctx, resourcesRepo.GetFunctionResourceByURNParams{
			ProjectID: projectID,
			Urn:       resourceUrn,
		})
		if err != nil {
			return nil, fmt.Errorf("get function resource definition by urn: %w", err)
		}
		return t.extractFunctionResourceCallPlan(ctx, resource)

	default:
		return nil, fmt.Errorf("unsupported resource kind: %s", resourceUrn.Kind)
	}
}

func (t *Toolsets) extractFunctionResourceCallPlan(ctx context.Context, resource resourcesRepo.GetFunctionResourceByURNRow) (*gateway.ResourceCallPlan, error) {
	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, resource.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	var envconfig map[string]any
	if len(resource.Variables) > 0 {
		if err := json.Unmarshal(resource.Variables, &envconfig); err != nil {
			return nil, fmt.Errorf("unmarshal function resource env vars: %w", err)
		}
	}

	envvars := slices.Collect(maps.Keys(envconfig))

	mimeType := ""
	if resource.MimeType.Valid {
		mimeType = resource.MimeType.String
	}

	descriptor := &gateway.ResourceDescriptor{
		ID:               resource.ID.String(),
		Name:             resource.Name,
		DeploymentID:     resource.DeploymentID.String(),
		ProjectID:        resource.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		URN:              resource.ResourceUrn,
		URI:              resource.Uri,
	}
	accessID := ""
	if resource.AccessID.Valid {
		accessID = resource.AccessID.UUID.String()
	}

	plan := &gateway.ResourceFunctionCallPlan{
		FunctionID:        resource.FunctionID.String(),
		FunctionsAccessID: accessID,
		Runtime:           resource.Runtime,
		URI:               resource.Uri,
		MimeType:          mimeType,
		Variables:         envvars,
	}

	return gateway.NewResourceFunctionCallPlan(descriptor, plan), nil
}

// trimFragment removes the fragment from a URL by trimming everything after '#'.
// Fragments are client-side only and should not be sent to servers.
// https://datatracker.ietf.org/doc/html/rfc3986#section-3.5 a fragment should always end the URL and there should only be one included.
func trimFragment(path string) string {
	if idx := strings.Index(path, "#"); idx != -1 {
		return path[:idx]
	}
	return path
}
