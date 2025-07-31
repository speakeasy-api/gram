package deployments

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type IdempotencyKey *string
type DeploymentID uuid.UUID
type ProjectID uuid.UUID

type upsertOpenAPIv3 struct {
	assetID uuid.UUID
	name    string
	slug    string
}

type upsertPackage struct {
	packageID uuid.UUID
	versionID uuid.UUID
}

type deploymentFields struct {
	projectID      uuid.UUID
	userID         string
	organizationID string
	githubRepo     string
	githubPr       string
	githubSha      string
	externalID     string
	externalURL    string
}

func createDeployment(
	ctx context.Context,
	tracer trace.Tracer,
	logger *slog.Logger,
	tx *repo.Queries,
	idempotencyKey IdempotencyKey,
	fields deploymentFields,
	openAPIv3ToUpsert []upsertOpenAPIv3,
	packagesToUpsert []upsertPackage,
) (uuid.UUID, error) {
	ctx, span := tracer.Start(ctx, "createDeployment")
	defer span.End()
	defer span.SetStatus(codes.Ok, "deployment created")
	key := conv.PtrValOr(idempotencyKey, "")
	if key == "" {
		key = uuid.New().String()
	}

	cmd, err := tx.CreateDeployment(ctx, repo.CreateDeploymentParams{
		ProjectID:      fields.projectID,
		UserID:         fields.userID,
		OrganizationID: fields.organizationID,
		IdempotencyKey: key,

		GithubRepo:  conv.ToPGTextEmpty(fields.githubRepo),
		GithubPr:    conv.ToPGTextEmpty(fields.githubPr),
		GithubSha:   conv.ToPGTextEmpty(fields.githubSha),
		ExternalID:  conv.ToPGTextEmpty(fields.externalID),
		ExternalUrl: conv.ToPGTextEmpty(fields.externalURL),
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error creating deployment").Log(ctx, logger)
	}

	created := cmd.RowsAffected() > 0

	d, err := tx.GetDeploymentByIdempotencyKey(ctx, repo.GetDeploymentByIdempotencyKeyParams{
		IdempotencyKey: key,
		ProjectID:      fields.projectID,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error reading deployment").Log(ctx, logger)
	}

	newID := d.Deployment.ID
	if !created {
		return newID, nil
	}

	logger = logger.With(attr.SlogDeploymentID(d.Deployment.ID.String()))
	span.SetAttributes(attr.DeploymentID(d.Deployment.ID.String()))

	aerr := amendDeployment(ctx, logger, tx, DeploymentID(newID), openAPIv3ToUpsert, packagesToUpsert)
	if aerr != nil {
		return uuid.Nil, aerr
	}

	_, err = tx.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: newID,
		ProjectID:    fields.projectID,
		Status:       "created",
		Event:        "deployment:created",
		Message:      "Deployment created",
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error logging deployment creation").Log(ctx, logger)
	}

	return newID, nil
}

func cloneDeployment(
	ctx context.Context,
	tracer trace.Tracer,
	logger *slog.Logger,
	depRepo *repo.Queries,
	projectID ProjectID,
	srcDeploymentID DeploymentID,
	openAPIv3ToUpsert []upsertOpenAPIv3,
	packagesToUpsert []upsertPackage,
	openAPIv3ToExclude []uuid.UUID,
	packagesToExclude []uuid.UUID,
) (uuid.UUID, error) {
	ctx, span := tracer.Start(ctx, "cloneDeployment")
	defer span.End()
	defer span.SetStatus(codes.Ok, "deployment cloned")

	srcDepID := uuid.UUID(srcDeploymentID)
	projID := uuid.UUID(projectID)

	newID, err := depRepo.CloneDeployment(ctx, repo.CloneDeploymentParams{
		ID:        srcDepID,
		ProjectID: projID,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error cloning deployment").Log(ctx, logger)
	}

	logger = logger.With(attr.SlogDeploymentID(newID.String()))
	span.SetAttributes(attribute.String("deployment_id", newID.String()))

	_, err = depRepo.CloneDeploymentPackages(ctx, repo.CloneDeploymentPackagesParams{
		OriginalDeploymentID: srcDepID,
		CloneDeploymentID:    newID,
		ExcludedIds:          packagesToExclude,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error cloning deployment openapi v3 assets").Log(ctx, logger)
	}

	_, err = depRepo.CloneDeploymentOpenAPIv3Assets(ctx, repo.CloneDeploymentOpenAPIv3AssetsParams{
		OriginalDeploymentID: srcDepID,
		CloneDeploymentID:    newID,
		ExcludedIds:          openAPIv3ToExclude,
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error cloning deployment openapi v3 assets").Log(ctx, logger)
	}

	err = amendDeployment(ctx, logger, depRepo, DeploymentID(newID), openAPIv3ToUpsert, packagesToUpsert)
	if err != nil {
		return uuid.Nil, err
	}

	_, err = depRepo.TransitionDeployment(ctx, repo.TransitionDeploymentParams{
		DeploymentID: newID,
		ProjectID:    projID,
		Status:       "created",
		Event:        "deployment:created",
		Message:      "Deployment created",
	})
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeUnexpected, err, "error logging deployment creation").Log(ctx, logger)
	}

	return newID, nil
}

func amendDeployment(
	ctx context.Context,
	logger *slog.Logger,
	depRepo *repo.Queries,
	deploymentID DeploymentID,
	openAPIv3ToUpsert []upsertOpenAPIv3,
	packagesToUpsert []upsertPackage,
) error {
	id := uuid.UUID(deploymentID)

	for _, a := range openAPIv3ToUpsert {
		_, err := depRepo.UpsertDeploymentOpenAPIv3Asset(ctx, repo.UpsertDeploymentOpenAPIv3AssetParams{
			DeploymentID: id,
			AssetID:      a.assetID,
			Name:         a.name,
			Slug:         a.slug,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return oops.E(oops.CodeUnexpected, err, "error adding deployment openapi v3 asset").Log(ctx, logger)
		}
	}

	for _, p := range packagesToUpsert {
		_, err := depRepo.UpsertDeploymentPackage(ctx, repo.UpsertDeploymentPackageParams{
			DeploymentID: id,
			PackageID:    p.packageID,
			VersionID:    p.versionID,
		})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return oops.E(oops.CodeUnexpected, err, "error adding deployment package").Log(ctx, logger)
		}
	}

	return nil
}
