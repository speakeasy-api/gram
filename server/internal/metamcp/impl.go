package metamcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/meta_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/meta_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policylifecycle"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracerProvider           trace.TracerProvider
	tracer                   trace.Tracer
	logger                   *slog.Logger
	db                       *pgxpool.Pool
	auth                     *auth.Auth
	authz                    *authz.Engine
	audit                    *audit.Logger
	temporalEnv              *tenv.Environment
	networkAccessEligibility networkaccess.EligibilityChecker
	distributionAdmission    *admission.Guard
	publisher                plugins.PluginPublishSignaler
	publicationRequests      plugins.PublicationRequests
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
	temporalEnv *tenv.Environment,
	networkAccessEligibility networkaccess.EligibilityChecker,
) *Service {
	logger = logger.With(attr.SlogComponent("metamcp"))

	return &Service{
		tracerProvider:           tracerProvider,
		tracer:                   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/metamcp"),
		logger:                   logger,
		db:                       db,
		auth:                     auth.New(logger, db, sessions, authzEngine),
		authz:                    authzEngine,
		audit:                    auditLogger,
		temporalEnv:              temporalEnv,
		networkAccessEligibility: networkAccessEligibility,
		distributionAdmission:    nil,
		publisher:                nil,
		publicationRequests:      plugins.PublicationRequests{Enabled: false},
	}
}

func (s *Service) WithDistributionAdmission(guard *admission.Guard) *Service {
	s.distributionAdmission = guard
	return s
}

func (s *Service) WithPublicationRequests(enabled bool) *Service {
	s.publicationRequests.Enabled = enabled
	return s
}

func (s *Service) WithPluginPublisher(publisher plugins.PluginPublishSignaler) *Service {
	s.publisher = publisher
	return s
}

func (s *Service) signalPluginPublish(ctx context.Context, projectID uuid.UUID, userID string, gatewayID uuid.UUID, detached bool) {
	SignalGatewayPluginPublish(ctx, s.logger, s.db, s.publisher, projectID, userID, gatewayID, detached)
}

// writer is the shared gateway write core, so this API and the Platform MCP
// apply exactly the same rules.
func (s *Service) writer() GatewayWriter {
	return GatewayWriter{Logger: s.logger, Audit: s.audit, Authz: s.authz, Admission: s.distributionAdmission}
}

func actorFromAuthContext(authCtx *contextvalues.AuthContext) Actor {
	return Actor{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID, UserID: authCtx.UserID, Email: authCtx.Email}
}

// gatewayWriteRefusals maps each GatewayWriter refusal to the code this API
// has always returned for it.
var gatewayWriteRefusals = []struct {
	err  error
	code oops.Code
}{
	{ErrGatewayNotFound, oops.CodeNotFound},
	{ErrMembershipNotFound, oops.CodeNotFound},
	{ErrMemberServerNotFoundForUpdate, oops.CodeNotFound},
	{ErrIssuerNotInProject, oops.CodeInvalid},
	{ErrMemberServerNotInProject, oops.CodeInvalid},
	{ErrMemberServerHasNoSlug, oops.CodeInvalid},
	{ErrMemberServerUnproxied, oops.CodeInvalid},
	{ErrMemberServerIsHostedToolset, oops.CodeInvalid},
	{ErrMemberSharesBackend, oops.CodeConflict},
	{ErrMemberAlreadyPresent, oops.CodeConflict},
	{ErrMemberNeedsShadowApproval, oops.CodeConflict},
	{ErrMemberAdmissionUnverifiable, oops.CodeUnavailable},
}

