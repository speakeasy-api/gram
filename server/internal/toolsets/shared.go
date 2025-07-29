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

type HTTPToolExecutionInfo struct {
	Tool             *gateway.HTTPTool
	OrganizationSlug string
	ProjectSlug      string
}

func (t *Toolsets) GetHTTPToolExecutionInfoByID(ctx context.Context, id uuid.UUID, projectID uuid.UUID) (*HTTPToolExecutionInfo, error) {
	tool, err := t.toolsRepo.GetHTTPToolDefinitionByID(ctx, toolsRepo.GetHTTPToolDefinitionByIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get http tool definition by id: %w", err)
	}

	relevantSecurityKeysMap := make(map[string]bool)
	securityKeys, securityScopes, err := security.ParseHTTPToolSecurityKeys(tool.Security)
	if err != nil {
		return nil, fmt.Errorf("parse http tool security keys: %w", err)
	}
	for _, key := range securityKeys {
		relevantSecurityKeysMap[key] = true
	}

	securityEntries, err := t.repo.GetHTTPSecurityDefinitions(ctx, repo.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(relevantSecurityKeysMap)),
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

	gatewayTool := &gateway.HTTPTool{
		ID:                 tool.ID.String(),
		DeploymentID:       tool.DeploymentID.String(),
		ProjectID:          tool.ProjectID.String(),
		OrganizationID:     orgData.ID,
		Name:               tool.Name,
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

	return &HTTPToolExecutionInfo{
		Tool:             gatewayTool,
		OrganizationSlug: orgData.Slug,
		ProjectSlug:      orgData.ProjectSlug,
	}, nil
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
