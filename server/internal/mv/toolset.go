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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	oauth "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	org "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	tr "github.com/speakeasy-api/gram/server/internal/tools/repo"
	"github.com/speakeasy-api/gram/server/internal/tools/security"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	vr "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

const DefaultEmptyToolSchema = `{"type":"object","properties":{}}`

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

	var httpTools []*types.HTTPToolDefinitionEntry
	var securityVars []*types.SecurityVariable
	var serverVars []*types.ServerVariable
	if len(toolset.HttpToolNames) > 0 {
		definitions, err := toolsRepo.FindToolEntriesByName(ctx, tr.FindToolEntriesByNameParams{
			ProjectID:    pid,
			Names:        toolset.HttpToolNames,
			DeploymentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools in toolset").Log(ctx, logger)
		}

		names := make([]string, 0, len(definitions))
		for _, def := range definitions {
			names = append(names, def.Name)
		}

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

		httpTools = make([]*types.HTTPToolDefinitionEntry, 0, len(definitions))
		envQueries := make([]toolEnvLookupParams, 0, len(definitions))
		seen := make(map[string]bool, 0)
		for _, def := range definitions {
			if _, ok := seen[def.Name]; ok {
				continue
			}
			seen[def.ID.String()] = true

			name := conv.Default(nameVariations[def.Name], def.Name)

			tool := &types.HTTPToolDefinitionEntry{
				ID:   def.ID.String(),
				Name: name,
			}

			envQueries = append(envQueries, toolEnvLookupParams{
				deploymentID: def.DeploymentID,
				security:     def.Security,
				serverEnvVar: def.ServerEnvVar,
			})

			httpTools = append(httpTools, tool)
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
		ID:                     toolset.ID.String(),
		OrganizationID:         toolset.OrganizationID,
		ProjectID:              toolset.ProjectID.String(),
		Name:                   toolset.Name,
		Slug:                   types.Slug(toolset.Slug),
		DefaultEnvironmentSlug: conv.FromPGText[types.Slug](toolset.DefaultEnvironmentSlug),
		SecurityVariables:      securityVars,
		ServerVariables:        serverVars,
		Description:            conv.FromPGText[string](toolset.Description),
		McpSlug:                conv.FromPGText[types.Slug](toolset.McpSlug),
		McpEnabled:             &toolset.McpEnabled,
		CustomDomainID:         conv.FromNullableUUID(toolset.CustomDomainID),
		McpIsPublic:            &toolset.McpIsPublic,
		CreatedAt:              toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              toolset.UpdatedAt.Time.Format(time.RFC3339),
		HTTPTools:              httpTools,
		PromptTemplates:        promptTemplates,
	}, nil
}

func DescribeToolset(
	ctx context.Context,
	logger *slog.Logger,
	tx DBTX,
	projectID ProjectID,
	toolsetSlug ToolsetSlug,
) (*types.Toolset, error) {
	toolsetRepo := tsr.New(tx)
	orgRepo := org.New(tx)
	toolsRepo := tr.New(tx)
	variationsRepo := vr.New(tx)
	pid := uuid.UUID(projectID)
	oauthRepo := oauth.New(tx)

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

	var httpTools []*types.HTTPToolDefinition
	var securityVars []*types.SecurityVariable
	var serverVars []*types.ServerVariable
	if len(toolset.HttpToolNames) > 0 {
		definitions, err := toolsRepo.FindToolsByName(ctx, tr.FindToolsByNameParams{
			ProjectID:    pid,
			Names:        toolset.HttpToolNames,
			DeploymentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list tools in toolset").Log(ctx, logger)
		}

		names := make([]string, 0, len(definitions))
		for _, def := range definitions {
			names = append(names, def.HttpToolDefinition.Name)
		}

		allVariations, err := variationsRepo.FindGlobalVariationsByToolNames(ctx, vr.FindGlobalVariationsByToolNamesParams{
			ProjectID: pid,
			ToolNames: names,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to list global tool variations").Log(ctx, logger)
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
				Summary:       conv.FromPGText[string](variation.Summary),
				Description:   conv.FromPGText[string](variation.Description),
				Tags:          variation.Tags,
				Summarizer:    conv.FromPGText[string](variation.Summarizer),
				CreatedAt:     variation.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:     variation.UpdatedAt.Time.Format(time.RFC3339),
			}
		}

		httpTools = make([]*types.HTTPToolDefinition, 0, len(definitions))
		seen := make(map[string]bool, 0)
		envQueries := make([]toolEnvLookupParams, 0, len(definitions))
		for _, def := range definitions {
			if _, ok := seen[def.HttpToolDefinition.Name]; ok {
				continue
			}
			seen[def.HttpToolDefinition.ID.String()] = true

			var variation *types.ToolVariation
			var canonical *types.CanonicalToolAttributes

			name := def.HttpToolDefinition.Name
			summary := def.HttpToolDefinition.Summary
			description := def.HttpToolDefinition.Description
			confirmRaw := conv.PtrValOr(conv.FromPGText[string](def.HttpToolDefinition.Confirm), "")
			confirmPrompt := conv.FromPGText[string](def.HttpToolDefinition.ConfirmPrompt)
			summarizer := conv.FromPGText[string](def.HttpToolDefinition.Summarizer)
			tags := def.HttpToolDefinition.Tags

			variations, ok := keyedVariations[def.HttpToolDefinition.Name]
			if ok {
				name = conv.PtrValOrEmpty(variations.Name, name)
				summary = conv.PtrValOr(variations.Summary, summary)
				description = conv.PtrValOr(variations.Description, description)
				confirmRaw = conv.PtrValOrEmpty(variations.Confirm, confirmRaw)
				confirmPrompt = conv.Default(variations.ConfirmPrompt, confirmPrompt)
				summarizer = conv.Default(variations.Summarizer, summarizer)
				if len(variations.Tags) > 0 {
					tags = variations.Tags
				}

				variation = &variations
				canonical = &types.CanonicalToolAttributes{
					VariationID:   variations.ID,
					Name:          def.HttpToolDefinition.Name,
					Summary:       conv.PtrEmpty(def.HttpToolDefinition.Summary),
					Description:   conv.PtrEmpty(def.HttpToolDefinition.Description),
					Tags:          def.HttpToolDefinition.Tags,
					Confirm:       conv.FromPGText[string](def.HttpToolDefinition.Confirm),
					ConfirmPrompt: conv.FromPGText[string](def.HttpToolDefinition.ConfirmPrompt),
					Summarizer:    conv.FromPGText[string](def.HttpToolDefinition.Summarizer),
				}
			}

			canonicalName := name
			if canonical != nil {
				canonicalName = canonical.Name
			}

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
				ProjectID:           def.HttpToolDefinition.Description,
				DeploymentID:        def.HttpToolDefinition.DeploymentID.String(),
				Openapiv3DocumentID: conv.FromNullableUUID(def.HttpToolDefinition.Openapiv3DocumentID),
				Name:                name,
				CanonicalName:       canonicalName,
				Summary:             summary,
				Description:         description,
				Confirm:             string(confirm),
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
				Canonical:           canonical,
				Variation:           variation,
				PackageName:         &def.PackageName,
				ResponseFilter:      responseFilter,
			}

			if newSchema, err := variedToolSchema(ctx, logger, tool); err == nil {
				tool.Schema = newSchema
			}

			// models like claude expect schema to never be empty but be a valid json schema
			if tool.Schema == "" {
				tool.Schema = DefaultEmptyToolSchema
			}

			envQueries = append(envQueries, toolEnvLookupParams{
				deploymentID: def.HttpToolDefinition.DeploymentID,
				security:     def.HttpToolDefinition.Security,
				serverEnvVar: def.HttpToolDefinition.ServerEnvVar,
			})

			httpTools = append(httpTools, tool)
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

	promptTemplates := make([]*types.PromptTemplate, 0, len(ptrows))
	for _, pt := range ptrows {
		var args *string
		if len(pt.Arguments) > 0 {
			args = conv.PtrEmpty(string(pt.Arguments))
		}

		hint := pt.ToolsHint
		if hint == nil {
			hint = []string{}
		}

		promptTemplates = append(promptTemplates, &types.PromptTemplate{
			ID:            pt.ID.String(),
			HistoryID:     pt.HistoryID.String(),
			PredecessorID: conv.FromNullableUUID(pt.PredecessorID),
			Name:          types.Slug(pt.Name),
			Prompt:        pt.Prompt,
			Description:   conv.FromPGText[string](pt.Description),
			Arguments:     args,
			Engine:        conv.PtrValOrEmpty(conv.FromPGText[string](pt.Engine), "none"),
			Kind:          conv.PtrValOrEmpty(conv.FromPGText[string](pt.Kind), "prompt"),
			ToolsHint:     hint,
			CreatedAt:     pt.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:     pt.UpdatedAt.Time.Format(time.RFC3339),
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

	return &types.Toolset{
		ID:                     toolset.ID.String(),
		OrganizationID:         toolset.OrganizationID,
		AccountType:            orgMetadata.GramAccountType,
		ProjectID:              toolset.ProjectID.String(),
		Name:                   toolset.Name,
		Slug:                   types.Slug(toolset.Slug),
		DefaultEnvironmentSlug: conv.FromPGText[types.Slug](toolset.DefaultEnvironmentSlug),
		SecurityVariables:      securityVars,
		ServerVariables:        serverVars,
		Description:            conv.FromPGText[string](toolset.Description),
		HTTPTools:              httpTools,
		PromptTemplates:        promptTemplates,
		McpSlug:                conv.FromPGText[types.Slug](toolset.McpSlug),
		McpEnabled:             &toolset.McpEnabled,
		CustomDomainID:         conv.FromNullableUUID(toolset.CustomDomainID),
		McpIsPublic:            &toolset.McpIsPublic,
		CreatedAt:              toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:              toolset.UpdatedAt.Time.Format(time.RFC3339),
		ExternalOauthServer:    externalOAuthServer,
		OauthProxyServer:       oauthProxyServer,
	}, nil
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

func variedToolSchema(ctx context.Context, logger *slog.Logger, tool *types.HTTPToolDefinition) (string, error) {
	schema := tool.Schema
	if tool.Summarizer != nil {
		var jsonSchema map[string]interface{}
		err := json.Unmarshal([]byte(schema), &jsonSchema)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to unmarshal schema").Log(ctx, logger)
		}

		properties, ok := jsonSchema["properties"].(map[string]interface{})
		if !ok {
			properties = make(map[string]interface{})
		}

		properties["gram-request-summary"] = map[string]interface{}{
			"type":        "string",
			"description": "REQUIRED: A summary of the request to the tool. Distill the user's intention in order to ensure the response contains all the necessary information, without unnecessary details.",
		}

		jsonSchema["properties"] = properties

		var required []string
		required, ok = jsonSchema["required"].([]string)
		if !ok {
			required = []string{}
		}

		required = append(required, "gram-request-summary")
		jsonSchema["required"] = required

		newSchema, err := json.Marshal(jsonSchema)
		if err != nil {
			return "", oops.E(oops.CodeUnexpected, err, "failed to marshal schema").Log(ctx, logger)
		}

		schema = string(newSchema)
	}

	return schema, nil
}

const defaultPromptTemplateKind = "prompt"

func parseKind(pt tsr.GetPromptTemplatesForToolsetRow) string {
	rawKind := conv.FromPGText[string](pt.Kind)
	kind := conv.PtrValOrEmpty(rawKind, defaultPromptTemplateKind)

	return kind
}