func gatewayWriteError(ctx context.Context, logger *slog.Logger, err error, unexpected string) error {
	for _, refusal := range gatewayWriteRefusals {
		if errors.Is(err, refusal.err) {
			return oops.E(refusal.code, err, "%s", refusal.err.Error()).LogError(ctx, logger)
		}
	}
	return oops.E(oops.CodeUnexpected, err, "%s", unexpected).LogError(ctx, logger)
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

func (s *Service) CreateMetaMcpServer(ctx context.Context, payload *gen.CreateMetaMcpServerPayload) (*types.MetaMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	issuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}
	mode, err := networkaccess.ParseRequested(payload.NetworkAccessMode, networkaccess.Storage(networkaccess.ModePublicOnly))
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid network access mode").LogError(ctx, logger)
	}
	finalizeNetworkAccess, err := s.prepareNetworkAccessMode(ctx, authCtx.ActiveOrganizationID, mode)
	if err != nil {
		return nil, err
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := finalizeNetworkAccess.Finalize(ctx, dbtx); err != nil {
		return nil, fmt.Errorf("finalize network access admission: %w", err)
	}
	created, err := s.writer().CreateGateway(ctx, dbtx, actorFromAuthContext(authCtx), CreateGatewayInput{
		Name:                payload.Name,
		UserSessionIssuerID: issuerID,
		Visibility:          string(conv.PtrValOrEmpty(payload.Visibility, VisibilityPrivate)),
		NetworkAccessMode:   mode,
	})
	if err != nil {
		return nil, gatewayWriteError(ctx, logger, err, "create meta mcp server")
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildMetaMcpServerView(created), nil
}

func (s *Service) GetMetaMcpServer(ctx context.Context, payload *gen.GetMetaMcpServerPayload) (*types.MetaMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid meta mcp server id").LogError(ctx, s.logger)
	}

	row, err := repo.New(s.db).GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get meta mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildMetaMcpServerView(row), nil
}

func (s *Service) ListMetaMcpServers(ctx context.Context, payload *gen.ListMetaMcpServersPayload) (*gen.ListMetaMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListMetaMCPServers(ctx, repo.ListMetaMCPServersParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list meta mcp servers").LogError(ctx, s.logger)
	}

	return &gen.ListMetaMcpServersResult{MetaMcpServers: mv.BuildMetaMcpServerListView(rows)}, nil
}

