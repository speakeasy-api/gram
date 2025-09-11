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
	openapiToolCount := rows[0].OpenapiToolCount
	functionsToolCount := rows[0].FunctionsToolCount
	attachedOpenAPIAssets := make([]*types.OpenAPIv3DeploymentAsset, 0, len(rows))
	attachedFunctionsAssets := make([]*types.DeploymentFunctions, 0, len(rows))
	attachedPackages := make([]*types.DeploymentPackage, 0, len(rows))
	var seenOpenAPIAssets = make(map[uuid.UUID]bool)
	var seenFunctionsAssets = make(map[uuid.UUID]bool)
	var seenPackages = make(map[uuid.UUID]bool)

	for _, r := range rows {
		depAssetID := r.DeploymentsOpenapiv3AssetID.UUID
		if depAssetID != uuid.Nil && !seenOpenAPIAssets[depAssetID] {
			if err := inv.Check(
				"describe deployment openapiv3 asset",
				"valid asset store id", r.DeploymentsOpenapiv3AssetStoreID.Valid && r.DeploymentsOpenapiv3AssetStoreID.UUID != uuid.Nil,
				"valid asset name", r.DeploymentsOpenapiv3AssetName.Valid && r.DeploymentsOpenapiv3AssetName.String != "",
				"valid asset slug", r.DeploymentsOpenapiv3AssetSlug.Valid && r.DeploymentsOpenapiv3AssetSlug.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment openapiv3 asset").Log(ctx, logger)
			}

			attachedOpenAPIAssets = append(attachedOpenAPIAssets, &types.OpenAPIv3DeploymentAsset{
				ID:      depAssetID.String(),
				AssetID: r.DeploymentsOpenapiv3AssetStoreID.UUID.String(),
				Name:    r.DeploymentsOpenapiv3AssetName.String,
				Slug:    types.Slug(r.DeploymentsOpenapiv3AssetSlug.String),
			})
			seenOpenAPIAssets[depAssetID] = true
		}

		functionsAssetID := r.DeploymentsFunctionsAssetID.UUID
		if functionsAssetID != uuid.Nil && !seenFunctionsAssets[functionsAssetID] {
			if err := inv.Check(
				"describe deployment functions asset",
				"valid asset store id", r.DeploymentsFunctionsAssetStoreID.Valid && r.DeploymentsFunctionsAssetStoreID.UUID != uuid.Nil,
				"valid asset name", r.DeploymentsFunctionsAssetName.Valid && r.DeploymentsFunctionsAssetName.String != "",
				"valid asset slug", r.DeploymentsFunctionsAssetSlug.Valid && r.DeploymentsFunctionsAssetSlug.String != "",
				"valid functions runtime", r.DeploymentsFunctionsAssetRuntime.Valid && r.DeploymentsFunctionsAssetRuntime.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment functions asset").Log(ctx, logger)
			}

			attachedFunctionsAssets = append(attachedFunctionsAssets, &types.DeploymentFunctions{
				ID:      functionsAssetID.String(),
				AssetID: r.DeploymentsFunctionsAssetID.UUID.String(),
				Name:    r.DeploymentsFunctionsAssetName.String,
				Slug:    types.Slug(r.DeploymentsFunctionsAssetSlug.String),
				Runtime: r.DeploymentsFunctionsAssetRuntime.String,
			})
			seenFunctionsAssets[functionsAssetID] = true
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
		Openapiv3Assets:    attachedOpenAPIAssets,
		OpenapiToolCount:   openapiToolCount,
		FunctionsToolCount: functionsToolCount,
		FunctionsAssets:    attachedFunctionsAssets,
	}, nil
}
