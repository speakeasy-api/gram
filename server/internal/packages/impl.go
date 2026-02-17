package packages

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/packages/server"
	gen "github.com/speakeasy-api/gram/server/gen/packages"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/packages/repo"
	"github.com/speakeasy-api/gram/server/internal/packages/semver"
)

var nilID uuid.NullUUID

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("packages"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/packages"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListPackages(ctx context.Context, form *gen.ListPackagesPayload) (res *gen.ListPackagesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	packages, err := s.repo.ListPackages(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing packages").Log(ctx, logger)
	}

	result := &gen.ListPackagesResult{
		Packages: make([]*gen.Package, 0, len(packages)),
	}

	for _, pkg := range packages {
		versionString := semver.Semver{
			Valid:      true,
			Major:      pkg.VersionMajor.Int64,
			Minor:      pkg.VersionMinor.Int64,
			Patch:      pkg.VersionPatch.Int64,
			Prerelease: conv.PtrValOr(conv.FromPGText[string](pkg.VersionPrerelease), ""),
			Build:      conv.PtrValOr(conv.FromPGText[string](pkg.VersionBuild), ""),
		}.String()

		imageID := pkg.Package.ImageAssetID.UUID.String()
		result.Packages = append(result.Packages, &gen.Package{
			ID:             pkg.Package.ID.String(),
			Name:           pkg.Package.Name,
			Title:          &pkg.Package.Title.String,
			Summary:        &pkg.Package.Summary.String,
			URL:            &pkg.Package.Url.String,
			Description:    &pkg.Package.DescriptionHtml.String,
			DescriptionRaw: &pkg.Package.DescriptionRaw.String,
			Keywords:       pkg.Package.Keywords,
			OrganizationID: pkg.Package.OrganizationID,
			ProjectID:      pkg.Package.ProjectID.String(),
			LatestVersion:  &versionString,
			ImageAssetID:   &imageID,
			CreatedAt:      pkg.Package.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      pkg.Package.UpdatedAt.Time.Format(time.RFC3339),
			DeletedAt:      new(pkg.Package.DeletedAt.Time.Format(time.RFC3339)),
		})
	}

	return result, nil
}

func (s *Service) CreatePackage(ctx context.Context, form *gen.CreatePackagePayload) (res *gen.CreatePackageResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing packages").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	kw := form.Keywords
	if kw == nil {
		kw = []string{}
	}

	imageAssetID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if form.ImageAssetID != nil {
		imgasset, err := uuid.Parse(*form.ImageAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "image id is not a valid uuid").Log(ctx, logger)
		}

		imageAssetID = uuid.NullUUID{UUID: imgasset, Valid: imgasset != uuid.Nil}
	}

	var descriptionHTML *string
	if form.Description != nil {
		html, err := conv.MarkdownToHTML([]byte(*form.Description))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error converting markdown to html").Log(ctx, logger)
		}

		descriptionHTML = new(string(html))
	}

	packageID, err := tx.CreatePackage(ctx, repo.CreatePackageParams{
		Name:            form.Name,
		Title:           conv.ToPGText(form.Title),
		Summary:         conv.ToPGText(form.Summary),
		Url:             conv.PtrToPGTextEmpty(form.URL),
		Keywords:        kw,
		DescriptionRaw:  conv.PtrToPGTextEmpty(form.Description),
		DescriptionHtml: conv.PtrToPGTextEmpty(descriptionHTML),
		OrganizationID:  authCtx.ActiveOrganizationID,
		ProjectID:       *authCtx.ProjectID,
		ImageAssetID:    imageAssetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating package").Log(ctx, logger)
	}

	if packageID == uuid.Nil {
		return nil, oops.E(oops.CodeInvariantViolation, nil, "error retrieving package id").Log(ctx, logger)
	}

	nullID := uuid.NullUUID{UUID: packageID, Valid: true}
	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(nullID), NullablePackageName(nil))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving package").Log(ctx, logger)
	}

	return &gen.CreatePackageResult{Package: pkg}, nil
}

func (s *Service) UpdatePackage(ctx context.Context, form *gen.UpdatePackagePayload) (*gen.UpdatePackageResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	pkgID, err := uuid.Parse(form.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "error parsing package id").Log(ctx, logger)
	}

	imageAssetID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if form.ImageAssetID != nil {
		imgasset, err := uuid.Parse(*form.ImageAssetID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "image id is not a valid uuid").Log(ctx, logger)
		}

		imageAssetID = uuid.NullUUID{UUID: imgasset, Valid: imgasset != uuid.Nil}
	}

	var descriptionHTML *string
	if form.Description != nil {
		html, err := conv.MarkdownToHTML([]byte(*form.Description))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error converting markdown to html").Log(ctx, logger)
		}

		descriptionHTML = new(string(html))
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing packages").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)
	id, err := tx.UpdatePackage(ctx, repo.UpdatePackageParams{
		ID:              pkgID,
		ProjectID:       *authCtx.ProjectID,
		Url:             conv.PtrToPGTextEmpty(form.URL),
		Title:           conv.PtrToPGTextEmpty(form.Title),
		Summary:         conv.PtrToPGTextEmpty(form.Summary),
		DescriptionRaw:  conv.PtrToPGTextEmpty(form.Description),
		DescriptionHtml: conv.PtrToPGTextEmpty(descriptionHTML),
		Keywords:        form.Keywords,
		ImageAssetID:    imageAssetID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error updating package").Log(ctx, logger)
	}

	if err := inv.Check("package update result", "id is set", id != uuid.Nil); err != nil {
		return nil, oops.E(oops.CodeInvariantViolation, err, "error updating package").Log(ctx, logger)
	}

	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(uuid.NullUUID{UUID: id, Valid: true}), NullablePackageName(nil))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving package").Log(ctx, logger)
	}

	return &gen.UpdatePackageResult{Package: pkg}, nil
}

