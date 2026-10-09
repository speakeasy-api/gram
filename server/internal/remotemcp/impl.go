package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
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
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer                trace.Tracer
	logger                *slog.Logger
	db                    *pgxpool.Pool
	auth                  *auth.Auth
	authz                 *authz.Engine
	headers               *Headers
	policy                *guardian.Policy
	audit                 *audit.Logger
	provisioning          *RemoteMCPProvisioningService
	distributionAdmission *admission.Guard
	features              feature.Provider
	// beforeClaim runs between the claim's list and re-read with the locked previous URL; tests only.
	beforeClaim func(holderPID uint32, previousURL string)
	// beforeScopePinLock runs inside the pin transaction before it locks anything; tests only.
	beforeScopePinLock func()
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
	iconSetter mcpservers.DefaultServerIconSetter,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("remotemcp"))

	return &Service{
		tracer:                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotemcp"),
		logger:                logger,
		db:                    db,
		auth:                  auth.New(logger, db, sessions, authzEngine),
		authz:                 authzEngine,
		headers:               NewHeaders(logger, db, enc),
		policy:                policy,
		audit:                 auditLogger,
		provisioning:          NewRemoteMCPProvisioningService(db, policy, auditLogger, iconSetter),
		distributionAdmission: admission.NewGuard(nil, nil),
		features:              features,
		beforeClaim:           nil,
		beforeScopePinLock:    nil,
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
	if payload.UserSessionIssuerID != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "user_session_issuer_id is only supported when creating a linked MCP server").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if _, err := proxy.ValidateRemoteMCPURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	server, err := createRemoteMCPSource(ctx, dbtx, s.audit, authCtx, remoteMCPSourceInput{
		Name:          payload.Name,
		URL:           payload.URL,
		TransportType: payload.TransportType,
	})
	if err != nil {
		if shareableErr, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, shareableErr.LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create remote MCP server").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteMcpServerView(server), nil
}

