package mv

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	deploymentR "github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	oauth "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	org "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	templatesR "github.com/speakeasy-api/gram/server/internal/templates/repo"
	tr "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/security"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	vr "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// functionManifestVariable represents a variable definition from a function manifest.
type functionManifestVariable struct {
	Description *string `json:"description"`
}

func DescribeToolsetEntry(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	projectID ProjectID,
	toolsetSlug ToolsetSlug,
) (*types.ToolsetEntry, error) {
	toolsetRepo := tsr.New(tx)
	toolsRepo := tr.New(tx)
	variationsRepo := vr.New(tx)
	templatesRepo := templatesR.New(tx)
	pid := uuid.UUID(projectID)

	if err := inv.Check(
		"describe toolset inputs",
		"project id is set", pid != uuid.Nil,
		"toolset slug is set", toolsetSlug != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "not enough information to describe toolset").Log(ctx, logger)
	}

	toolset, err := toolsetRepo.GetToolset(ctx, tsr.GetToolsetParams{
		Slug:      conv.ToLower(toolsetSlug),
		ProjectID: pid,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load toolset").Log(ctx, logger)
	}

	// Get tool URNs from latest toolset version
	var toolUrns []string
	latestVersion, err := toolsetRepo.GetLatestToolsetVersion(ctx, toolset.ID)
	if err == nil {
		toolUrns = make([]string, len(latestVersion.ToolUrns))
		for i, urn := range latestVersion.ToolUrns {
			toolUrns[i] = urn.String()
		}
	}

	var tools []*types.ToolEntry
	var securityVars []*types.SecurityVariable
	var serverVars []*types.ServerVariable
	var functionEnvVars []*types.FunctionEnvironmentVariable
	if len(toolUrns) > 0 {
		definitions, err := toolsRepo.FindHttpToolEntriesByUrn(ctx, tr.FindHttpToolEntriesByUrnParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools in toolset").Log(ctx, logger)
		}

		names := make([]string, 0, len(definitions))
		for _, def := range definitions {
			names = append(names, def.Name)
		}

		// TODO variations by urns
		allVariations, err := variationsRepo.FindGlobalVariationsByToolNames(ctx, vr.FindGlobalVariationsByToolNamesParams{
			ProjectID: pid,
			ToolNames: names,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list global tool variations").Log(ctx, logger)
		}

		nameVariations := make(map[string]string)
		for _, variation := range allVariations {
			n := conv.FromPGText[string](variation.Name)
			if n == nil || *n == "" {
				continue
			}

			nameVariations[variation.SrcToolName] = *n
		}

		tools = make([]*types.ToolEntry, 0, len(definitions))
		envQueries := make([]toolEnvLookupParams, 0, len(definitions))
		seen := make(map[string]bool, 0)
		for _, def := range definitions {
			if _, ok := seen[def.Name]; ok {
				continue
			}
			seen[def.ID.String()] = true

			name := conv.Default(nameVariations[def.Name], def.Name)

			tool := &types.ToolEntry{
				Type:    string(urn.ToolKindHTTP),
				ID:      def.ID.String(),
				Name:    name,
				ToolUrn: def.ToolUrn.String(),
			}

			envQueries = append(envQueries, toolEnvLookupParams{
				deploymentID: def.DeploymentID,
				security:     def.Security,
				serverEnvVar: def.ServerEnvVar,
			})

			tools = append(tools, tool)
		}

		funcTools, err := toolsRepo.FindFunctionToolEntriesByUrn(ctx, tr.FindFunctionToolEntriesByUrnParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list function tools in toolset").Log(ctx, logger)
		}
		for _, tool := range funcTools {
			tools = append(tools, &types.ToolEntry{
				Type:    string(urn.ToolKindFunction),
				ID:      tool.ID.String(),
				Name:    tool.Name,
				ToolUrn: tool.ToolUrn.String(),
			})

			envVars, err := extractFunctionEnvVars(ctx, logger, tool.Variables)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to extract function environment variables").Log(ctx, logger)
			}
			functionEnvVars = append(functionEnvVars, envVars...)
		}

		promptTools, err := templatesRepo.PeekTemplatesByUrns(ctx, templatesR.PeekTemplatesByUrnsParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get prompt templates").Log(ctx, logger)
		}

		for _, pt := range promptTools {
			tools = append(tools, &types.ToolEntry{
				Type:    string(urn.ToolKindPrompt),
				ID:      pt.ID.String(),
				Name:    pt.Name,
				ToolUrn: pt.ToolUrn.String(),
			})
		}

		securityVars, serverVars, err = environmentVariablesForTools(ctx, tx, envQueries)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment variables for toolset").Log(ctx, logger)
		}
	}

	ptrows, err := toolsetRepo.GetPromptTemplatesForToolset(ctx, tsr.GetPromptTemplatesForToolsetParams{
		ProjectID: pid,
		ToolsetID: toolset.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get prompt templates for toolset").Log(ctx, logger)
	}

	promptTemplates := make([]*types.PromptTemplateEntry, 0, len(ptrows))
	for _, pt := range ptrows {
		promptTemplates = append(promptTemplates, &types.PromptTemplateEntry{
			ID:   pt.ID.String(),
			Name: types.Slug(pt.Name),
			Kind: conv.Ptr(parseKind(pt)),
		})
	}

	return &types.ToolsetEntry{
		ID:                           toolset.ID.String(),
		OrganizationID:               toolset.OrganizationID,
		ProjectID:                    toolset.ProjectID.String(),
		Name:                         toolset.Name,
		Slug:                         types.Slug(toolset.Slug),
		DefaultEnvironmentSlug:       conv.FromPGText[types.Slug](toolset.DefaultEnvironmentSlug),
		SecurityVariables:            securityVars,
		ServerVariables:              serverVars,
		FunctionEnvironmentVariables: functionEnvVars,
		Description:                  conv.FromPGText[string](toolset.Description),
		McpSlug:                      conv.FromPGText[types.Slug](toolset.McpSlug),
		McpEnabled:                   &toolset.McpEnabled,
		CustomDomainID:               conv.FromNullableUUID(toolset.CustomDomainID),
		McpIsPublic:                  &toolset.McpIsPublic,
		CreatedAt:                    toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                    toolset.UpdatedAt.Time.Format(time.RFC3339),
		Tools:                        tools,
		PromptTemplates:              promptTemplates,
		ToolUrns:                     toolUrns,
	}, nil
}