func (s *Service) UpdateMetaMcpServer(ctx context.Context, payload *gen.UpdateMetaMcpServerPayload) (*types.MetaMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	// Validate the same normalized text that will be persisted, not the raw input.
	instructions := conv.PtrToPGTextTrimmed(conv.PtrStripNUL(payload.Instructions))
	if utf8.RuneCountInString(instructions.String) > 10000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "instructions must not exceed 10000 characters after normalization")
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid meta mcp server id").LogError(ctx, logger)
	}

	issuerID, err := conv.PtrToNullUUID(payload.UserSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_session_issuer_id").LogError(ctx, logger)
	}
	unlocked, err := repo.New(s.db).GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get meta mcp server").LogError(ctx, logger)
	}
	preflightMode, err := networkaccess.ParseRequested(payload.NetworkAccessMode, unlocked.NetworkAccessMode)
	if err != nil {
		if payload.NetworkAccessMode == nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid stored network access mode").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeBadRequest, err, "invalid network access mode").LogError(ctx, logger)
	}
	finalizeNetworkAccess, err := s.prepareNetworkAccessMode(ctx, authCtx.ActiveOrganizationID, preflightMode)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	if err := finalizeNetworkAccess.Finalize(ctx, dbtx); err != nil {
		return nil, fmt.Errorf("finalize network access admission: %w", err)
	}
	if err := admission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock distribution admission").LogError(ctx, logger)
	}
	txRepo := repo.New(dbtx)

	existing, err := txRepo.LockMetaMCPServer(ctx, repo.LockMetaMCPServerParams{
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock meta mcp server").LogError(ctx, logger)
	}

	// Defensive issuer wiring, mirroring create: an omitted issuer preserves
	// the existing one (matching UpdateMCPServer's COALESCE) and a gateway
	// that would end up issuer-less gets one minted, so no update path can
	// strand a gateway in the anonymous trap.
	if !issuerID.Valid {
		issuerID = existing.UserSessionIssuerID
	}
	if !issuerID.Valid {
		issuerID, err = mcpservers.MintServerUserSessionIssuer(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, payload.Name)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mint meta mcp issuer").LogError(ctx, logger)
		}
	}

	if err := s.lockIssuerReference(ctx, txRepo, *authCtx.ProjectID, issuerID); err != nil {
		return nil, err
	}

	mode, err := networkaccess.ParseRequested(payload.NetworkAccessMode, existing.NetworkAccessMode)
	if err != nil {
		if payload.NetworkAccessMode == nil {
			return nil, oops.E(oops.CodeUnexpected, err, "invalid stored network access mode").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeBadRequest, err, "invalid network access mode").LogError(ctx, logger)
	}
	if mode != preflightMode {
		return nil, oops.E(oops.CodeConflict, nil, "meta mcp server network access mode changed concurrently; retry the update")
	}
	visibility := existing.Visibility
	if payload.Visibility != nil {
		visibility = string(*payload.Visibility)
	}
	if visibility != VisibilityDisabled {
		if err := admission.CheckGatewayNetworkMode(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID, mode); err != nil {
			switch {
			case errors.Is(err, admission.ErrPrivateGatewayAudience):
				return nil, oops.E(oops.CodeConflict, err, "private-only gateway cannot be distributed to Everyone")
			default:
				return nil, oops.E(oops.CodeUnavailable, err, "check gateway plugin audience")
			}
		}
	}
	storedMode := networkaccess.Storage(mode)

	updated, err := txRepo.UpdateMetaMCPServer(ctx, repo.UpdateMetaMCPServerParams{
		Name:                 payload.Name,
		UserSessionIssuerID:  issuerID,
		Visibility:           conv.PtrToPGText((*string)(payload.Visibility)),
		NetworkAccessModeSet: payload.NetworkAccessMode != nil,
		NetworkAccessMode:    storedMode,
		InstructionsSet:      payload.Instructions != nil,
		// A blank submission restores the built-in instructions (NULL).
		Instructions:   instructions,
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update meta mcp server").LogError(ctx, logger)
	}

	// Consent wiring binds member provider clients to a specific issuer, so
	// pointing the gateway at a different issuer (or gaining one) would
	// silently orphan every members' tiles. Re-run the member attachment
	// against the new issuer instead of leaving that to a manual ceremony.
	rewiredIssuer := false
	if issuerID.Valid && (!existing.UserSessionIssuerID.Valid || existing.UserSessionIssuerID.UUID != issuerID.UUID) {
		identities, ierr := txRepo.ListMemberProviderIdentities(ctx, repo.ListMemberProviderIdentitiesParams{
			MetaMcpServerID: serverID,
			ProjectID:       *authCtx.ProjectID,
		})
		if ierr != nil {
			return nil, oops.E(oops.CodeUnexpected, ierr, "list member provider identities").LogError(ctx, logger)
		}
		for _, identity := range identities {
			if lerr := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, identity.RemoteSessionIssuerID.UUID); lerr != nil {
				return nil, oops.E(oops.CodeUnexpected, lerr, "lock remote session issuer for client binding").LogError(ctx, logger)
			}
			if _, aerr := txRepo.AutoAttachMemberProviderClient(ctx, repo.AutoAttachMemberProviderClientParams{
				GatewayIssuerID: issuerID.UUID,
				ProjectID:       *authCtx.ProjectID,
				MemberIssuerID:  identity.UserSessionIssuerID.UUID,
				RemoteIssuerID:  identity.RemoteSessionIssuerID.UUID,
			}); aerr != nil {
				return nil, oops.E(oops.CodeUnexpected, aerr, "attach member provider client").LogError(ctx, logger)
			}
		}
		rewiredIssuer = true
	}

	afterView := mv.BuildMetaMcpServerView(updated)

	if err := s.audit.LogMetaMcpServerUpdate(ctx, dbtx, audit.LogMetaMcpServerUpdateEvent{
		OrganizationID:              authCtx.ActiveOrganizationID,
		ProjectID:                   *authCtx.ProjectID,
		Actor:                       urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:            authCtx.Email,
		ActorSlug:                   nil,
		MetaMcpServerURN:            urn.NewMetaMcpServer(updated.ID),
		Name:                        updated.Name,
		MetaMcpServerSnapshotBefore: mv.BuildMetaMcpServerView(existing),
		MetaMcpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log meta mcp server update").LogError(ctx, logger)
	}

	if err := s.publicationRequests.Project(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue gateway publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Client-binding writers follow their commit with this resync so the
	// denormalized mcp_servers.remote_session_issuer_id cannot go stale when
	// the gateway issuer is shared with a server row.
	if rewiredIssuer {
		remotesessions.BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{issuerID.UUID})
	}
	s.signalPluginPublish(ctx, *authCtx.ProjectID, authCtx.UserID, serverID, false)

	return afterView, nil
}

