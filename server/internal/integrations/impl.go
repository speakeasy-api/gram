package integrations

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/integrations/server"
	gen "github.com/speakeasy-api/gram/gen/integrations"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/integrations/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/packages"
)

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

func (s *Service) Get(ctx context.Context, form *gen.GetPayload) (res *gen.GetIntegrationResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	pname := conv.PtrToPGTextEmpty(form.Name)

	var pid uuid.NullUUID
	if form.ID != nil {
		id, err := uuid.Parse(*form.ID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "invalid package id").Log(ctx, s.logger)
		}

		pid = uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
	}

	if !pname.Valid && !pid.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "must provide either a valid package name or id")
	}

	row, err := s.repo.GetIntegration(ctx, repo.GetIntegrationParams{
		PackageID:   pid,
		PackageName: pname,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting integration").Log(ctx, s.logger)
	}

	v := packages.Semver{
		Valid:      true,
		Major:      row.VersionMajor,
		Minor:      row.VersionMinor,
		Patch:      row.VersionPatch,
		Prerelease: conv.PtrValOr(conv.FromPGText[string](row.VersionPrerelease), ""),
		Build:      conv.PtrValOr(conv.FromPGText[string](row.VersionBuild), ""),
	}

	versionRows, err := s.repo.ListIntegrationVersions(ctx, repo.ListIntegrationVersionsParams{
		PackageID:   pid,
		PackageName: pname,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing integration versions").Log(ctx, s.logger)
	}

	versions := make([]*gen.IntegrationVersion, 0, len(versionRows))
	for _, row := range versionRows {
		versions = append(versions, &gen.IntegrationVersion{
			Version: packages.Semver{
				Valid:      true,
				Major:      row.Major,
				Minor:      row.Minor,
				Patch:      row.Patch,
				Prerelease: conv.PtrValOr(conv.FromPGText[string](row.Prerelease), ""),
				Build:      conv.PtrValOr(conv.FromPGText[string](row.Build), ""),
			}.String(),
			CreatedAt: row.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.GetIntegrationResult{
		Integration: &gen.Integration{
			PackageID:             row.Package.ID.String(),
			PackageName:           row.Package.Name,
			PackageTitle:          row.Package.Title.String,
			PackageSummary:        row.Package.Summary.String,
			PackageURL:            conv.FromPGText[string](row.Package.Url),
			PackageKeywords:       row.Package.Keywords,
			PackageImageAssetID:   conv.FromNullableUUID(row.Package.ImageAssetID),
			PackageDescription:    conv.FromPGText[string](row.Package.DescriptionHtml),
			PackageDescriptionRaw: conv.FromPGText[string](row.Package.DescriptionRaw),
			Version:               v.String(),
			VersionCreatedAt:      row.VersionCreatedAt.Time.Format(time.RFC3339),
			ToolNames:             row.ToolNames,
			Versions:              versions,
		},
	}, nil
}

func (s *Service) List(ctx context.Context, form *gen.ListPayload) (res *gen.ListIntegrationsResult, err error) {
	rows, err := s.repo.ListIntegrations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing integrations").Log(ctx, s.logger)
	}

	integrations := make([]*gen.IntegrationEntry, 0, len(rows))
	for _, row := range rows {
		if !containsSubset(row.PackageKeywords, form.Keywords) {
			continue
		}

		v := packages.Semver{
			Valid:      true,
			Major:      row.VersionMajor,
			Minor:      row.VersionMinor,
			Patch:      row.VersionPatch,
			Prerelease: conv.PtrValOr(conv.FromPGText[string](row.VersionPrerelease), ""),
			Build:      conv.PtrValOr(conv.FromPGText[string](row.VersionBuild), ""),
		}

		integrations = append(integrations, &gen.IntegrationEntry{
			PackageID:           row.PackageID.String(),
			PackageName:         row.PackageName,
			PackageTitle:        conv.FromPGText[string](row.PackageTitle),
			PackageSummary:      conv.FromPGText[string](row.PackageSummary),
			PackageURL:          conv.FromPGText[string](row.PackageUrl),
			PackageKeywords:     row.PackageKeywords,
			Version:             v.String(),
			VersionCreatedAt:    row.VersionCreatedAt.Time.Format(time.RFC3339),
			ToolNames:           row.ToolNames,
			PackageImageAssetID: conv.FromNullableUUID(row.PackageImageAssetID),
		})
	}

	return &gen.ListIntegrationsResult{
		Integrations: integrations,
	}, nil
}

func containsSubset(all []string, subset []string) bool {
	for _, keyword := range subset {
		if !slices.Contains(all, keyword) {
			return false
		}
	}

	return true
}
