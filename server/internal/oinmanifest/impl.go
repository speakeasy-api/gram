package oinmanifest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/oin_manifest/server"
	gen "github.com/speakeasy-api/gram/server/gen/oin_manifest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	gramauth "github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oinmanifest/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	// AuditActionExport is the structured-log audit action stamped on every
	// export. Platform-scoped audit rows do not exist yet (audit_logs requires
	// an organization), so the log line plus the export metric are the record.
	AuditActionExport = "oin-manifest:export"
	auditSubject      = "oin_manifest"

	FormatJSON     = "json"
	FormatMarkdown = "markdown"

	contentTypeJSON     = "application/json"
	contentTypeMarkdown = "text/markdown; charset=utf-8"
)

// PlatformService is the main-server platform-admin export adapter. It reads
// the global remote session catalog only and is gated by the fresh
// platform-admin session check, exactly like the killswitch break-glass
// transport.
type PlatformService struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	auth     *gramauth.Auth
	sessions gramauth.PlatformAdminEntitlementReader
	repo     *repo.Queries
	metrics  *exportMetrics
	config   Config
	now      func() time.Time
}

var _ gen.Service = (*PlatformService)(nil)
var _ gen.Auther = (*PlatformService)(nil)

func NewPlatformService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	config Config,
) *PlatformService {
	return &PlatformService{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oinmanifest"),
		logger:   logger,
		auth:     gramauth.New(logger, db, sessionManager, authzEngine),
		sessions: sessionManager,
		repo:     repo.New(db),
		metrics:  newExportMetrics(logger, meterProvider),
		config:   config,
		now:      time.Now,
	}
}

func AttachPlatformService(mux goahttp.Muxer, service *PlatformService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *PlatformService) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// Export builds the manifest from the global catalog and streams it in the
// requested format. The audit line and metric are emitted before the body is
// handed to the transport, so a failed write still leaves a record.
func (s *PlatformService) Export(ctx context.Context, payload *gen.ExportPayload) (*gen.ExportResult, io.ReadCloser, error) {
	ctx, actor, err := s.authorize(ctx)
	if err != nil {
		return nil, nil, err
	}

	format := FormatJSON
	if payload != nil && payload.Format != "" {
		format = payload.Format
	}
	if format != FormatJSON && format != FormatMarkdown {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "format must be json or markdown")
	}

	rows, err := s.repo.ListGlobalIDJAGIssuers(ctx)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "list global issuers").LogError(ctx, s.logger)
	}

	now := s.now().UTC()
	manifest := Build(rows, s.config, now)
	if manifest.Summary.Registrations > MaxRegistrations {
		return nil, nil, oops.E(oops.CodeFailedPrecondition, nil, "manifest exceeds %d registrations", MaxRegistrations).LogError(ctx, s.logger)
	}

	var body []byte
	contentType := contentTypeJSON
	extension := "json"
	switch format {
	case FormatMarkdown:
		body = RenderMarkdown(manifest)
		contentType = contentTypeMarkdown
		extension = "md"
	default:
		body, err = RenderJSON(manifest)
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "render manifest").LogError(ctx, s.logger)
		}
	}

	s.logger.InfoContext(ctx, "oin manifest exported",
		attr.SlogAuditAction(AuditActionExport),
		attr.SlogAuditSubject(auditSubject),
		attr.SlogUserID(actor.userID),
		attr.SlogAuthUserEmail(actor.email),
		attr.SlogOINManifestFormat(format),
		attr.SlogOINManifestRegistrationCount(manifest.Summary.Registrations),
		attr.SlogOINManifestReadyCount(manifest.Summary.Ready),
		attr.SlogOINManifestBlockedCount(manifest.Summary.Blocked),
	)
	s.metrics.recordExport(ctx, format)

	return &gen.ExportResult{
		ContentType:        contentType,
		ContentDisposition: fmt.Sprintf(`attachment; filename="speakeasy-oin-xaa-manifest-%s.%s"`, now.Format("2006-01-02"), extension),
	}, io.NopCloser(bytes.NewReader(body)), nil
}

type platformActor struct {
	userID string
	email  string
}

func (s *PlatformService) authorize(ctx context.Context) (context.Context, platformActor, error) {
	authCtx, _, err := gramauth.RequireFreshPlatformAdminSession(ctx, s.logger, s.sessions)
	if err != nil {
		return ctx, platformActor{}, fmt.Errorf("authorize oin manifest export: %w", err)
	}
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfacePlatformBreakGlass))
	return ctx, platformActor{userID: authCtx.UserID, email: strings.TrimSpace(*authCtx.Email)}, nil
}
