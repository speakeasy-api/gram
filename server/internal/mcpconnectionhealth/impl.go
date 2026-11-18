// Package mcpconnectionhealth serves the MCP connection health pages: Connection
// Logs, the sign-in summary, session activity and denied clients for one MCP
// authentication provider in a project.
package mcpconnectionhealth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_connection_health/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_connection_health"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts/attemptlog"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// LogsEnabled reports whether an organization has the logs product feature on.
type LogsEnabled func(ctx context.Context, organizationID string) (bool, error)

const (
	viewerScopeAll gen.ConnectionViewerScope = "all"
	viewerScopeOwn gen.ConnectionViewerScope = "own"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *pgxpool.Pool
	ch          driver.Conn
	auth        *auth.Auth
	authz       *authz.Engine
	features    feature.Provider
	logsEnabled LogsEnabled
	now         func() time.Time
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	ch driver.Conn,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	features feature.Provider,
	logsEnabled LogsEnabled,
) *Service {
	logger = logger.With(attr.SlogComponent("mcpconnectionhealth"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpconnectionhealth"),
		logger:      logger,
		db:          db,
		ch:          ch,
		auth:        auth.New(logger, db, sessionManager, authzEngine),
		authz:       authzEngine,
		features:    features,
		logsEnabled: logsEnabled,
		now:         time.Now,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(mux, srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// caller is who is reading and which attempts they may see.
type caller struct {
	organizationID string
	projectID      uuid.UUID

	// ownSubjects is nil for a caller who reads the whole project.
	ownSubjects []string
}

func (c caller) viewerScope() gen.ConnectionViewerScope {
	if c.ownSubjects == nil {
		return viewerScopeAll
	}
	return viewerScopeOwn
}

// begin checks the rollout flag and the caller. The flag comes first so a dark
// feature answers not found to everyone.
func (s *Service) begin(ctx context.Context) (caller, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return caller{}, oops.C(oops.CodeUnauthorized)
	}
	groups := feature.OrgProjectGroups(authCtx.OrganizationSlug, conv.PtrValOr(authCtx.ProjectSlug, ""))
	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagMCPConnectionHealth, authCtx.ActiveOrganizationID, groups)
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate mcp connection health flag", attr.SlogError(err))
	}
	if evaluation != feature.EvaluationEnabled {
		return caller{}, oops.C(oops.CodeNotFound)
	}
	return caller{organizationID: authCtx.ActiveOrganizationID, projectID: *authCtx.ProjectID, ownSubjects: nil}, nil
}

// beginLogs adds the read scope: the whole project under log:read, otherwise the
// member's own attempts and those of agents they own.
func (s *Service) beginLogs(ctx context.Context) (caller, error) {
	c, err := s.begin(ctx)
	if err != nil {
		return caller{}, err
	}
	scope, err := s.authz.ResolveReadScope(ctx, authz.ScopeLogRead, c.projectID.String())
	if err != nil {
		return caller{}, err //nolint:wrapcheck // authz errors carry the code the caller must see
	}
	if !scope.Unrestricted() {
		c.ownSubjects = []string{urn.NewUserSubject(scope.UserID).String()}
		for _, agentID := range scope.AgentIDs {
			c.ownSubjects = append(c.ownSubjects, urn.NewAgentSubject(agentID).String())
		}
	}
	return c, nil
}

// loggingOn reports the organization's logs setting.
func (s *Service) loggingOn(ctx context.Context, c caller) (bool, error) {
	enabled, err := s.logsEnabled(ctx, c.organizationID)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check whether logs are enabled").LogError(ctx, s.logger)
	}
	return enabled, nil
}

// provider loads the issuer the page is for, or not found.
func (s *Service) provider(ctx context.Context, c caller, issuerID string) (repo.GetProviderIssuerRow, error) {
	id, err := uuid.Parse(issuerID)
	if err != nil {
		return repo.GetProviderIssuerRow{}, oops.E(oops.CodeBadRequest, err, "invalid provider id").LogWarn(ctx, s.logger)
	}
	issuer, err := repo.New(s.db).GetProviderIssuer(ctx, repo.GetProviderIssuerParams{
		IssuerID:       id,
		ProjectID:      uuid.NullUUID{UUID: c.projectID, Valid: true},
		OrganizationID: c.organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return repo.GetProviderIssuerRow{}, oops.E(oops.CodeNotFound, err, "provider not found").LogWarn(ctx, s.logger)
	case err != nil:
		return repo.GetProviderIssuerRow{}, oops.E(oops.CodeUnexpected, err, "load provider").LogError(ctx, s.logger)
	}
	return issuer, nil
}

// rangeQuery is the parsed common part of a provider read.
type rangeQuery struct {
	issuerID string
	serverID *string
	from     string
	to       string
}

// attemptScope builds the ClickHouse scope for a provider read.
func (s *Service) attemptScope(ctx context.Context, c caller, q rangeQuery, now time.Time) (attemptlog.Scope, error) {
	var none attemptlog.Scope
	from, to, err := s.parseRange(ctx, q.from, q.to, now)
	if err != nil {
		return none, err
	}
	issuer, err := s.provider(ctx, c, q.issuerID)
	if err != nil {
		return none, err
	}
	serverID := ""
	if q.serverID != nil {
		parsed, err := uuid.Parse(*q.serverID)
		if err != nil {
			return none, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, s.logger)
		}
		serverID = parsed.String()
	}
	ids, err := repo.New(s.db).ListIssuerServerIDs(ctx, repo.ListIssuerServerIDsParams{
		ProjectID:      c.projectID,
		IssuerID:       uuid.NullUUID{UUID: issuer.ID, Valid: true},
		OrganizationID: c.organizationID,
	})
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "list provider servers").LogError(ctx, s.logger)
	}
	serverIDs := make([]string, len(ids))
	for i, id := range ids {
		serverIDs[i] = id.String()
	}
	return attemptlog.Scope{
		OrganizationID:  c.organizationID,
		ProjectID:       c.projectID,
		IssuerID:        issuer.ID,
		IssuerServerIDs: serverIDs,
		ServerID:        serverID,
		AttemptID:       uuid.Nil,
		From:            from,
		To:              to,
		OwnSubjects:     c.ownSubjects,
	}, nil
}

func (s *Service) parseRange(ctx context.Context, fromValue, toValue string, now time.Time) (time.Time, time.Time, error) {
	from, err := time.Parse(time.RFC3339, fromValue)
	if err != nil {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, err, "invalid from time").LogWarn(ctx, s.logger)
	}
	to, err := time.Parse(time.RFC3339, toValue)
	if err != nil {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, err, "invalid to time").LogWarn(ctx, s.logger)
	}
	if err := attemptlog.ValidateRange(from, to, now); err != nil {
		return time.Time{}, time.Time{}, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, s.logger)
	}
	return from.UTC(), to.UTC(), nil
}
