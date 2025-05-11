package mv

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/gen/types"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
	"github.com/speakeasy-api/gram/internal/inv"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/packages"
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
	attachedAssets := make([]*types.OpenAPIv3DeploymentAsset, 0, len(rows))
	attachedPackages := make([]*types.DeploymentPackage, 0, len(rows))
	var seenAssets = make(map[uuid.UUID]bool)
	var seenPackages = make(map[uuid.UUID]bool)

	for _, r := range rows {
		depAssetID := r.DeploymentsOpenapiv3AssetID.UUID
		if depAssetID != uuid.Nil && !seenAssets[depAssetID] {
			if err := inv.Check(
				"describe deployment asset",
				"valid asset store id", r.DeploymentsOpenapiv3AssetStoreID.Valid && r.DeploymentsOpenapiv3AssetStoreID.UUID != uuid.Nil,
				"valid asset name", r.DeploymentsOpenapiv3AssetName.Valid && r.DeploymentsOpenapiv3AssetName.String != "",
				"valid asset slug", r.DeploymentsOpenapiv3AssetSlug.Valid && r.DeploymentsOpenapiv3AssetSlug.String != "",
			); err != nil {
				return nil, oops.E(oops.CodeInvariantViolation, err, "invalid state for deployment openapiv3 asset").Log(ctx, logger)
			}

			attachedAssets = append(attachedAssets, &types.OpenAPIv3DeploymentAsset{
				ID:      depAssetID.String(),
				AssetID: r.DeploymentsOpenapiv3AssetStoreID.UUID.String(),
				Name:    r.DeploymentsOpenapiv3AssetName.String,
				Slug:    types.Slug(r.DeploymentsOpenapiv3AssetSlug.String),
			})
			seenAssets[depAssetID] = true
		}

		pkgID := r.DeploymentPackageID.UUID
		if pkgID != uuid.Nil && !seenPackages[pkgID] && r.PackageName.Valid {
			pkgName := r.PackageName.String
			attachedPackages = append(attachedPackages, &types.DeploymentPackage{
				ID:   pkgID.String(),
				Name: pkgName,
				Version: packages.Semver{
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
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID,
		ProjectID:       deployment.ProjectID.String(),
		UserID:          deployment.UserID,
		Status:          stat,
		ExternalID:      conv.FromPGText[string](deployment.ExternalID),
		ExternalURL:     conv.FromPGText[string](deployment.ExternalUrl),
		GithubSha:       conv.FromPGText[string](deployment.GithubSha),
		GithubPr:        conv.FromPGText[string](deployment.GithubPr),
		GithubRepo:      conv.FromPGText[string](deployment.GithubRepo),
		IdempotencyKey:  conv.Ptr(deployment.IdempotencyKey),
		Openapiv3Assets: attachedAssets,
		Packages:        attachedPackages,
	}, nil
}