func DescribeToolset(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	projectID ProjectID,
	toolsetSlug ToolsetSlug,
	toolsetCache *cache.TypedCacheObject[ToolsetTools],
) (*types.Toolset, error) {
	toolsetRepo := tsr.New(tx)
	orgRepo := org.New(tx)
	pid := uuid.UUID(projectID)
	oauthRepo := oauth.New(tx)
	deploymentRepo := deploymentR.New(tx)

	if err := inv.Check(
		"describe toolset inputs",
		"project id is set", pid != uuid.Nil,
		"toolset slug is set", toolsetSlug != "",
	); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "not enough information to describe toolset").Log(ctx, logger)
	}

	toolset, err := toolsetRepo.GetToolset(ctx, tsr.GetToolsetParams{
		Slug:      conv.ToLower(toolsetSlug),
		ProjectID: pid,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "toolset not found").Log(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load toolset").Log(ctx, logger)
	}

	// TODO: It would be better if every query below accepted a deployment ID as a parameter to guarantee cache consistency.
	activeDeploymentID, err := deploymentRepo.GetActiveDeploymentID(ctx, pid)
	if err != nil {
		// We only log this because we only need to know this for the cache
		logger.ErrorContext(ctx, "failed to get active deployment id", attr.SlogError(err))
	}

	// Get tool URNs from latest toolset version
	var toolUrns []string
	var toolsetVersion int64
	latestVersion, err := toolsetRepo.GetLatestToolsetVersion(ctx, toolset.ID)
	if err == nil {
		toolUrns = make([]string, len(latestVersion.ToolUrns))
		for i, urn := range latestVersion.ToolUrns {
			toolUrns[i] = urn.String()
		}
		toolsetVersion = latestVersion.Version
	}

	toolsetTools, err := readToolsetTools(ctx, logger, tx, pid, activeDeploymentID, toolset.ID, toolsetVersion, toolUrns, toolsetCache)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get toolset tools").Log(ctx, logger)
	}

	err = ApplyVariations(ctx, logger, tx, pid, toolsetTools.Tools)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to apply variations to toolset").Log(ctx, logger)
	}

	ptrows, err := toolsetRepo.GetPromptTemplatesForToolset(ctx, tsr.GetPromptTemplatesForToolsetParams{
		ProjectID: pid,
		ToolsetID: toolset.ID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get prompt templates for toolset").Log(ctx, logger)
	}

	promptTemplates := make([]*types.PromptTemplate, 0, len(ptrows))
	for _, pt := range ptrows {
		hint := pt.ToolsHint
		if hint == nil {
			hint = []string{}
		}

		promptTemplates = append(promptTemplates, &types.PromptTemplate{
			ID:            pt.ID.String(),
			ToolUrn:       pt.ToolUrn,
			HistoryID:     pt.HistoryID.String(),
			PredecessorID: conv.FromNullableUUID(pt.PredecessorID),
			Name:          pt.Name,
			Prompt:        pt.Prompt,
			Description:   conv.PtrValOrEmpty(conv.FromPGText[string](pt.Description), ""),
			Schema:        string(pt.Arguments),
			SchemaVersion: nil,
			Engine:        conv.PtrValOrEmpty(conv.FromPGText[string](pt.Engine), "none"),
			Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](pt.Kind), "prompt"),
			ToolsHint:     hint,
			CreatedAt:     pt.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     pt.UpdatedAt.Time.Format(time.RFC3339),
			ProjectID:     pt.ProjectID.String(),
			CanonicalName: pt.Name,
			Confirm:       nil,
			ConfirmPrompt: nil,
			Summarizer:    nil,
			Canonical:     nil,
			Variation:     nil,
		})
	}

	var externalOAuthServer *types.ExternalOAuthServer
	var oauthProxyServer *types.OAuthProxyServer

	if toolset.ExternalOauthServerID.Valid {
		externalOauthMetadata, err := oauthRepo.GetExternalOAuthServerMetadata(ctx, oauth.GetExternalOAuthServerMetadataParams{
			ProjectID: pid,
			ID:        toolset.ExternalOauthServerID.UUID,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get external oauth server metadata").Log(ctx, logger)
		}
		if len(externalOauthMetadata.Metadata) > 0 {
			var metadata interface{}
			if err := json.Unmarshal(externalOauthMetadata.Metadata, &metadata); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to unmarshal external oauth metadata").Log(ctx, logger)
			}

			externalOAuthServer = &types.ExternalOAuthServer{
				ID:        externalOauthMetadata.ID.String(),
				ProjectID: externalOauthMetadata.ProjectID.String(),
				Slug:      types.Slug(externalOauthMetadata.Slug),
				Metadata:  metadata,
				CreatedAt: externalOauthMetadata.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt: externalOauthMetadata.UpdatedAt.Time.Format(time.RFC3339),
			}
		}
	}

	if toolset.OauthProxyServerID.Valid {
		oauthProxyServerData, err := oauthRepo.GetOAuthProxyServer(ctx, oauth.GetOAuthProxyServerParams{
			ProjectID: pid,
			ID:        toolset.OauthProxyServerID.UUID,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get oauth proxy server").Log(ctx, logger)
		}
		if err == nil {
			oauthProxyProviders, err := oauthRepo.ListOAuthProxyProvidersByServer(ctx, oauth.ListOAuthProxyProvidersByServerParams{
				ProjectID:          pid,
				OauthProxyServerID: oauthProxyServerData.ID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to get oauth proxy providers").Log(ctx, logger)
			}

			providers := make([]*types.OAuthProxyProvider, 0, len(oauthProxyProviders))
			for _, provider := range oauthProxyProviders {
				providers = append(providers, &types.OAuthProxyProvider{
					ID:                                provider.ID.String(),
					Slug:                              types.Slug(provider.Slug),
					AuthorizationEndpoint:             provider.AuthorizationEndpoint,
					TokenEndpoint:                     provider.TokenEndpoint,
					ScopesSupported:                   provider.ScopesSupported,
					GrantTypesSupported:               provider.GrantTypesSupported,
					TokenEndpointAuthMethodsSupported: provider.TokenEndpointAuthMethodsSupported,
					CreatedAt:                         provider.CreatedAt.Time.Format(time.RFC3339),
					UpdatedAt:                         provider.UpdatedAt.Time.Format(time.RFC3339),
				})
			}

			oauthProxyServer = &types.OAuthProxyServer{
				ID:                  oauthProxyServerData.ID.String(),
				ProjectID:           oauthProxyServerData.ProjectID.String(),
				Slug:                types.Slug(oauthProxyServerData.Slug),
				OauthProxyProviders: providers,
				CreatedAt:           oauthProxyServerData.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           oauthProxyServerData.UpdatedAt.Time.Format(time.RFC3339),
			}
		}
	}

	orgMetadata, err := orgRepo.GetOrganizationMetadata(ctx, toolset.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization metadata").Log(ctx, logger)
	}

	result := &types.Toolset{
		ID:                           toolset.ID.String(),
		OrganizationID:               toolset.OrganizationID,
		AccountType:                  orgMetadata.GramAccountType,
		ProjectID:                    toolset.ProjectID.String(),
		Name:                         toolset.Name,
		Slug:                         types.Slug(toolset.Slug),
		DefaultEnvironmentSlug:       conv.FromPGText[types.Slug](toolset.DefaultEnvironmentSlug),
		SecurityVariables:            toolsetTools.SecurityVars,
		ServerVariables:              toolsetTools.ServerVars,
		FunctionEnvironmentVariables: toolsetTools.FunctionEnvVars,
		Description:                  conv.FromPGText[string](toolset.Description),
		Tools:                        toolsetTools.Tools,
		PromptTemplates:              promptTemplates,
		McpSlug:                      conv.FromPGText[types.Slug](toolset.McpSlug),
		McpEnabled:                   &toolset.McpEnabled,
		CustomDomainID:               conv.FromNullableUUID(toolset.CustomDomainID),
		McpIsPublic:                  &toolset.McpIsPublic,
		CreatedAt:                    toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                    toolset.UpdatedAt.Time.Format(time.RFC3339),
		ToolUrns:                     toolUrns,
		ExternalOauthServer:          externalOAuthServer,
		OauthProxyServer:             oauthProxyServer,
	}

	return result, nil
}

func readToolsetTools(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	pid uuid.UUID,
	activeDeploymentID uuid.UUID,
	toolsetID uuid.UUID,
	toolsetVersion int64,
	toolUrns []string,
	toolsetCache *cache.TypedCacheObject[ToolsetTools],
) (*ToolsetTools, error) {
	toolsRepo := tr.New(tx)
	templatesRepo := templatesR.New(tx)

	var tools []*types.Tool
	var securityVars []*types.SecurityVariable
	var serverVars []*types.ServerVariable
	var functionEnvVars []*types.FunctionEnvironmentVariable

	// NOTE: A slight shortcoming here is that the cache is keyed by the active deployment id, but the queries below don't strictly depend on
	// the deployment ID fetched above. Technically the deployment could change at just the right time to mess up the cache.
	if toolsetCache != nil && activeDeploymentID != uuid.Nil {
		if cached, cacheErr := toolsetCache.Get(ctx, ToolsetCacheKey(toolsetID.String(), activeDeploymentID.String(), toolsetVersion)); cacheErr == nil {
			return &cached, nil
		}
	}

	if len(toolUrns) > 0 {
		definitions, err := toolsRepo.FindHttpToolsByUrn(ctx, tr.FindHttpToolsByUrnParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools in toolset").Log(ctx, logger)
		}

		tools = make([]*types.Tool, 0, len(definitions))
		seen := make(map[string]bool, 0)
		envQueries := make([]toolEnvLookupParams, 0, len(definitions))
		for _, def := range definitions {
			if _, ok := seen[def.HttpToolDefinition.Name]; ok {
				continue
			}
			seen[def.HttpToolDefinition.ID.String()] = true

			name := def.HttpToolDefinition.Name
			description := def.HttpToolDefinition.Description
			confirmRaw := conv.PtrValOr(conv.FromPGText[string](def.HttpToolDefinition.Confirm), "")
			confirmPrompt := conv.FromPGText[string](def.HttpToolDefinition.ConfirmPrompt)
			summarizer := conv.FromPGText[string](def.HttpToolDefinition.Summarizer)
			tags := def.HttpToolDefinition.Tags

			confirm, _ := SanitizeConfirm(confirmRaw)

			var responseFilter *types.ResponseFilter
			if def.HttpToolDefinition.ResponseFilter != nil {
				responseFilter = &types.ResponseFilter{
					Type:         string(def.HttpToolDefinition.ResponseFilter.Type),
					StatusCodes:  def.HttpToolDefinition.ResponseFilter.StatusCodes,
					ContentTypes: def.HttpToolDefinition.ResponseFilter.ContentTypes,
				}
			}

			tool := &types.HTTPToolDefinition{
				ID:                  def.HttpToolDefinition.ID.String(),
				ToolUrn:             def.HttpToolDefinition.ToolUrn.String(),
				ProjectID:           def.HttpToolDefinition.Description,
				DeploymentID:        def.HttpToolDefinition.DeploymentID.String(),
				Openapiv3DocumentID: conv.FromNullableUUID(def.HttpToolDefinition.Openapiv3DocumentID),
				Name:                name,
				CanonicalName:       name,
				Summary:             "", // Slowly phasing this out
				Description:         description,
				Confirm:             conv.Ptr(string(confirm)),
				ConfirmPrompt:       confirmPrompt,
				Summarizer:          summarizer,
				Tags:                tags,
				Openapiv3Operation:  conv.FromPGText[string](def.HttpToolDefinition.Openapiv3Operation),
				Security:            conv.FromBytes(def.HttpToolDefinition.Security),
				DefaultServerURL:    conv.FromPGText[string](def.HttpToolDefinition.DefaultServerUrl),
				HTTPMethod:          def.HttpToolDefinition.HttpMethod,
				Path:                def.HttpToolDefinition.Path,
				SchemaVersion:       &def.HttpToolDefinition.SchemaVersion,
				Schema:              string(def.HttpToolDefinition.Schema),
				CreatedAt:           def.HttpToolDefinition.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           def.HttpToolDefinition.UpdatedAt.Time.Format(time.RFC3339),
				PackageName:         &def.PackageName,
				ResponseFilter:      responseFilter,
				Variation:           nil, // Applied later
				Canonical:           nil,
			}

			envQueries = append(envQueries, toolEnvLookupParams{
				deploymentID: def.HttpToolDefinition.DeploymentID,
				security:     def.HttpToolDefinition.Security,
				serverEnvVar: def.HttpToolDefinition.ServerEnvVar,
			})

			tools = append(tools, &types.Tool{
				HTTPToolDefinition: tool,
			})
		}

		promptTools, err := templatesRepo.FindPromptTemplatesByUrns(ctx, templatesR.FindPromptTemplatesByUrnsParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get prompt templates for toolset").Log(ctx, logger)
		}

		for _, pt := range promptTools {
			tools = append(tools, &types.Tool{
				PromptTemplate: &types.PromptTemplate{
					ID:            pt.ID.String(),
					ToolUrn:       pt.ToolUrn.String(),
					HistoryID:     pt.HistoryID.String(),
					PredecessorID: conv.FromNullableUUID(pt.PredecessorID),
					Name:          pt.Name,
					Prompt:        pt.Prompt,
					Description:   conv.PtrValOrEmpty(conv.FromPGText[string](pt.Description), ""),
					Schema:        string(pt.Arguments),
					SchemaVersion: nil,
					Engine:        conv.PtrValOrEmpty(conv.FromPGText[string](pt.Engine), "none"),
					Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](pt.Kind), "prompt"),
					ToolsHint:     pt.ToolsHint,
					CreatedAt:     pt.CreatedAt.Time.Format(time.RFC3339),
					UpdatedAt:     pt.UpdatedAt.Time.Format(time.RFC3339),
					ProjectID:     pt.ProjectID.String(),
					CanonicalName: pt.Name,
					Confirm:       nil,
					ConfirmPrompt: nil,
					Summarizer:    nil,
					Canonical:     nil,
					Variation:     nil,
				},
			})
		}

		functionDefinitions, err := toolsRepo.FindFunctionToolsByUrn(ctx, tr.FindFunctionToolsByUrnParams{
			ProjectID: pid,
			Urns:      toolUrns,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get function tools for toolset").Log(ctx, logger)
		}

		for _, def := range functionDefinitions {
			project := ""
			if projectID := conv.FromNullableUUID(def.FunctionToolDefinition.ProjectID); projectID != nil {
				project = *projectID
			}
			functionTool := &types.FunctionToolDefinition{
				ID:            def.FunctionToolDefinition.ID.String(),
				ToolUrn:       def.FunctionToolDefinition.ToolUrn.String(),
				ProjectID:     project,
				DeploymentID:  def.FunctionToolDefinition.DeploymentID.String(),
				FunctionID:    def.FunctionToolDefinition.FunctionID.String(),
				Runtime:       def.FunctionToolDefinition.Runtime,
				Name:          def.FunctionToolDefinition.Name,
				CanonicalName: def.FunctionToolDefinition.Name,
				Description:   def.FunctionToolDefinition.Description,
				Variables:     def.FunctionToolDefinition.Variables,
				SchemaVersion: nil,
				Schema:        string(def.FunctionToolDefinition.InputSchema),
				Confirm:       nil,
				ConfirmPrompt: nil,
				Summarizer:    nil,
				CreatedAt:     def.FunctionToolDefinition.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:     def.FunctionToolDefinition.UpdatedAt.Time.Format(time.RFC3339),
				Canonical:     nil,
				Variation:     nil,
			}
			if functionTool.Schema == "" {
				functionTool.Schema = constants.DefaultEmptyToolSchema
			}

			envVars, err := extractFunctionEnvVars(ctx, logger, def.FunctionToolDefinition.Variables)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to extract function environment variables").Log(ctx, logger)
			}
			functionEnvVars = append(functionEnvVars, envVars...)

			tools = append(tools, &types.Tool{
				FunctionToolDefinition: functionTool,
			})
		}

		securityVars, serverVars, err = environmentVariablesForTools(ctx, tx, envQueries)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment variables for toolset").Log(ctx, logger)
		}
	}

	toolsetTools := ToolsetTools{
		DeploymentID:    activeDeploymentID.String(),
		ToolsetID:       toolsetID.String(),
		Version:         toolsetVersion,
		Tools:           tools,
		SecurityVars:    securityVars,
		ServerVars:      serverVars,
		FunctionEnvVars: functionEnvVars,
	}

	if toolsetCache != nil && activeDeploymentID != uuid.Nil {
		if err := toolsetCache.Store(ctx, toolsetTools); err != nil {
			logger.ErrorContext(ctx, "failed to cache toolset", attr.SlogError(err))
		}
	}

	return &toolsetTools, nil
}