// UpdateMetaMCPServerNetworkAccessModeInTransaction changes only an existing
// gateway's network access mode. The caller owns the transaction and performs
// authorization before calling. Admission is finalized before project and row locks.
func UpdateMetaMCPServerNetworkAccessModeInTransaction(ctx context.Context, tx pgx.Tx, auditLogger *audit.Logger, organizationID string, projectID uuid.UUID, actorUserID string, actorEmail *string, serverID uuid.UUID, mode networkaccess.Mode, finalize networkaccess.AdmissionFinalizer) (repo.MetaMcpServer, error) {
	if tx == nil || auditLogger == nil || organizationID == "" || projectID == uuid.Nil || actorUserID == "" || serverID == uuid.Nil {
		return repo.MetaMcpServer{}, fmt.Errorf("invalid gateway network access update input")
	}
	if _, err := networkaccess.Parse(string(mode)); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("validate gateway network access mode: %w", err)
	}
	if err := finalize.Finalize(ctx, tx); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("finalize network access admission: %w", err)
	}
	if err := admission.LockProject(ctx, tx, projectID); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("lock distribution admission: %w", err)
	}
	queries := repo.New(tx)
	existing, err := queries.LockMetaMCPServer(ctx, repo.LockMetaMCPServerParams{ID: serverID, OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("lock gateway: %w", err)
	}
	updated, err := tx.Exec(ctx, `UPDATE meta_mcp_servers SET network_access_mode = $1, updated_at = clock_timestamp() WHERE id = $2 AND organization_id = $3 AND project_id = $4 AND deleted IS FALSE`, networkaccess.Storage(mode), serverID, organizationID, projectID)
	if err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("update gateway network access mode: %w", err)
	}
	if updated.RowsAffected() != 1 {
		return repo.MetaMcpServer{}, pgx.ErrNoRows
	}
	after, err := queries.GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{ID: serverID, OrganizationID: organizationID, ProjectID: projectID})
	if err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("reload gateway: %w", err)
	}
	if err := auditLogger.LogMetaMcpServerUpdate(ctx, tx, audit.LogMetaMcpServerUpdateEvent{
		OrganizationID: organizationID, ProjectID: projectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, actorUserID), ActorDisplayName: actorEmail,
		ActorSlug: nil, MetaMcpServerURN: urn.NewMetaMcpServer(after.ID), Name: after.Name,
		MetaMcpServerSnapshotBefore: mv.BuildMetaMcpServerView(existing),
		MetaMcpServerSnapshotAfter:  mv.BuildMetaMcpServerView(after),
	}); err != nil {
		return repo.MetaMcpServer{}, fmt.Errorf("audit gateway network access update: %w", err)
	}
	return after, nil
}

func (s *Service) prepareNetworkAccessMode(ctx context.Context, organizationID string, mode networkaccess.Mode) (networkaccess.AdmissionFinalizer, error) {
	if mode.IsPublicOnly() {
		return networkaccess.NewAdmissionFinalizer(func(context.Context, pgx.Tx) error { return nil }), nil
	}
	if s.networkAccessEligibility == nil {
		return networkaccess.AdmissionFinalizer{}, oops.E(oops.CodeForbidden, nil, "private network access is not enabled for this organization")
	}
	finalize, err := s.networkAccessEligibility.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: organizationID, Mode: mode})
	if err != nil {
		return networkaccess.AdmissionFinalizer{}, oops.E(oops.CodeForbidden, err, "private network access is not enabled for this organization")
	}
	return networkaccess.NewAdmissionFinalizer(func(ctx context.Context, tx pgx.Tx) error {
		if err := finalize.Finalize(ctx, tx); err != nil {
			return oops.E(oops.CodeForbidden, err, "private network access is not enabled for this organization")
		}
		return nil
	}), nil
}

