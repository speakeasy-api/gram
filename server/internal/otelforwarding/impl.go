package otelforwarding

import (
	"context"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/otel_forwarding"
	srv "github.com/speakeasy-api/gram/server/gen/http/otel_forwarding/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otelforwarding/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	client *Client
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	client *Client,
) *Service {
	logger = logger.With(attr.SlogComponent("otelforwarding.api"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/otelforwarding"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
		client: client,
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

func (s *Service) GetConfig(ctx context.Context, _ *gen.GetConfigPayload) (*gen.OtelForwardingConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	cfg, row, err := s.client.LoadForOrgRow(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return emptyView(authCtx.ActiveOrganizationID), nil
	}
	return buildView(cfg, *row), nil
}

func (s *Service) UpsertConfig(ctx context.Context, payload *gen.UpsertConfigPayload) (*gen.OtelForwardingConfig, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if err := validateForwardingURL(payload.EndpointURL); err != nil {
		return nil, err
	}

	headers, err := normalizeHeaderInputs(payload.Headers)
	if err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	// Load the pre-state for the audit before-snapshot. Stale cache here is
	// fine — the snapshot is for human review, not for write decisions.
	before, beforeRow, err := s.client.LoadForOrgRow(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	cfg, row, err := s.client.UpsertWithTx(ctx, dbtx, authCtx.ActiveOrganizationID, payload.EndpointURL, headers, payload.Enabled)
	if err != nil {
		return nil, err
	}

	var beforeSnap *audit.OtelForwardingSnapshot
	if beforeRow != nil {
		snap := snapshotFromConfig(before)
		beforeSnap = &snap
	}
	afterSnap := snapshotFromConfig(cfg)

	if err := s.audit.LogOtelForwardingUpsert(ctx, dbtx, audit.LogOtelForwardingUpsertEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewOtelForwardingConfig(row.ID),
		SnapshotBefore:   beforeSnap,
		SnapshotAfter:    &afterSnap,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log otel forwarding upsert").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit otel forwarding upsert").Log(ctx, logger)
	}

	s.client.RefreshCache(ctx, cfg)
	return buildView(cfg, *row), nil
}

func (s *Service) DeleteConfig(ctx context.Context, _ *gen.DeleteConfigPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))

	_, row, err := s.client.LoadForOrgRow(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return err
	}
	if row == nil {
		// No active config — nothing to delete, no audit row to emit.
		return nil
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.client.SoftDeleteWithTx(ctx, dbtx, authCtx.ActiveOrganizationID); err != nil {
		return err
	}

	if err := s.audit.LogOtelForwardingDelete(ctx, dbtx, audit.LogOtelForwardingDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConfigURN:        urn.NewOtelForwardingConfig(row.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log otel forwarding delete").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit otel forwarding delete").Log(ctx, logger)
	}

	s.client.InvalidateCache(ctx, authCtx.ActiveOrganizationID)
	return nil
}

func validateForwardingURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return oops.E(oops.CodeInvalid, nil, "endpoint_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "endpoint_url is not a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return oops.E(oops.CodeInvalid, nil, "endpoint_url must use http or https")
	}
	if u.Host == "" {
		return oops.E(oops.CodeInvalid, nil, "endpoint_url must include a host")
	}
	return nil
}

func normalizeHeaderInputs(in []*gen.OtelForwardingHeaderInput) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(in))
	for _, h := range in {
		if h == nil {
			continue
		}
		name := strings.TrimSpace(h.Name)
		if name == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "header name cannot be empty")
		}
		// Reject HTTP-illegal characters early — the forwarder would either
		// silently drop these or panic when constructing the outbound
		// request.
		if strings.ContainsAny(name, " \t\r\n") {
			return nil, oops.E(oops.CodeInvalid, nil, "header name contains invalid whitespace: %q", name)
		}
		if _, exists := out[name]; exists {
			return nil, oops.E(oops.CodeInvalid, nil, "duplicate header name: %q", name)
		}
		out[name] = h.Value
	}
	return out, nil
}

func snapshotFromConfig(cfg CachedConfig) audit.OtelForwardingSnapshot {
	names := make([]string, 0, len(cfg.Headers))
	for k := range cfg.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	return audit.OtelForwardingSnapshot{
		EndpointURL: cfg.URL,
		HeaderNames: names,
		Enabled:     cfg.Enabled,
	}
}

func emptyView(orgID string) *gen.OtelForwardingConfig {
	return &gen.OtelForwardingConfig{
		ID:             "",
		OrganizationID: orgID,
		EndpointURL:    "",
		Enabled:        false,
		Headers:        []*gen.OtelForwardingHeader{},
		CreatedAt:      "",
		UpdatedAt:      "",
	}
}

func buildView(cfg CachedConfig, row repo.OtelForwardingConfig) *gen.OtelForwardingConfig {
	names := make([]string, 0, len(cfg.Headers))
	for k := range cfg.Headers {
		names = append(names, k)
	}
	sort.Strings(names)
	headers := make([]*gen.OtelForwardingHeader, 0, len(names))
	for _, n := range names {
		headers = append(headers, &gen.OtelForwardingHeader{
			Name:     n,
			HasValue: cfg.Headers[n] != "",
		})
	}
	return &gen.OtelForwardingConfig{
		ID:             row.ID.String(),
		OrganizationID: cfg.OrganizationID,
		EndpointURL:    cfg.URL,
		Enabled:        cfg.Enabled,
		Headers:        headers,
		CreatedAt:      row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      row.UpdatedAt.Time.Format(time.RFC3339),
	}
}