func ApplyVariations(ctx context.Context, logger *slog.Logger, tx DBTX, projectID uuid.UUID, tools []*types.Tool) error {
	variationsRepo := vr.New(tx)

	names := make([]string, 0, len(tools))
	for _, def := range tools {
		baseTool := conv.ToBaseTool(def)
		names = append(names, baseTool.Name)
	}

	// TODO variations by urns
	allVariations, err := variationsRepo.FindGlobalVariationsByToolNames(ctx, vr.FindGlobalVariationsByToolNamesParams{
		ProjectID: projectID,
		ToolNames: names,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to list global tool variations").Log(ctx, logger)
	}

	keyedVariations := make(map[string]types.ToolVariation, len(allVariations))
	for _, variation := range allVariations {
		keyedVariations[variation.SrcToolName] = types.ToolVariation{
			ID:            variation.ID.String(),
			GroupID:       variation.GroupID.String(),
			SrcToolName:   variation.SrcToolName,
			Confirm:       conv.FromPGText[string](variation.Confirm),
			ConfirmPrompt: conv.FromPGText[string](variation.ConfirmPrompt),
			Name:          conv.FromPGText[string](variation.Name),
			Description:   conv.FromPGText[string](variation.Description),
			Summarizer:    conv.FromPGText[string](variation.Summarizer),
			CreatedAt:     variation.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     variation.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	for _, tool := range tools {
		if tool == nil {
			continue
		}
		baseTool := conv.ToBaseTool(tool)

		v, ok := keyedVariations[baseTool.Name]
		if ok {
			conv.ApplyVariation(*tool, v)
		}
	}

	return nil
}

func extractFunctionEnvVars(ctx context.Context, logger *slog.Logger, variableData []byte) ([]*types.FunctionEnvironmentVariable, error) {
	var functionEnvVars []*types.FunctionEnvironmentVariable

	if variableData != nil {
		var variables map[string]*functionManifestVariable
		if err := json.Unmarshal(variableData, &variables); err != nil {
			logger.ErrorContext(ctx, "failed to unmarshal function tool variables", attr.SlogError(err))
		} else {
			for k, v := range variables {
				var description *string
				if v != nil && v.Description != nil {
					description = v.Description
				}
				functionEnvVars = append(functionEnvVars, &types.FunctionEnvironmentVariable{
					Name:        k,
					Description: description,
				})
			}

		}
	}

	return functionEnvVars, nil
}

type toolEnvLookupParams struct {
	// The deployment ID of the tool.
	deploymentID uuid.UUID

	// The security requirements for the tool.
	security []byte

	// The server environment variable for the tool if available.
	serverEnvVar string
}

func environmentVariablesForTools(ctx context.Context, tx DBTX, tools []toolEnvLookupParams) ([]*types.SecurityVariable, []*types.ServerVariable, error) {
	if len(tools) == 0 {
		return []*types.SecurityVariable{}, []*types.ServerVariable{}, nil
	}

	toolsetRepo := tsr.New(tx)

	securityKeysMap := make(map[string]bool)
	serverEnvVarsMap := make(map[string]bool)
	for _, tool := range tools {
		securityKeys, _, err := security.ParseHTTPToolSecurityKeys(tool.security)
		if err != nil {
			return nil, nil, fmt.Errorf("http tool security keys: %w", err)
		}

		for _, key := range securityKeys {
			securityKeysMap[key] = true
		}

		if tool.serverEnvVar != "" {
			serverEnvVarsMap[tool.serverEnvVar] = true
		}
	}

	uniqueDeploymentIDs := make(map[uuid.UUID]bool)
	for _, tool := range tools {
		uniqueDeploymentIDs[tool.deploymentID] = true
	}

	securityEntries, err := toolsetRepo.GetHTTPSecurityDefinitions(ctx, tsr.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(securityKeysMap)),
		DeploymentIds: slices.Collect(maps.Keys(uniqueDeploymentIDs)), // all selected tools share the same deployment
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read toolset security definitions: %w", err)
	}

	// Build security variables map to avoid duplicates
	securityVarsMap := make(map[string]*types.SecurityVariable)
	for _, entry := range securityEntries {
		key := entry.Key
		if _, exists := securityVarsMap[key]; !exists {
			securityVar := &types.SecurityVariable{
				Type:         conv.FromPGText[string](entry.Type),
				Name:         entry.Name.String,
				InPlacement:  entry.InPlacement.String,
				Scheme:       entry.Scheme.String,
				BearerFormat: conv.FromPGText[string](entry.BearerFormat),
				OauthTypes:   entry.OauthTypes,
				OauthFlows:   entry.OauthFlows,
				EnvVariables: entry.EnvVariables,
			}

			securityVarsMap[key] = securityVar
		}
	}

	// Build server variables
	var serverVars []*types.ServerVariable
	if len(serverEnvVarsMap) > 0 {
		serverVars = append(serverVars, &types.ServerVariable{
			Description:  "",
			EnvVariables: slices.Collect(maps.Keys(serverEnvVarsMap)),
		})
	}

	return slices.Collect(maps.Values(securityVarsMap)), serverVars, nil
}

const defaultPromptTemplateKind = "prompt"

func parseKind(pt tsr.GetPromptTemplatesForToolsetRow) string {
	rawKind := conv.FromPGText[string](pt.Kind)
	kind := conv.PtrValOrEmpty(rawKind, defaultPromptTemplateKind)

	return kind
}
