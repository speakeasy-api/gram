package assistantmemories

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/assistant_memories"
	srv "github.com/speakeasy-api/gram/server/gen/http/assistant_memories/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/memory"
	"github.com/speakeasy-api/gram/server/internal/memory/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// memoryStore is the subset of memory.MemoryService that this service depends
// on. Defined here so handler tests can substitute a fake without spinning up
// the full memory subsystem.
type memoryStore interface {
	List(ctx context.Context, projectID uuid.UUID, params memory.ListParams) (memory.ListResult, error)
	Get(ctx context.Context, projectID, id uuid.UUID) (repo.GetAssistantMemoryByIDRow, error)
	DeleteByID(ctx context.Context, projectID, id uuid.UUID) error
}

var _ memoryStore = (*memory.MemoryService)(nil)

// featureChecker is the productfeatures surface this service consumes.
// *productfeatures.Client implements it.
type featureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	auth     *auth.Auth
	authz    *authz.Engine
	features featureChecker
	memory   memoryStore
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	features featureChecker,
	memorySvc *memory.MemoryService,
) *Service {
	logger = logger.With(attr.SlogComponent("assistantmemories"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistantmemories"),
		logger:   logger,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		features: features,
		memory:   memorySvc,
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
