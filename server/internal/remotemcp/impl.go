package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/remote_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	auth    *auth.Auth
	authz   *authz.Engine
	headers *Headers
	policy  *guardian.Policy
	audit   *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	enc *encryption.Client,
	authzEngine *authz.Engine,
	policy *guardian.Policy,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("remotemcp"))

	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotemcp"),
		logger:  logger,
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		headers: NewHeaders(logger, db, enc),
		policy:  policy,
		audit:   auditLogger,
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

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*types.RemoteMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := validateURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").Log(ctx, logger)
	}

	// Validate header inputs
	for _, h := range payload.Headers {
		if err := validateHeaderInput(h, false); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid header").Log(ctx, logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ProjectID:     *authCtx.ProjectID,
		TransportType: payload.TransportType,
		Url:           payload.URL,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create remote mcp server").Log(ctx, logger)
	}

	var headerRows []repo.RemoteMcpServerHeader
	for _, h := range payload.Headers {
		row, err := headersRepo.CreateHeader(ctx, repo.CreateHeaderParams{
			RemoteMcpServerID:      server.ID,
			Name:                   h.Name,
			Description:            conv.PtrToPGText(h.Description),
			IsRequired:             conv.PtrValOr(h.IsRequired, false),
			IsSecret:               conv.PtrValOr(h.IsSecret, false),
			Value:                  conv.PtrToPGTextEmpty(h.Value),
			ValueFromRequestHeader: conv.PtrToPGTextEmpty(h.ValueFromRequestHeader),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "create remote mcp server header").Log(ctx, logger)
		}

		headerRows = append(headerRows, row)
	}

	if err := s.audit.LogRemoteMcpServerCreate(ctx, dbtx, audit.LogRemoteMcpServerCreateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerID:  server.ID,
		RemoteMcpServerURL: server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildRemoteMcpServerView(server, headerRows), nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	servers, err := txRepo.ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp servers").Log(ctx, s.logger)
	}

	serverIDs := make([]uuid.UUID, len(servers))
	for i, server := range servers {
		serverIDs[i] = server.ID
	}

	headersByServerID, err := headersRepo.ListHeadersByServerIDs(ctx, serverIDs, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp server headers").Log(ctx, s.logger)
	}

	result := make([]*types.RemoteMcpServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, mv.BuildRemoteMcpServerView(server, headersByServerID[server.ID]))
	}

	return &gen.ListServersResult{RemoteMcpServers: result}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.RemoteMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").Log(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	server, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").Log(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").Log(ctx, s.logger)
	}

	headers, err := headersRepo.ListHeaders(ctx, server.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp server headers").Log(ctx, s.logger)
	}

	return mv.BuildRemoteMcpServerView(server, headers), nil
}

