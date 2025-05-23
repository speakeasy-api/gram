package mv

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/database"
	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/oops"
	tr "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/tools/security"
	tsr "github.com/speakeasy-api/gram/internal/toolsets/repo"
	vr "github.com/speakeasy-api/gram/internal/variations/repo"
)

func DescribeToolset(
	ctx context.Context,
	logger *slog.Logger,
	tx database.DBTX,
	projectID ProjectID,
	toolsetSlug ToolsetSlug,
) (*types.Toolset, error) {
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

	var httpTools []*types.HTTPToolDefinition
	var relevantEnvVars []string
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
		for _, def := range definitions {
			if _, ok := seen[def.HttpToolDefinition.Name]; ok {
				continue
			}
			seen[def.HttpToolDefinition.ID.String()] = true

			var canonical *types.CanonicalToolAttributes
			name := def.HttpToolDefinition.Name
			summary := def.HttpToolDefinition.Summary
			description := def.HttpToolDefinition.Description
			confirmRaw := conv.PtrValOr(conv.FromPGText[string](def.HttpToolDefinition.Confirm), "")
			confirmPrompt := conv.FromPGText[string](def.HttpToolDefinition.ConfirmPrompt)
			tags := def.HttpToolDefinition.Tags
			variations, ok := keyedVariations[def.HttpToolDefinition.Name]
			if ok {
				name = conv.PtrValOrEmpty(variations.Name, name)
				summary = conv.PtrValOr(variations.Summary, summary)
				description = conv.PtrValOr(variations.Description, description)
				confirmRaw = conv.PtrValOrEmpty(variations.Confirm, confirmRaw)
				confirmPrompt = conv.Default(variations.ConfirmPrompt, confirmPrompt)
				if len(variations.Tags) > 0 {
					tags = variations.Tags
				}

				canonical = &types.CanonicalToolAttributes{
					VariationID:   variations.ID,
					Name:          def.HttpToolDefinition.Name,
					Summary:       conv.PtrEmpty(def.HttpToolDefinition.Summary),
					Description:   conv.PtrEmpty(def.HttpToolDefinition.Description),
					Tags:          def.HttpToolDefinition.Tags,
					Confirm:       conv.FromPGText[string](def.HttpToolDefinition.Confirm),
					ConfirmPrompt: conv.FromPGText[string](def.HttpToolDefinition.ConfirmPrompt),
				}
			}

			confirm, _ := SanitizeConfirm(confirmRaw)

			httpTools = append(httpTools, &types.HTTPToolDefinition{
				ID:                  def.HttpToolDefinition.ID.String(),
				ProjectID:           def.HttpToolDefinition.Description,
				DeploymentID:        def.HttpToolDefinition.DeploymentID.String(),
				Openapiv3DocumentID: conv.FromNullableUUID(def.HttpToolDefinition.Openapiv3DocumentID),
				Name:                name,
				Summary:             summary,
				Description:         description,
				Confirm:             string(confirm),
				ConfirmPrompt:       confirmPrompt,
				Tags:                tags,
				Openapiv3Operation:  conv.FromPGText[string](def.HttpToolDefinition.Openapiv3Operation),
				Security:            conv.FromBytes(def.HttpToolDefinition.Security),
				HTTPMethod:          def.HttpToolDefinition.HttpMethod,
				Path:                def.HttpToolDefinition.Path,
				SchemaVersion:       &def.HttpToolDefinition.SchemaVersion,
				Schema:              string(def.HttpToolDefinition.Schema),
				CreatedAt:           def.HttpToolDefinition.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           def.HttpToolDefinition.UpdatedAt.Time.Format(time.RFC3339),
				Canonical:           canonical,
			})
		}

		relevantEnvVars, err = environmentVariablesForTools(ctx, tx, definitions)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get environment variables for toolset").Log(ctx, logger)
		}
	}

	return &types.Toolset{
		ID:                           toolset.ID.String(),
		OrganizationID:               toolset.OrganizationID,
		ProjectID:                    toolset.ProjectID.String(),
		Name:                         toolset.Name,
		Slug:                         types.Slug(toolset.Slug),
		DefaultEnvironmentSlug:       conv.FromPGText[types.Slug](toolset.DefaultEnvironmentSlug),
		RelevantEnvironmentVariables: relevantEnvVars,
		Description:                  conv.FromPGText[string](toolset.Description),
		HTTPTools:                    httpTools,
		McpSlug:                      conv.FromPGText[types.Slug](toolset.McpSlug),
		McpIsPublic:                  &toolset.McpIsPublic,
		CreatedAt:                    toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                    toolset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func environmentVariablesForTools(ctx context.Context, tx database.DBTX, tools []tr.FindToolsByNameRow) ([]string, error) {
	if len(tools) == 0 {
		return []string{}, nil
	}

	toolsetRepo := tsr.New(tx)

	relevantSecurityKeysMap := make(map[string]bool)
	serverEnvVarsMap := make(map[string]bool)
	for _, tool := range tools {
		securityKeys, err := security.ParseHTTPToolSecurityKeys(tool.HttpToolDefinition.Security)
		if err != nil {
			return nil, err
		}

		for _, key := range securityKeys {
			relevantSecurityKeysMap[key] = true
		}

		if tool.HttpToolDefinition.ServerEnvVar != "" {
			serverEnvVarsMap[tool.HttpToolDefinition.ServerEnvVar] = true
		}
	}

	uniqueDeploymentIDs := make(map[uuid.UUID]bool)
	for _, tool := range tools {
		uniqueDeploymentIDs[tool.HttpToolDefinition.DeploymentID] = true
	}

	securityEntries, err := toolsetRepo.GetHTTPSecurityDefinitions(ctx, tsr.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(relevantSecurityKeysMap)),
		DeploymentIds: slices.Collect(maps.Keys(uniqueDeploymentIDs)), // all selected tools share the same deployment
	})
	if err != nil {
		return nil, err
	}

	relevantEnvVarsMap := make(map[string]bool)
	for _, entry := range securityEntries {
		for _, envVar := range entry.EnvVariables {
			relevantEnvVarsMap[envVar] = true
		}
	}

	for key := range serverEnvVarsMap {
		relevantEnvVarsMap[key] = true
	}

	return slices.Collect(maps.Keys(relevantEnvVarsMap)), nil
}
