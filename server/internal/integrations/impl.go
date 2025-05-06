package integrations

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/integrations/server"
	gen "github.com/speakeasy-api/gram/gen/integrations"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
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

func (s *Service) List(ctx context.Context, form *gen.ListPayload) (res *gen.ListIntegrationsResult, err error) {
	rows, err := s.repo.ListIntegrations(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing integrations").Log(ctx, s.logger)
	}

	integrations := make([]*gen.Integration, 0, len(rows))
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

		integrations = append(integrations, &gen.Integration{
			PackageID:           row.PackageID.String(),
			PackageName:         row.PackageName,
			PackageTitle:        conv.FromPGText[string](row.PackageTitle),
			PackageSummary:      conv.FromPGText[string](row.PackageSummary),
			PackageKeywords:     row.PackageKeywords,
			Version:             v.String(),
			VersionCreatedAt:    row.VersionCreatedAt.Time.Format(time.RFC3339),
			ToolCount:           int(row.ToolCount),
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