func (s *Service) CreateServerAndMcpServer(ctx context.Context, payload *gen.CreateServerAndMcpServerPayload) (*gen.CreateServerAndMcpServerResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	result, err := s.provisioning.ProvisionDashboardRemoteMCP(ctx, authCtx, DashboardRemoteMCPProvisioningInput{
		Name:                payload.Name,
		URL:                 payload.URL,
		TransportType:       payload.TransportType,
		UserSessionIssuerID: payload.UserSessionIssuerID,
	})
	if err != nil {
		if shareableErr, ok := errors.AsType[*oops.ShareableError](err); ok {
			return nil, shareableErr.LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "provision remote MCP server").LogError(ctx, logger)
	}
	if payload.UserSessionIssuerID != nil && result.MCPServer.UserSessionIssuerID.Valid {
		remotesessions.BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{result.MCPServer.UserSessionIssuerID.UUID})
		refreshed, refreshErr := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID:        result.MCPServer.ID,
			ProjectID: *authCtx.ProjectID,
		})
		if refreshErr != nil {
			logger.ErrorContext(ctx, "reload MCP server after issuer sync", attr.SlogError(refreshErr))
		} else {
			result.MCPServer = refreshed
		}
	}
	return &gen.CreateServerAndMcpServerResult{
		RemoteMcpServer: mv.BuildRemoteMcpServerView(result.RemoteMCPServer),
		McpServer:       mv.BuildMcpServerView(result.MCPServer),
	}, nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp servers").LogError(ctx, s.logger)
	}

	result := make([]*types.RemoteMcpServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, mv.BuildRemoteMcpServerView(server))
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

	idProvided := payload.ID != nil && *payload.ID != ""
	slugProvided := payload.Slug != nil && *payload.Slug != ""
	if !idProvided && !slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id or slug is required").LogError(ctx, s.logger)
	}
	if idProvided && slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id and slug are mutually exclusive").LogError(ctx, s.logger)
	}

	dbRepo := repo.New(s.db)

	var server repo.RemoteMcpServer
	var err error
	if idProvided {
		serverID, parseErr := uuid.Parse(*payload.ID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid server id").LogError(ctx, s.logger)
		}
		server, err = dbRepo.GetServerByID(ctx, repo.GetServerByIDParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
		})
	} else {
		server, err = dbRepo.GetServerBySlug(ctx, repo.GetServerBySlugParams{
			Slug:      conv.ToPGText(*payload.Slug),
			ProjectID: *authCtx.ProjectID,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildRemoteMcpServerView(server), nil
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	if payload.URL != nil {
		if _, err := proxy.ValidateRemoteMCPURL(ctx, s.policy, *payload.URL); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
		}
	}
	var rollout admission.RolloutConfig
	var rolloutErr error
	rollout, rolloutErr = s.distributionAdmission.ResolveProject(ctx, s.db, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug, *authCtx.ProjectID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	if err := admission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock distribution admission").LogError(ctx, logger)
	}

	// Locked so the previous URL the claim moves from is the one current at commit time.
	existingServer, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteMcpServerView(existingServer)

	// Resolve name: nil = leave existing, "" = clear, value = trimmed value.
	name := existingServer.Name
	if payload.Name != nil {
		trimmed := strings.TrimSpace(*payload.Name)
		name = pgtype.Text{String: trimmed, Valid: trimmed != ""}
	}

	// Always recompute slug from the post-update URL so it tracks the URL
	// even when the URL didn't change (idempotent).
	finalURL := conv.PtrValOr(payload.URL, existingServer.Url)
	if payload.URL != nil && finalURL != existingServer.Url {
		if err := s.checkRemoteDistributionAdmission(ctx, dbtx, rollout, rolloutErr, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID, finalURL); err != nil {
			return nil, err
		}
	}
	slug, err := conv.URLBackedSlug(finalURL, existingServer.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	updatedServer, err := txRepo.UpdateServer(ctx, repo.UpdateServerParams{
		ID:            serverID,
		ProjectID:     *authCtx.ProjectID,
		Name:          name,
		Slug:          conv.ToPGText(slug),
		TransportType: conv.PtrValOr(payload.TransportType, existingServer.TransportType),
		Url:           finalURL,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "remote mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote mcp server").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteMcpServerView(updatedServer)

	if err := s.audit.LogRemoteMcpServerUpdate(ctx, dbtx, audit.LogRemoteMcpServerUpdateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(updatedServer.ID),
		RemoteMcpServerURL: updatedServer.Url,
		SnapshotBefore:     beforeView,
		SnapshotAfter:      afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server update").LogError(ctx, logger)
	}

	// The claim moves with the URL in the same transaction; the probe that
	// fills it runs after commit. Every save runs it: a client still
	// recording another resource retries.
	claimed, err := s.claimProtectedResource(ctx, dbtx, authCtx, updatedServer.ID, existingServer.Url, updatedServer.Url)
	if err != nil {
		if shared, ok := errors.AsType[*oops.ShareableError](err); ok && shared.Code == oops.CodeConflict {
			return nil, shared.LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "claim protected resource for remote session clients").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.refreshProtectedResourceDisplay(ctx, logger, authCtx, updatedServer.ID, updatedServer.Url, claimed)

	return afterView, nil
}

func (s *Service) ProbeURL(ctx context.Context, payload *gen.ProbeURLPayload) (*gen.ProbeURLResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	probeCtx, cancel := context.WithTimeout(ctx, probeURLTimeout)
	defer cancel()

	if _, err := proxy.ValidateRemoteMCPURL(probeCtx, s.policy, payload.URL); err != nil {
		if errors.Is(err, guardian.ErrBlockedIP) || errors.Is(err, guardian.ErrBadHost) || errors.Is(err, context.DeadlineExceeded) {
			result := classifyTransportError(probeCtx, err)
			return buildProbeURLResult(result), nil
		}
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	return buildProbeURLResult(probeRemoteMcpURL(probeCtx, s.policy, payload.URL).result), nil
}

func buildProbeURLResult(result ProbeResult) *gen.ProbeURLResult {
	return &gen.ProbeURLResult{
		Outcome:                      result.Outcome,
		ProtectedResourceMetadataURL: result.ProtectedResourceMetadataURL,
		HTTPStatus:                   result.HTTPStatus,
		Reason:                       result.Reason,
	}
}

// VerifyURL preserves the shipped verification contract for existing clients.
//
// Deprecated: use ProbeURL instead.
func (s *Service) VerifyURL(ctx context.Context, payload *gen.VerifyURLPayload) (*gen.VerifyURLResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if _, err := proxy.ValidateRemoteMCPURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeURLTimeout)
	defer cancel()

	observation := probeRemoteMcpURL(probeCtx, s.policy, payload.URL)
	switch observation.result.Outcome {
	case ProbeOutcomeMCPAvailable:
		return &gen.VerifyURLResult{Verified: true, HTTPStatus: observation.httpStatus, Message: "Success"}, nil
	case ProbeOutcomeAuthenticationRequired:
		return &gen.VerifyURLResult{Verified: true, HTTPStatus: observation.httpStatus, Message: "Reachable: received authorization required response"}, nil
	case ProbeOutcomeInvalidMCPResponse:
		verified := observation.httpStatus != nil && *observation.httpStatus >= 200 && *observation.httpStatus < 300
		message := "Unexpected response from server"
		if verified {
			message = "Reachable: although received unexpected MCP response"
		} else if observation.httpStatus != nil && *observation.httpStatus == http.StatusNotFound {
			message = "MCP response not found"
		}
		return &gen.VerifyURLResult{Verified: verified, HTTPStatus: observation.httpStatus, Message: message}, nil
	case ProbeOutcomeUnreachable:
		message := "Could not connect to host"
		if observation.httpStatus != nil {
			message = "Unexpected response from server"
		} else if observation.result.Reason != nil {
			switch *observation.result.Reason {
			case ProbeReasonGuardianRejected:
				message = "Host is not allowed"
			case ProbeReasonTimeout:
				message = "Request timed out"
			case ProbeReasonTLSError:
				message = "TLS certificate verification failed"
			}
		}
		return &gen.VerifyURLResult{Verified: false, HTTPStatus: observation.httpStatus, Message: message}, nil
	default:
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("unknown remote mcp probe outcome %q", observation.result.Outcome), "probe remote mcp server").LogError(ctx, logger)
	}
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
		return oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Lock the parent before touching its headers. Header writers take the
	// same lock first, so the two never wait on each other in opposite order
	// and no header can be created under a server being deleted.
	if _, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "lock remote mcp server").LogError(ctx, logger)
	}

	// The FK's ON DELETE CASCADE only fires for hard deletes, so soft-delete the
	// headers explicitly. This runs before the parent row is tombstoned so the
	// query's project subselect can still see it.
	if err := txRepo.DeleteHeadersByServerID(ctx, repo.DeleteHeadersByServerIDParams{
		RemoteMcpServerID: serverID,
		ProjectID:         *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server headers").LogError(ctx, logger)
	}

	deleted, err := txRepo.DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server").LogError(ctx, logger)
	}

	// Deliberately no per-header audit event for the cascade. The parent's
	// remote-mcp:delete entry already accounts for the headers going away, and
	// one entry per header would bury that signal under detail nobody asked for.
	// Headers removed on their own still emit remote-mcp-server-header:delete.
	if err := s.audit.LogRemoteMcpServerDelete(ctx, dbtx, audit.LogRemoteMcpServerDeleteEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(deleted.ID),
		RemoteMcpServerURL: deleted.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) ListServerHeaders(ctx context.Context, payload *gen.ListServerHeadersPayload) (*gen.ListServerHeadersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}

	// Resolve the parent first so an unknown server, or one owned by another
	// project, is a 404 rather than an indistinguishable empty list.
	if _, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, s.logger)
	}

	headers, err := s.headers.ListServerHeaders(ctx, serverID, *authCtx.ProjectID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp server headers").LogError(ctx, s.logger)
	}

	return &gen.ListServerHeadersResult{Headers: mv.BuildRemoteMcpServerHeaderListView(headers)}, nil
}

