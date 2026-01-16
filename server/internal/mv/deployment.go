package mv

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/packages/semver"
)

func DescribeDeployment(ctx context.Context, logger *slog.Logger, depRepo *repo.Queries, projectID ProjectID, depID DeploymentID) (*types.Deployment, error) {
	rows, err := depRepo.GetDeploymentWithAssets(ctx, repo.GetDeploymentWithAssetsParams{
		ID:        uuid.UUID(depID),
		ProjectID: uuid.UUID(projectID),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && len(rows) == 0:
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting deployment with assets").Log(ctx, logger)
	}

	deployment := rows[0].Deployment
	stat := rows[0].Status
	openapiv3ToolCount := rows[0].Openapiv3ToolCount
	functionsToolCount := rows[0].FunctionsToolCount
	attachedOpenAPIv3 := make([]*types.OpenAPIv3DeploymentAsset, 0, len(rows))
	attachedFunctionsAssets := make([]*types.DeploymentFunctions, 0, len(rows))
	attachedPackages := make([]*types.DeploymentPackage, 0, len(rows))
	attachedExternalMCPs := make([]*types.DeploymentExternalMCP, 0, len(rows))
	var seenOpenAPIv3 = make(map[uuid.UUID]bool)
	var seenFunctions = make(map[uuid.UUID]bool)
	var seenPackages = make(map[uuid.UUID]bool)
	var seenExternalMCPs = make(map[uuid.UUID]bool)

	for _, r := range rows {
		depOpenAPIv3ID := r.DeploymentsOpenapiv3AssetID.UUID
		if depOpenAPIv3ID != uuid.Nil && !seenOpenAPIv3[depOpenAPIv3ID] {
			if err := inv.Check(
				"describe deployment openapiv3 asset",
				"valid asset store id", r.DeploymentsOpenapiv3AssetStoreID.Valid && r.DeploymentsOpenapiv3AssetStoreID.UUID != uuid.Nil,
				"valid asset name", r.DeploymentsOpenapiv3AssetName.Valid && r.DeploymentsOpenapiv3AssetName.String != "",
				"valid asset slug", r.DeploymentsOpenapiv3AssetSlug.Valid && r.DeploymentsOpenapiv3AssetSlug.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment openapiv3 asset").Log(ctx, logger)
			}

			attachedOpenAPIv3 = append(attachedOpenAPIv3, &types.OpenAPIv3DeploymentAsset{
				ID:      depOpenAPIv3ID.String(),
				AssetID: r.DeploymentsOpenapiv3AssetStoreID.UUID.String(),
				Name:    r.DeploymentsOpenapiv3AssetName.String,
				Slug:    types.Slug(r.DeploymentsOpenapiv3AssetSlug.String),
			})
			seenOpenAPIv3[depOpenAPIv3ID] = true
		}

		functionsID := r.DeploymentsFunctionsID.UUID
		if functionsID != uuid.Nil && !seenFunctions[functionsID] {
			if err := inv.Check(
				"describe deployment functions asset",
				"valid asset id", r.DeploymentsFunctionsAssetID.Valid && r.DeploymentsFunctionsAssetID.UUID != uuid.Nil,
				"valid asset name", r.DeploymentsFunctionsName.Valid && r.DeploymentsFunctionsName.String != "",
				"valid asset slug", r.DeploymentsFunctionsSlug.Valid && r.DeploymentsFunctionsSlug.String != "",
				"valid functions runtime", r.DeploymentsFunctionsRuntime.Valid && r.DeploymentsFunctionsRuntime.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment functions").Log(ctx, logger)
			}

			attachedFunctionsAssets = append(attachedFunctionsAssets, &types.DeploymentFunctions{
				ID:      functionsID.String(),
				AssetID: r.DeploymentsFunctionsAssetID.UUID.String(),
				Name:    r.DeploymentsFunctionsName.String,
				Slug:    types.Slug(r.DeploymentsFunctionsSlug.String),
				Runtime: r.DeploymentsFunctionsRuntime.String,
			})
			seenFunctions[functionsID] = true
		}

		pkgID := r.DeploymentPackageID.UUID
		if pkgID != uuid.Nil && !seenPackages[pkgID] && r.PackageName.Valid {
			pkgName := r.PackageName.String
			attachedPackages = append(attachedPackages, &types.DeploymentPackage{
				ID:   pkgID.String(),
				Name: pkgName,
				Version: semver.Semver{
					Valid:      true,
					Major:      r.PackageVersionMajor.Int64,
					Minor:      r.PackageVersionMinor.Int64,
					Patch:      r.PackageVersionPatch.Int64,
					Prerelease: r.PackageVersionPrerelease.String,
					Build:      r.PackageVersionBuild.String,
				}.String(),
			})
			seenPackages[pkgID] = true
		}

		externalMCPID := r.ExternalMcpID.UUID
		if externalMCPID != uuid.Nil && !seenExternalMCPs[externalMCPID] {
			if err := inv.Check(
				"describe deployment external mcp",
				"valid registry id", r.ExternalMcpRegistryID.Valid && r.ExternalMcpRegistryID.UUID != uuid.Nil,
				"valid name", r.ExternalMcpName.Valid && r.ExternalMcpName.String != "",
				"valid slug", r.ExternalMcpSlug.Valid && r.ExternalMcpSlug.String != "",
				"valid registry server specifier", r.ExternalMcpRegistryServerSpecifier.Valid && r.ExternalMcpRegistryServerSpecifier.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment external mcp").Log(ctx, logger)
			}

			attachedExternalMCPs = append(attachedExternalMCPs, &types.DeploymentExternalMCP{
				ID:                      externalMCPID.String(),
				RegistryID:              r.ExternalMcpRegistryID.UUID.String(),
				Name:                    r.ExternalMcpName.String,
				Slug:                    types.Slug(r.ExternalMcpSlug.String),
				RegistryServerSpecifier: r.ExternalMcpRegistryServerSpecifier.String,
			})
			seenExternalMCPs[externalMCPID] = true
		}
	}

	return &types.Deployment{
		ID:                 deployment.ID.String(),
		CreatedAt:          deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:     deployment.OrganizationID,
		ProjectID:          deployment.ProjectID.String(),
		UserID:             deployment.UserID,
		Status:             stat,
		ExternalID:         conv.FromPGText[string](deployment.ExternalID),
		ExternalURL:        conv.FromPGText[string](deployment.ExternalUrl),
		GithubSha:          conv.FromPGText[string](deployment.GithubSha),
		GithubPr:           conv.FromPGText[string](deployment.GithubPr),
		GithubRepo:         conv.FromPGText[string](deployment.GithubRepo),
		IdempotencyKey:     conv.Ptr(deployment.IdempotencyKey),
		ClonedFrom:         conv.FromNullableUUID(deployment.ClonedFrom),
		Packages:           attachedPackages,
		Openapiv3Assets:    attachedOpenAPIv3,
		Openapiv3ToolCount: openapiv3ToolCount,
		FunctionsToolCount: functionsToolCount,
		FunctionsAssets:    attachedFunctionsAssets,
		ExternalMcps:       attachedExternalMCPs,
	}, nil
}