func (s *Service) DeleteMetaMcpServer(ctx context.Context, payload *gen.DeleteMetaMcpServerPayload) error {
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
		return oops.E(oops.CodeBadRequest, err, "invalid meta mcp server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	if err := admission.LockProject(ctx, dbtx, *authCtx.ProjectID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock distribution admission").LogError(ctx, logger)
	}

	txRepo := repo.New(dbtx)
	endpointsRepo := mcpendpointsrepo.New(dbtx)

	// Match the lock order used by endpoint mutations and generic server
	// deletion: custom domains, then endpoints, then the backend row.
	affectedDomainIDs, err := endpointsRepo.ListCustomDomainIDsByMetaMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMetaMCPServerIDParams{
		MetaMcpServerID: serverID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list custom domains for meta mcp server").LogError(ctx, logger)
	}
	domainsRepo := customdomainsrepo.New(dbtx)
	for _, domainID := range affectedDomainIDs {
		if _, err := domainsRepo.LockCustomDomainByID(ctx, domainID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "lock custom domain").LogError(ctx, logger)
		}
	}
	if _, err := endpointsRepo.LockMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMetaMCPServerIDParams{
		MetaMcpServerID: serverID,
		ProjectID:       *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock mcp endpoints").LogError(ctx, logger)
	}

	existing, err := txRepo.LockMetaMCPServer(ctx, repo.LockMetaMCPServerParams{
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock meta mcp server").LogError(ctx, logger)
	}

	// Post-meta-lock read is the authoritative root set: endpoint mutations
	// hold the meta row lock while writing a meta-backed endpoint, so no new
	// root can commit past this point, and rows here carry the pre-delete
	// is_domain_root.
	rootEndpoints, err := endpointsRepo.LockMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMetaMCPServerIDParams{
		MetaMcpServerID: serverID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock root mcp endpoints").LogError(ctx, logger)
	}
	rootEndpoints = slices.DeleteFunc(rootEndpoints, func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return !endpoint.IsDomainRoot.Valid || !endpoint.IsDomainRoot.Bool
	})

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
	metaURN := urn.NewMetaMcpServer(existing.ID)

	detached, err := pluginsrepo.New(dbtx).SoftDeletePluginServersByGatewayID(ctx, pluginsrepo.SoftDeletePluginServersByGatewayIDParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		GatewayID: uuid.NullUUID{UUID: existing.ID, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach gateway from plugins").LogError(ctx, logger)
	}
	for _, attachment := range detached {
		if err := s.audit.LogPluginServerRemove(ctx, dbtx, audit.LogPluginServerRemoveEvent{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
			Actor: actor, ActorDisplayName: authCtx.Email, ActorSlug: nil,
			PluginID: attachment.PluginID, PluginName: attachment.PluginName, PluginSlug: attachment.PluginSlug,
			ServerID: attachment.ID, ToolsetURN: nil, McpServerURN: nil, MetaMcpServerURN: &metaURN,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log gateway plugin detachment").LogError(ctx, logger)
		}
	}

	members, err := txRepo.DeleteMetaMCPMembersByMetaMCPServerID(ctx, repo.DeleteMetaMCPMembersByMetaMCPServerIDParams{
		MetaMcpServerID: existing.ID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete meta mcp memberships").LogError(ctx, logger)
	}
	for _, member := range members {
		if err := s.audit.LogMetaMcpMemberRemove(ctx, dbtx, audit.LogMetaMcpMemberEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			MetaMcpServerURN: metaURN,
			Name:             existing.Name,
			MembershipURN:    urn.NewMetaMcpServerMember(member.ID),
			McpServerURN:     urn.NewMcpServer(member.McpServerID),
			SortOrder:        member.SortOrder,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log meta mcp membership removal").LogError(ctx, logger)
		}
	}

	endpoints, err := endpointsRepo.SoftDeleteMCPEndpointsByMetaMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMetaMCPServerIDParams{
		MetaMcpServerID: existing.ID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete meta mcp endpoints").LogError(ctx, logger)
	}
	if err := s.logRootAutoClears(ctx, dbtx, authCtx, rootEndpoints); err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if err := s.audit.LogMcpEndpointDelete(ctx, dbtx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            actor,
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			McpEndpointURN:   urn.NewMcpEndpoint(endpoint.ID),
			Slug:             endpoint.Slug,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log meta mcp endpoint deletion").LogError(ctx, logger)
		}
	}

	deleted, err := txRepo.DeleteMetaMCPServer(ctx, repo.DeleteMetaMCPServerParams{
		ID:             serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete meta mcp server").LogError(ctx, logger)
	}
	// The admission lock taken above precedes the gateway row lock, as the
	// risk policy cleanup requires.
	deletedPolicies, err := policylifecycle.NewCleaner(s.tracerProvider, s.audit).SoftDeleteForMCPServer(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, deleted.ID, policylifecycle.Actor{
		Principal:   actor,
		DisplayName: authCtx.Email,
		Slug:        nil,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete meta mcp server risk policies").LogError(ctx, logger)
	}

	if err := s.audit.LogMetaMcpServerDelete(ctx, dbtx, audit.LogMetaMcpServerDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            actor,
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: metaURN,
		Name:             deleted.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log meta mcp server deletion").LogError(ctx, logger)
	}

	if err := s.publicationRequests.Project(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "enqueue gateway publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.signalPluginPublish(ctx, *authCtx.ProjectID, authCtx.UserID, serverID, len(detached) > 0)
	resultsCleaner := background.TemporalRiskPolicyResultsCleaner{TemporalEnv: s.temporalEnv, Logger: logger}
	resultsCleaner.CleanAll(ctx, *authCtx.ProjectID, deletedPolicies)
	if err := s.reconcileCustomDomains(ctx, rootDomainIDs(rootEndpoints)); err != nil {
		return err
	}

	return nil
}

func (s *Service) ListMetaMcpMembers(ctx context.Context, payload *gen.ListMetaMcpMembersPayload) (*gen.ListMetaMcpMembersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	metaID, err := uuid.Parse(payload.MetaMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogError(ctx, s.logger)
	}

	r := repo.New(s.db)

	if _, err := r.GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
		ID:             metaID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get meta mcp server").LogError(ctx, s.logger)
	}

	rows, err := r.ListMetaMCPMembers(ctx, repo.ListMetaMCPMembersParams{
		MetaMcpServerID: metaID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list meta mcp members").LogError(ctx, s.logger)
	}

	return &gen.ListMetaMcpMembersResult{Members: mv.BuildMetaMcpMemberListView(rows)}, nil
}

func (s *Service) AddMetaMcpMember(ctx context.Context, payload *gen.AddMetaMcpMemberPayload) (*types.MetaMcpMember, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	metaID, err := uuid.Parse(payload.MetaMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, metaID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	mcpServerID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, logger)
	}

	sortOrder, err := sortOrderValue(payload.SortOrder)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sort_order").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	actor := actorFromAuthContext(authCtx)
	writer := s.writer()
	meta, err := writer.LockGateway(ctx, dbtx, actor, metaID)
	if err != nil {
		return nil, gatewayWriteError(ctx, logger, err, "lock meta mcp server")
	}
	added, err := writer.AddMember(ctx, dbtx, actor, meta, mcpServerID, sortOrder)
	if err != nil {
		return nil, gatewayWriteError(ctx, logger, err, "add meta mcp member")
	}

	if err := s.publicationRequests.Project(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue gateway publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	AfterMembershipCommit(ctx, logger, s.db, s.publisher, actor, meta, added.IssuerWired)

	return mv.BuildMetaMcpMemberViewFromParts(added.Member, added.Server), nil
}

func (s *Service) UpdateMetaMcpMember(ctx context.Context, payload *gen.UpdateMetaMcpMemberPayload) (*types.MetaMcpMember, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	memberID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid membership id").LogError(ctx, logger)
	}

	sortOrder, err := sortOrderValue(&payload.SortOrder)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid sort_order").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	existing, err := repo.New(dbtx).LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp membership not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock meta mcp membership").LogError(ctx, logger)
	}

	meta, err := repo.New(dbtx).GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
		ID:             existing.MetaMcpServerID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get meta mcp server").LogError(ctx, logger)
	}

	actor := actorFromAuthContext(authCtx)
	updated, server, err := s.writer().SetMemberSortOrder(ctx, dbtx, actor, meta, memberID, sortOrder)
	if err != nil {
		return nil, gatewayWriteError(ctx, logger, err, "update meta mcp membership")
	}

	if err := s.publicationRequests.Project(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "enqueue gateway publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	s.signalPluginPublish(ctx, *authCtx.ProjectID, authCtx.UserID, meta.ID, false)

	return mv.BuildMetaMcpMemberViewFromParts(updated, server), nil
}

func (s *Service) RemoveMetaMcpMember(ctx context.Context, payload *gen.RemoveMetaMcpMemberPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	memberID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid membership id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	existing, err := repo.New(dbtx).LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "meta mcp membership not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock meta mcp membership").LogError(ctx, logger)
	}

	meta, err := repo.New(dbtx).GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
		ID:             existing.MetaMcpServerID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get meta mcp server").LogError(ctx, logger)
	}

	actor := actorFromAuthContext(authCtx)
	_, issuerWired, err := s.writer().RemoveMember(ctx, dbtx, actor, meta, memberID)
	if err != nil {
		return gatewayWriteError(ctx, logger, err, "remove meta mcp member")
	}

	if err := s.publicationRequests.Project(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "enqueue gateway publication").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	AfterMembershipCommit(ctx, logger, s.db, s.publisher, actor, meta, issuerWired)

	return nil
}