func (s *Service) ListVersions(ctx context.Context, form *gen.ListVersionsPayload) (res *gen.ListVersionsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing package versions").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	packageName := new(form.Name)

	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(nilID), NullablePackageName(packageName))
	if err != nil {
		return nil, err
	}

	versionRows, err := tx.ListVersions(ctx, repo.ListVersionsParams{
		PackageID:   nilID,
		PackageName: conv.PtrToPGTextEmpty(packageName),
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing versions").Log(ctx, logger)
	}

	versions := make([]*gen.PackageVersion, 0, len(versionRows))
	for _, row := range versionRows {
		sv := semver.Semver{
			Valid:      true,
			Major:      row.VersionMajor,
			Minor:      row.VersionMinor,
			Patch:      row.VersionPatch,
			Prerelease: conv.PtrValOr(conv.FromPGText[string](row.VersionPrerelease), ""),
			Build:      conv.PtrValOr(conv.FromPGText[string](row.VersionBuild), ""),
		}

		versions = append(versions, &gen.PackageVersion{
			ID:           row.VersionID.String(),
			PackageID:    row.Package.ID.String(),
			DeploymentID: row.VersionDeploymentID.String(),
			Visibility:   row.VersionVisibility,
			CreatedAt:    row.VersionCreatedAt.Time.Format(time.RFC3339),
			Semver:       sv.String(),
		})
	}

	return &gen.ListVersionsResult{
		Package:  pkg,
		Versions: versions,
	}, nil
}

func (s *Service) Publish(ctx context.Context, form *gen.PublishPayload) (res *gen.PublishPackageResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	semver, err := semver.ParseSemver(form.Version)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "error parsing version").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing packages").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	depID, err := uuid.Parse(form.DeploymentID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "error parsing deployment id").Log(ctx, logger)
	}

	pkgID, err := tx.PokePackageByName(ctx, repo.PokePackageByNameParams{
		Name:      form.Name,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading package data").Log(ctx, logger)
	}

	if pkgID == uuid.Nil {
		return nil, oops.E(oops.CodeNotFound, nil, "package not found").Log(ctx, logger)
	}

	row, err := tx.CreatePackageVersion(ctx, repo.CreatePackageVersionParams{
		PackageID:    pkgID,
		DeploymentID: depID,
		Major:        semver.Major,
		Minor:        semver.Minor,
		Patch:        semver.Patch,
		Prerelease:   conv.PtrToPGTextEmpty(&semver.Prerelease),
		Build:        conv.PtrToPGTextEmpty(&semver.Build),
		Visibility:   form.Visibility,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating package version").Log(ctx, logger)
	}

	pid := uuid.NullUUID{UUID: pkgID, Valid: true}
	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(pid), NullablePackageName(nil))
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving package version").Log(ctx, logger)
	}

	return &gen.PublishPackageResult{
		Package: pkg,
		Version: &gen.PackageVersion{
			ID:           row.ID.String(),
			PackageID:    pkgID.String(),
			DeploymentID: depID.String(),
			Visibility:   row.Visibility,
			CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
			Semver:       semver.String(),
		},
	}, nil
}

type NullablePackageID uuid.NullUUID
type NullablePackageName *string
type ProjectID uuid.UUID

func describePackage(
	ctx context.Context,
	logger *slog.Logger,
	tx *repo.Queries,
	projectID ProjectID,
	packageID NullablePackageID,
	packageName NullablePackageName,
) (*gen.Package, error) {
	row, err := tx.GetPackageWithLatestVersion(ctx, repo.GetPackageWithLatestVersionParams{
		PackageID:   uuid.NullUUID(packageID),
		PackageName: conv.PtrToPGTextEmpty(packageName),
		ProjectID:   uuid.UUID(projectID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting package with latest version").Log(ctx, logger)
	}

	var deletedAt *string
	if row.Package.DeletedAt.Valid {
		deletedAt = new(row.Package.DeletedAt.Time.Format(time.RFC3339))
	}

	var versionString *string
	if row.VersionMajor.Valid && row.VersionMinor.Valid && row.VersionPatch.Valid {
		sv := semver.Semver{
			Valid:      true,
			Major:      row.VersionMajor.Int64,
			Minor:      row.VersionMinor.Int64,
			Patch:      row.VersionPatch.Int64,
			Prerelease: conv.PtrValOr(conv.FromPGText[string](row.VersionPrerelease), ""),
			Build:      conv.PtrValOr(conv.FromPGText[string](row.VersionBuild), ""),
		}
		versionString = new(sv.String())
	}

	pkg := &gen.Package{
		ID:             row.Package.ID.String(),
		Name:           row.Package.Name,
		Title:          conv.FromPGText[string](row.Package.Title),
		Summary:        conv.FromPGText[string](row.Package.Summary),
		URL:            conv.FromPGText[string](row.Package.Url),
		Description:    conv.FromPGText[string](row.Package.DescriptionHtml),
		DescriptionRaw: conv.FromPGText[string](row.Package.DescriptionRaw),
		Keywords:       row.Package.Keywords,
		ProjectID:      row.Package.ProjectID.String(),
		OrganizationID: row.Package.OrganizationID,
		ImageAssetID:   conv.FromNullableUUID(row.Package.ImageAssetID),
		CreatedAt:      row.Package.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.Package.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:      deletedAt,
		LatestVersion:  versionString,
	}

	return pkg, nil
}