func (s *Service) GetServerHeader(ctx context.Context, payload *gen.GetServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, s.logger)
	}

	header, err := s.headers.GetServerHeader(ctx, headerID, *authCtx.ProjectID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server header").LogError(ctx, s.logger)
	}

	return mv.BuildRemoteMcpServerHeaderView(header), nil
}

func (s *Service) CreateServerHeader(ctx context.Context, payload *gen.CreateServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)
	name, source, err := validateHeaderWrite(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret, false)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	// Resolve the parent up front so a missing server is a 404 rather than an
	// empty insert, and so the audit event can carry the server's URL. The row
	// lock serializes header writes on this server for the duplicate check.
	server, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	if err := requireUnusedHeaderName(ctx, txRepo, server.ID, *authCtx.ProjectID, name, uuid.Nil); err != nil {
		return nil, err.LogWarn(ctx, logger)
	}

	header, err := headersRepo.CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      server.ID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		Value:                  conv.PtrToPGTextEmpty(payload.Value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(source),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "%s", headerNameInUseMessage(name)).LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "create remote mcp server header").LogError(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerHeaderCreate(ctx, dbtx, audit.LogRemoteMcpServerHeaderCreateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		RemoteMcpServerHeaderURN:  urn.NewRemoteMcpServerHeader(header.ID),
		RemoteMcpServerHeaderName: header.Name,
		RemoteMcpServerURN:        urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:        server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server header creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteMcpServerHeaderView(header), nil
}

func (s *Service) UpdateServerHeader(ctx context.Context, payload *gen.UpdateServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	// This read only locates the parent. The header is read again once the
	// parent is locked, so every decision below uses its current state.
	located, err := headersRepo.GetServerHeader(ctx, headerID, *authCtx.ProjectID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server header").LogError(ctx, logger)
	}

	server, err := txRepo.GetServerByIDForUpdate(ctx, repo.GetServerByIDForUpdateParams{
		ID:        located.RemoteMcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	existing, err := headersRepo.GetServerHeader(ctx, headerID, *authCtx.ProjectID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server header").LogError(ctx, logger)
	}

	// Omitting value on a header that is already a secret preserves the stored
	// value. Everything else is a full replace of the mutable fields.
	hasValue := payload.Value != nil && *payload.Value != ""
	hasValueFromRequestHeader := payload.ValueFromRequestHeader != nil && *payload.ValueFromRequestHeader != ""
	preserveStoredValue := isSecret && !hasValue && !hasValueFromRequestHeader && existing.IsSecret && existing.Value.Valid

	name, source, err := validateHeaderWrite(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret, preserveStoredValue)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, logger)
	}

	if err := requireUnusedHeaderName(ctx, txRepo, server.ID, *authCtx.ProjectID, name, existing.ID); err != nil {
		return nil, err.LogWarn(ctx, logger)
	}

	beforeView := mv.BuildRemoteMcpServerHeaderView(existing)

	header, err := headersRepo.UpdateServerHeader(ctx, repo.UpdateServerHeaderParams{
		ID:                     headerID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		SetValue:               !preserveStoredValue,
		Value:                  conv.PtrToPGTextEmpty(payload.Value),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(source),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "%s", headerNameInUseMessage(name)).LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "update remote mcp server header").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteMcpServerHeaderView(header)

	if err := s.audit.LogRemoteMcpServerHeaderUpdate(ctx, dbtx, audit.LogRemoteMcpServerHeaderUpdateEvent{
		OrganizationID:                      authCtx.ActiveOrganizationID,
		ProjectID:                           *authCtx.ProjectID,
		Actor:                               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                    authCtx.Email,
		ActorSlug:                           nil,
		RemoteMcpServerHeaderURN:            urn.NewRemoteMcpServerHeader(header.ID),
		RemoteMcpServerHeaderName:           header.Name,
		RemoteMcpServerURN:                  urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:                  server.Url,
		RemoteMcpServerHeaderSnapshotBefore: beforeView,
		RemoteMcpServerHeaderSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server header update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteServerHeader(ctx context.Context, payload *gen.DeleteServerHeaderPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteServerHeader(ctx, repo.DeleteServerHeaderParams{
		ID:        headerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server header").LogError(ctx, logger)
	}

	server, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        deleted.RemoteMcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerHeaderDelete(ctx, dbtx, audit.LogRemoteMcpServerHeaderDeleteEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		RemoteMcpServerHeaderURN:  urn.NewRemoteMcpServerHeader(deleted.ID),
		RemoteMcpServerHeaderName: deleted.Name,
		RemoteMcpServerURN:        urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:        server.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote mcp server header deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// validateHeaderWrite checks a header create or update against the remote
// header policy and returns the name and pass-through source to store: as
// entered, with surrounding spaces trimmed. A returned error's message is shown to the operator as is,
// so it names the header and how to fix it, never a value.
//
// It mirrors the remote_mcp_server_headers_value_source_check constraint:
// exactly one of value or value_from_request_header, and a pass-through header
// is not secret. Those checks are skipped when preserveStoredValue is set,
// because an update that keeps an existing secret's stored value supplies
// neither; the name is still validated.
func validateHeaderWrite(name string, value *string, valueFromRequestHeader *string, isSecret bool, preserveStoredValue bool) (string, *string, error) {
	const fieldNameRule = "it cannot contain spaces, control characters or separators such as ':'"

	headerName, err := trimHeaderName(name)
	if err != nil {
		return "", nil, fmt.Errorf("header name %q is not a valid HTTP header name: %s", name, fieldNameRule)
	}

	hasValue := value != nil && *value != ""
	hasValueFromRequestHeader := valueFromRequestHeader != nil && *valueFromRequestHeader != ""

	var source *string
	if hasValueFromRequestHeader {
		sourceName, err := trimHeaderName(*valueFromRequestHeader)
		if err != nil {
			return "", nil, fmt.Errorf("header %q reads request header %q, which is not a valid HTTP header name: %s", headerName, *valueFromRequestHeader, fieldNameRule)
		}
		source = &sourceName
	}

	if !preserveStoredValue {
		if hasValue == hasValueFromRequestHeader {
			return "", nil, fmt.Errorf("header %q must specify exactly one of value or value_from_request_header", headerName)
		}
		if hasValueFromRequestHeader && isSecret {
			return "", nil, fmt.Errorf("header %q: pass-through headers cannot be marked as secret", headerName)
		}
	}

	check := proxy.ConfiguredHeader{
		IsRequired:             false,
		Name:                   headerName,
		StaticValue:            conv.PtrValOr(value, ""),
		ValueFromRequestHeader: conv.PtrValOr(source, ""),
	}
	switch err := proxy.CheckRemoteHeader(check); {
	case err == nil:
		return headerName, source, nil
	case errors.Is(err, proxy.ErrProtectedSource):
		return "", nil, fmt.Errorf("header %q cannot be populated from request header %q. %s", headerName, *source, proxy.ProtectedSourceRemediation)
	case errors.Is(err, proxy.ErrReservedHeader):
		return "", nil, fmt.Errorf("header %q cannot be configured on a remote MCP server: Set-Cookie, Proxy-Authorization, MCP protocol headers and the Speakeasy caller assertion are reserved, and Cookie can only hold a static value", headerName)
	case errors.Is(err, proxy.ErrInvalidHeaderValue):
		return "", nil, fmt.Errorf("the value of header %q contains a character an HTTP header cannot carry, such as a line break", headerName)
	default:
		return "", nil, fmt.Errorf("header %q: %w", headerName, err)
	}
}

// trimHeaderName validates raw as an HTTP field name and returns it as
// entered, without surrounding spaces. Names are stored as typed; the proxy
// and the duplicate check match them case-insensitively.
func trimHeaderName(raw string) (string, error) {
	if _, err := proxy.NormalizeHeaderName(raw); err != nil {
		return "", fmt.Errorf("validate header name: %w", err)
	}
	return strings.Trim(raw, " "), nil
}

// headerNameInUseMessage explains a refused header name that another header of
// the server already uses.
func headerNameInUseMessage(name string) string {
	return fmt.Sprintf("this server already has a header named %q: header names match regardless of case, and '_' matches '-'", name)
}

// requireUnusedHeaderName refuses a name another live header of the server
// already uses, ignoring case. The caller must hold the parent server's row
// lock so concurrent writers cannot both pass the check.
func requireUnusedHeaderName(ctx context.Context, txRepo *repo.Queries, serverID uuid.UUID, projectID uuid.UUID, name string, excludeID uuid.UUID) *oops.ShareableError {
	exists, err := txRepo.ServerHeaderNameExists(ctx, repo.ServerHeaderNameExistsParams{
		RemoteMcpServerID: serverID,
		Name:              name,
		ExcludeID:         excludeID,
		ProjectID:         projectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check remote mcp server header name")
	}
	if exists {
		return oops.E(oops.CodeConflict, nil, "%s", headerNameInUseMessage(name))
	}
	return nil
}