// lockIssuerReference validates an optional user session issuer reference and
// locks the issuer row for the duration of the transaction so a concurrent
// issuer delete cannot race the attach. A null issuer id is a no-op.
func (s *Service) lockIssuerReference(ctx context.Context, txRepo *repo.Queries, projectID uuid.UUID, issuerID uuid.NullUUID) error {
	if !issuerID.Valid {
		return nil
	}

	if _, err := txRepo.LockUserSessionIssuerForMetaMCP(ctx, repo.LockUserSessionIssuerForMetaMCPParams{
		UserSessionIssuerID: issuerID.UUID,
		ProjectID:           projectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeInvalid, err, "user_session_issuer_id does not reference a live issuer in this project").LogError(ctx, s.logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock user session issuer").LogError(ctx, s.logger)
	}

	return nil
}

// logRootAutoClears emits a custom-domain update audit event for every root
// endpoint tombstoned by a meta MCP server delete, mirroring the root cleanup
// audits that generic server deletion produces.
func (s *Service) logRootAutoClears(
	ctx context.Context,
	dbtx pgx.Tx,
	authCtx *contextvalues.AuthContext,
	rootEndpoints []mcpendpointsrepo.McpEndpoint,
) error {
	repository := customdomainsrepo.New(dbtx)
	for _, endpoint := range rootEndpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		domain, err := repository.GetCustomDomainByID(ctx, endpoint.CustomDomainID.UUID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "load custom domain for root cleanup audit").LogError(ctx, s.logger)
		}
		if err := s.audit.LogCustomDomainUpdate(ctx, dbtx, audit.LogCustomDomainUpdateEvent{
			OrganizationID:             authCtx.ActiveOrganizationID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			CustomDomainURN:            urn.NewCustomDomain(domain.ID),
			DomainName:                 domain.Domain,
			CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, endpoint.ID),
			CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log automatic root endpoint cleanup").LogError(ctx, s.logger)
		}
	}
	return nil
}

