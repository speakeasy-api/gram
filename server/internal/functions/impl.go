package functions

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/functions"
	srv "github.com/speakeasy-api/gram/server/gen/http/functions/server"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/functions/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	enc         *encryption.Client
	tigrisStore *assets.TigrisStore
}

var _ gen.Auther = (*Service)(nil)
var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, enc *encryption.Client, tigrisStore *assets.TigrisStore) *Service {
	logger = logger.With(attr.SlogComponent("functions"))

	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/functions"),
		logger:      logger,
		db:          db,
		enc:         enc,
		tigrisStore: tigrisStore,
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

func (s *Service) JWTAuth(ctx context.Context, token string, scheme *security.JWTScheme) (context.Context, error) {
	return jwtAuth(ctx, s.logger, s.db, s.enc, token, scheme)
}

func (s *Service) GetSignedAssetURL(ctx context.Context, p *gen.GetSignedAssetURLPayload) (*gen.GetSignedAssetURLResult, error) {
	authCtx := PullRunnerAuthContext(ctx)
	if authCtx.Validate() != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(
		attr.SlogProjectID(authCtx.ProjectID.String()),
		attr.SlogDeploymentID(authCtx.DeploymentID.String()),
		attr.SlogDeploymentFunctionsID(authCtx.FunctionID.String()),
	)

	assetID, err := uuid.Parse(p.AssetID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid asset id").Log(ctx, logger)
	}

	if assetID == uuid.Nil {
		return nil, oops.E(oops.CodeInvalid, nil, "asset id cannot be nil").Log(ctx, logger)
	}

	fr := repo.New(s.db)
	tu, err := fr.GetFunctionAssetURL(ctx, repo.GetFunctionAssetURLParams{
		ProjectID:    authCtx.ProjectID,
		DeploymentID: authCtx.DeploymentID,
		FunctionID:   authCtx.FunctionID,
		AssetID:      assetID,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get function asset url").Log(ctx, logger)
	}

	parsed, err := url.Parse(tu)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse function asset url").Log(ctx, logger)
	}

	signed, err := s.tigrisStore.PresignRead(ctx, parsed.Path, 10*time.Minute)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to presign function asset url").Log(ctx, logger)
	}

	return &gen.GetSignedAssetURLResult{
		URL: signed.String(),
	}, nil
}
