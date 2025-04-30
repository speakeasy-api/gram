package packages

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/packages/server"
	gen "github.com/speakeasy-api/gram/gen/packages"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/packages/repo"
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
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/internal/packages"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreatePackage(ctx context.Context, form *gen.CreatePackagePayload) (res *gen.CreatePackageResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("authorization check failed")
	}

	logger := s.logger.With(slog.String("project_id", authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(err, "database error", "failed to begin database transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	kw := form.Keywords
	if kw == nil {
		kw = []string{}
	}

	packageID, err := tx.CreatePackage(ctx, repo.CreatePackageParams{
		Name:           form.Name,
		Title:          conv.PtrToPGTextEmpty(form.Title),
		Summary:        conv.PtrToPGTextEmpty(form.Summary),
		Keywords:       kw,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(err, "error creating package", "failed to create package").Log(ctx, logger)
	}

	if packageID == uuid.Nil {
		return nil, oops.E(nil, "error retrieving package id", "nil package id returned from database").Log(ctx, logger)
	}

	nullID := uuid.NullUUID{UUID: packageID, Valid: true}
	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(nullID), NullablePackageName(nil))
	if err != nil {
		return nil, oops.E(err, "error reading package details", "failed to describe package").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(err, "error saving package", "failed to commit database transaction").Log(ctx, logger)
	}

	return &gen.CreatePackageResult{Package: pkg}, nil
}

func (s *Service) ListVersions(ctx context.Context, form *gen.ListVersionsPayload) (res *gen.ListVersionsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("authorization check failed")
	}

	logger := s.logger.With(slog.String("project_id", authCtx.ProjectID.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(err, "database error", "failed to begin database transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	packageName := conv.Ptr(form.Name)

	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(nilID), NullablePackageName(packageName))
	if err != nil {
		return nil, oops.E(err, "error describing package", "failed to describe package").Log(ctx, logger)
	}

	versionRows, err := tx.ListVersions(ctx, repo.ListVersionsParams{
		PackageID:   nilID,
		PackageName: conv.PtrToPGTextEmpty(packageName),
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(err, "error listing versions", "failed to list versions").Log(ctx, logger)
	}

	versions := make([]*gen.PackageVersion, 0, len(versionRows))
	for _, row := range versionRows {
		sv := Semver{
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
		return nil, errors.New("authorization check failed")
	}

	logger := s.logger.With(slog.String("project_id", authCtx.ProjectID.String()))

	semver, err := ParseSemver(form.Version)
	if err != nil {
		return nil, oops.E(err, "error parsing version", "failed to parse version").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(err, "database error", "failed to begin database transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error {
		return dbtx.Rollback(ctx)
	})

	tx := s.repo.WithTx(dbtx)

	depID, err := uuid.Parse(form.DeploymentID)
	if err != nil {
		return nil, oops.E(err, "error parsing deployment id", "failed to parse deployment id").Log(ctx, logger)
	}

	pkgID, err := tx.PokePackageByName(ctx, repo.PokePackageByNameParams{
		Name:      form.Name,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(err, "error reading package data", "failed to find package by name").Log(ctx, logger)
	}

	if pkgID == uuid.Nil {
		return nil, oops.E(nil, "package not found", "package not found").Log(ctx, logger)
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
		return nil, oops.E(err, "error creating package version", "failed to create package version").Log(ctx, logger)
	}

	pid := uuid.NullUUID{UUID: pkgID, Valid: true}
	pkg, err := describePackage(ctx, logger, tx, ProjectID(*authCtx.ProjectID), NullablePackageID(pid), NullablePackageName(nil))
	if err != nil {
		return nil, oops.E(err, "error describing package", "failed to describe package").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(err, "error saving package version", "failed to commit database transaction").Log(ctx, logger)
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
		return nil, oops.E(err, "error getting package with latest version", "failed to get package with latest version").Log(ctx, logger)
	}

	var deletedAt *string
	if row.Package.DeletedAt.Valid {
		deletedAt = conv.Ptr(row.Package.DeletedAt.Time.Format(time.RFC3339))
	}

	var semver *string
	if row.VersionMajor.Valid && row.VersionMinor.Valid && row.VersionPatch.Valid {
		sv := Semver{
			Valid:      true,
			Major:      row.VersionMajor.Int16,
			Minor:      row.VersionMinor.Int16,
			Patch:      row.VersionPatch.Int16,
			Prerelease: conv.PtrValOr(conv.FromPGText[string](row.VersionPrerelease), ""),
			Build:      conv.PtrValOr(conv.FromPGText[string](row.VersionBuild), ""),
		}
		semver = conv.Ptr(sv.String())
	}

	pkg := &gen.Package{
		ID:             row.Package.ID.String(),
		Name:           row.Package.Name,
		Title:          conv.FromPGText[string](row.Package.Title),
		Summary:        conv.FromPGText[string](row.Package.Summary),
		Keywords:       row.Package.Keywords,
		ProjectID:      row.Package.ProjectID.String(),
		OrganizationID: row.Package.OrganizationID,
		CreatedAt:      row.Package.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.Package.UpdatedAt.Time.Format(time.RFC3339),
		DeletedAt:      deletedAt,
		LatestVersion:  semver,
	}

	return pkg, nil
}