func (s *Service) UpdateServer(ctx context.Context, payload *gen.UpdateServerPayload) (*types.RemoteMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").Log(ctx, logger)
	}

	if payload.URL != nil {
		if err := validateURL(ctx, s.policy, *payload.URL); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid url").Log(ctx, logger)
		}
	}

	// Validate header inputs (allow secret headers to omit value on updates to preserve existing)
	for _, h := range payload.Headers {
		if err := validateHeaderInput(h, true); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid header").Log(ctx, logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	// Fetch current state for before-snapshot
	existingServer, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").Log(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").Log(ctx, logger)
	}

	beforeHeaders, err := headersRepo.ListHeaders(ctx, existingServer.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list headers for before snapshot").Log(ctx, logger)
	}

	beforeView := mv.BuildRemoteMcpServerView(existingServer, beforeHeaders)

	// Update server fields
	updatedServer, err := txRepo.UpdateServer(ctx, repo.UpdateServerParams{
		ID:            serverID,
		ProjectID:     *authCtx.ProjectID,
		TransportType: conv.PtrValOr(payload.TransportType, existingServer.TransportType),
		Url:           conv.PtrValOr(payload.URL, existingServer.Url),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update remote mcp server").Log(ctx, logger)
	}

	// Reconcile headers: nil means leave unchanged, non-nil is the desired state.
	if payload.Headers != nil {
		// Fetch raw headers (with encrypted values intact) for secret value preservation
		rawHeaders, err := txRepo.ListHeadersByServerID(ctx, serverID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list raw headers for preservation").Log(ctx, logger)
		}

		rawByName := make(map[string]repo.RemoteMcpServerHeader, len(rawHeaders))
		for _, rh := range rawHeaders {
			rawByName[rh.Name] = rh
		}

		// Build set of desired header names
		desiredNames := make(map[string]struct{}, len(payload.Headers))
		for _, h := range payload.Headers {
			desiredNames[h.Name] = struct{}{}
		}

		// Upsert all desired headers
		for _, h := range payload.Headers {
			isSecret := conv.PtrValOr(h.IsSecret, false)
			value := conv.PtrToPGTextEmpty(h.Value)

			// When value is omitted for an existing secret header, preserve the
			// stored encrypted value by passing it directly to the repo (bypassing
			// the encryption wrapper which would double-encrypt).
			if !value.Valid && isSecret {
				if existing, ok := rawByName[h.Name]; ok && existing.IsSecret && existing.Value.Valid {
					_, err := txRepo.UpsertHeader(ctx, repo.UpsertHeaderParams{
						RemoteMcpServerID:      serverID,
						Name:                   h.Name,
						Description:            conv.PtrToPGText(h.Description),
						IsRequired:             conv.PtrValOr(h.IsRequired, false),
						IsSecret:               isSecret,
						Value:                  existing.Value,
						ValueFromRequestHeader: conv.PtrToPGTextEmpty(h.ValueFromRequestHeader),
					})
					if err != nil {
						return nil, oops.E(oops.CodeUnexpected, err, "upsert remote mcp server header").Log(ctx, logger)
					}

					continue
				}

				return nil, oops.E(oops.CodeBadRequest, fmt.Errorf("header %q: new secret header must provide a value", h.Name), "invalid header").Log(ctx, logger)
			}

			_, err := headersRepo.UpsertHeader(ctx, repo.UpsertHeaderParams{
				RemoteMcpServerID:      serverID,
				Name:                   h.Name,
				Description:            conv.PtrToPGText(h.Description),
				IsRequired:             conv.PtrValOr(h.IsRequired, false),
				IsSecret:               isSecret,
				Value:                  value,
				ValueFromRequestHeader: conv.PtrToPGTextEmpty(h.ValueFromRequestHeader),
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "upsert remote mcp server header").Log(ctx, logger)
			}
		}

		// Remove any existing headers not in the desired set
		for _, existing := range beforeHeaders {
			if _, keep := desiredNames[existing.Name]; !keep {
				if err := headersRepo.DeleteHeader(ctx, serverID, existing.Name); err != nil {
					return nil, oops.E(oops.CodeUnexpected, err, "delete remote mcp server header").Log(ctx, logger)
				}
			}
		}
	}

	// Fetch updated state for after-snapshot
	afterHeaders, err := headersRepo.ListHeaders(ctx, updatedServer.ID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list headers for after snapshot").Log(ctx, logger)
	}

	afterView := mv.BuildRemoteMcpServerView(updatedServer, afterHeaders)

	if err := s.audit.LogRemoteMcpServerUpdate(ctx, dbtx, audit.LogRemoteMcpServerUpdateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerID:  updatedServer.ID,
		RemoteMcpServerURL: updatedServer.Url,
		SnapshotBefore:     beforeView,
		SnapshotAfter:      afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteServer(ctx context.Context, payload *gen.DeleteServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid server id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := txRepo.DeleteHeadersByServerID(ctx, serverID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server headers").Log(ctx, logger)
	}

	deleted, err := txRepo.DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server").Log(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerDelete(ctx, dbtx, audit.LogRemoteMcpServerDeleteEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerID:  deleted.ID,
		RemoteMcpServerURL: deleted.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote mcp server deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// validateURL checks that the given URL string is a valid absolute HTTP(S) URL
// whose host is permitted by the supplied [guardian.Policy].
func validateURL(ctx context.Context, policy *guardian.Policy, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}

	if u.Host == "" {
		return fmt.Errorf("url must include a host")
	}

	if err := policy.ValidateHost(ctx, u.Hostname()); err != nil {
		return fmt.Errorf("validate host: %w", err)
	}

	return nil
}

// validateHeaderInput checks that exactly one of value or value_from_request_header is provided.
// When preserveNullSecretValue is true (updates), secret headers may omit value to preserve
// the existing stored value.
func validateHeaderInput(h *gen.HeaderInput, preserveNullSecretValue bool) error {
	hasValue := h.Value != nil && *h.Value != ""
	hasValueFromRequestHeader := h.ValueFromRequestHeader != nil && *h.ValueFromRequestHeader != ""
	isSecret := h.IsSecret != nil && *h.IsSecret

	// For updates, secret headers can omit value to preserve the existing stored value
	if preserveNullSecretValue && isSecret && !hasValue && !hasValueFromRequestHeader {
		return nil
	}

	if hasValue == hasValueFromRequestHeader {
		return fmt.Errorf("header %q must specify exactly one of value or value_from_request_header", h.Name)
	}

	if hasValueFromRequestHeader && isSecret {
		return fmt.Errorf("header %q: pass-through headers cannot be marked as secret", h.Name)
	}

	return nil
}
