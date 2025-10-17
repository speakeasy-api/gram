package toolsets

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/openapi"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	toolsRepo "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/security"
	"github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo      *repo.Queries
	toolsRepo *toolsRepo.Queries
	projects  *projectsRepo.Queries
}

func NewToolsets(tx repo.DBTX) *Toolsets {
	return &Toolsets{
		repo:      repo.New(tx),
		toolsRepo: toolsRepo.New(tx),
		projects:  projectsRepo.New(tx),
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

	default:
		return nil, fmt.Errorf("unsupported tool kind: %s", toolUrn.Kind)
	}
}

// TODO: should we consider moving /rpc/instances.invoke/tool onto URNs, only the playground uses it right now.
func (t *Toolsets) GetToolCallPlanByID(ctx context.Context, id uuid.UUID, projectID uuid.UUID) (*gateway.ToolCallPlan, error) {
	toolUrnStr, err := t.toolsRepo.GetToolUrnByID(ctx, toolsRepo.GetToolUrnByIDParams{
		ProjectID: projectID,
		ID:        id,
	})
	if err != nil {
		return nil, fmt.Errorf("get tool urn by id: %w", err)
	}

	var toolURN urn.Tool
	err = toolURN.UnmarshalText([]byte(toolUrnStr))
	if err != nil {
		return nil, fmt.Errorf("unmarshal tool urn: %w", err)
	}

	return t.GetToolCallPlanByURN(ctx, toolURN, projectID)
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
		URN:              tool.ToolUrn,
		DeploymentID:     tool.DeploymentID.String(),
		ProjectID:        tool.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		Name:             tool.Name,
	}
	plan := &gateway.HTTPToolCallPlan{
		DefaultServerUrl:   gateway.NullString{Valid: tool.DefaultServerUrl.Valid, Value: tool.DefaultServerUrl.String},
		ServerEnvVar:       tool.ServerEnvVar,
		Method:             tool.HttpMethod,
		Path:               tool.Path,
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

	var envconfig map[string]any
	if len(tool.Variables) > 0 {
		if err := json.Unmarshal(tool.Variables, &envconfig); err != nil {
			return nil, fmt.Errorf("unmarshal function tool env vars: %w", err)
		}
	}

	envvars := slices.Collect(maps.Keys(envconfig))

	descriptor := &gateway.ToolDescriptor{
		ID:               tool.ID.String(),
		URN:              tool.ToolUrn,
		DeploymentID:     tool.DeploymentID.String(),
		ProjectID:        tool.ProjectID.String(),
		ProjectSlug:      orgData.Slug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
		Name:             tool.Name,
	}
	plan := &gateway.FunctionToolCallPlan{
		FunctionID:        tool.FunctionID.String(),
		FunctionsAccessID: accessID,
		Runtime:           tool.Runtime,
		InputSchema:       tool.InputSchema,
		Variables:         envvars,
	}

	return gateway.NewFunctionToolCallPlan(descriptor, plan), nil
}

func (t *Toolsets) GetResourceCallPlanByURN(ctx context.Context, resourceUrn urn.Resource, projectID uuid.UUID) (*gateway.ResourceCallPlan, error) {
	switch resourceUrn.Kind {
	case urn.ResourceKindFunction:
		resource, err := t.toolsRepo.GetFunctionResourceByURN(ctx, toolsRepo.GetFunctionResourceByURNParams{
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

func (t *Toolsets) extractFunctionResourceCallPlan(ctx context.Context, resource toolsRepo.GetFunctionResourceByURNRow) (*gateway.ResourceCallPlan, error) {
	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, resource.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	var envconfig map[string]string
	if len(resource.Variables) > 0 {
		if err := json.Unmarshal(resource.Variables, &envconfig); err != nil {
			return nil, fmt.Errorf("unmarshal function resource env vars: %w", err)
		}
	}

	mimeType := ""
	if resource.MimeType.Valid {
		mimeType = resource.MimeType.String
	}

	descriptor := &gateway.ResourceDescriptor{
		ID:               resource.ID.String(),
		URN:              resource.ResourceUrn,
		Name:             resource.Name,
		URI:              resource.Uri,
		DeploymentID:     resource.DeploymentID.String(),
		ProjectID:        resource.ProjectID.String(),
		ProjectSlug:      orgData.ProjectSlug,
		OrganizationID:   orgData.ID,
		OrganizationSlug: orgData.Slug,
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
		Variables:         envconfig,
	}

	return gateway.NewResourceFunctionCallPlan(descriptor, plan), nil
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