// rootDomainIDs collects the distinct custom domain ids referenced by the
// given endpoints, preserving the sorted endpoint order.
func rootDomainIDs(endpoints []mcpendpointsrepo.McpEndpoint) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(endpoints))
	result := make([]uuid.UUID, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		if _, ok := seen[endpoint.CustomDomainID.UUID]; ok {
			continue
		}
		seen[endpoint.CustomDomainID.UUID] = struct{}{}
		result = append(result, endpoint.CustomDomainID.UUID)
	}
	return result
}

func (s *Service) reconcileCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if s.temporalEnv == nil {
		return nil
	}
	var reconcileErrors []error
	for _, customDomainID := range customDomainIDs {
		_, err := (&background.CustomDomainRegistrationClient{TemporalEnv: s.temporalEnv}).ExecuteCustomDomainReconcile(ctx, customDomainID)
		if err != nil {
			reconcileErrors = append(reconcileErrors, oops.E(oops.CodeUnexpected, err, "start custom domain reconciliation").LogError(ctx, s.logger))
		}
	}
	return errors.Join(reconcileErrors...)
}

// sortOrderValue bounds-checks an optional sort order before narrowing it to
// the column's int32 type. A nil pointer defaults to 0.
func sortOrderValue(v *int) (int32, error) {
	value := conv.PtrValOr(v, 0)
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, errors.New("sort_order out of range")
	}
	return int32(value), nil
}
