package metamcp

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer                   trace.Tracer
	logger                   *slog.Logger
	db                       *pgxpool.Pool
	auth                     *auth.Auth
	authz                    *authz.Engine
	audit                    *audit.Logger
	temporalEnv              *tenv.Environment
	networkAccessEligibility networkaccess.EligibilityChecker
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
		tracer:                   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/metamcp"),
		logger:                   logger,
		db:                       db,
		auth:                     auth.New(logger, db, sessions, authzEngine),
		authz:                    authzEngine,
		audit:                    auditLogger,
		temporalEnv:              temporalEnv,
		networkAccessEligibility: networkAccessEligibility,
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
	if err := s.preflightNetworkAccessMode(ctx, authCtx.ActiveOrganizationID, mode); err != nil {
		return nil, err
	}
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.admitNetworkAccessMode(ctx, dbtx, authCtx.ActiveOrganizationID, mode); err != nil {
		return nil, err
	}
	txRepo := repo.New(dbtx)

	if err := s.lockIssuerReference(ctx, txRepo, *authCtx.ProjectID, issuerID); err != nil {
		return nil, err
	}

	// A gateway without sign-in serves everyone anonymously, which hides
	// every private member and can hold no member credentials — a trap, not
	// a use case. Mint a dedicated issuer when the caller supplies none.
	if !issuerID.Valid {
		issuerID, err = mcpservers.MintServerUserSessionIssuer(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, payload.Name)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mint meta mcp issuer").LogError(ctx, logger)
		}
	}

	created, err := txRepo.CreateMetaMCPServer(ctx, repo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                payload.Name,
		UserSessionIssuerID: issuerID,
		Visibility:          string(conv.PtrValOrEmpty(payload.Visibility, VisibilityPrivate)),
		NetworkAccessMode:   networkaccess.Storage(mode),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create meta mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogMetaMcpServerCreate(ctx, dbtx, audit.LogMetaMcpServerCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(created.ID),
		Name:             created.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log meta mcp server creation").LogError(ctx, logger)
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
	if err := s.preflightNetworkAccessMode(ctx, authCtx.ActiveOrganizationID, preflightMode); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := s.admitNetworkAccessMode(ctx, dbtx, authCtx.ActiveOrganizationID, preflightMode); err != nil {
		return nil, err
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
	storedMode := networkaccess.Storage(mode)

	updated, err := txRepo.UpdateMetaMCPServer(ctx, repo.UpdateMetaMCPServerParams{
		Name:                 payload.Name,
		UserSessionIssuerID:  issuerID,
		Visibility:           conv.PtrToPGText((*string)(payload.Visibility)),
		NetworkAccessModeSet: payload.NetworkAccessMode != nil,
		NetworkAccessMode:    storedMode,
		ID:                   serverID,
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
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

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Client-binding writers follow their commit with this resync so the
	// denormalized mcp_servers.remote_session_issuer_id cannot go stale when
	// the gateway issuer is shared with a server row.
	if rewiredIssuer {
		remotesessions.BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{issuerID.UUID})
	}

	return afterView, nil
}

func (s *Service) preflightNetworkAccessMode(ctx context.Context, organizationID string, mode networkaccess.Mode) error {
	if mode.IsPublicOnly() {
		return nil
	}
	if s.networkAccessEligibility == nil {
		return oops.E(oops.CodeForbidden, nil, "private network access is not enabled for this organization")
	}
	if err := s.networkAccessEligibility.PreflightNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: organizationID, Mode: mode}); err != nil {
		return oops.E(oops.CodeForbidden, err, "private network access is not enabled for this organization")
	}
	return nil
}

func (s *Service) admitNetworkAccessMode(ctx context.Context, tx pgx.Tx, organizationID string, mode networkaccess.Mode) error {
	if mode.IsPublicOnly() {
		return nil
	}
	if s.networkAccessEligibility == nil {
		return oops.E(oops.CodeForbidden, nil, "private network access is not enabled for this organization")
	}
	if err := s.networkAccessEligibility.CheckNetworkAccess(ctx, tx, networkaccess.EligibilityInput{OrganizationID: organizationID, Mode: mode}); err != nil {
		return oops.E(oops.CodeForbidden, err, "private network access is not enabled for this organization")
	}
	return nil
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

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

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

	txRepo := repo.New(dbtx)

	meta, err := txRepo.LockMetaMCPServer(ctx, repo.LockMetaMCPServerParams{
		ID:             metaID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock meta mcp server").LogError(ctx, logger)
	}

	server, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "mcp_server_id does not reference a live server in this project").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	}

	// The gateway addresses members by qualified serverslug--toolname, so a
	// slugless server (legacy pre-2026-05 rows never updated since) can never
	// be reached; updating the server generates a slug. Unproxied backends
	// have no gateway-side dispatch path.
	if !server.Slug.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "mcp server has no slug; update the server to generate one before attaching it").LogError(ctx, logger)
	}
	if server.UnproxiedMcpServerID.Valid {
		return nil, oops.E(oops.CodeInvalid, nil, "unproxied mcp servers cannot be meta mcp members").LogError(ctx, logger)
	}

	// The meta lock above serializes concurrent adds, so this sees every
	// committed member.
	sharing, err := txRepo.CountMetaMCPMembersSharingBackend(ctx, repo.CountMetaMCPMembersSharingBackendParams{
		MetaMcpServerID:      metaID,
		ProjectID:            *authCtx.ProjectID,
		McpServerID:          mcpServerID,
		RemoteMcpServerID:    server.RemoteMcpServerID,
		TunneledMcpServerID:  server.TunneledMcpServerID,
		ToolsetID:            server.ToolsetID,
		UnproxiedMcpServerID: server.UnproxiedMcpServerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count meta mcp members sharing a backend").LogError(ctx, logger)
	}
	if sharing > 0 {
		return nil, oops.E(oops.CodeConflict, nil, "another member of this meta mcp server already fronts the same backend").LogError(ctx, logger)
	}

	member, err := txRepo.CreateMetaMCPMember(ctx, repo.CreateMetaMCPMemberParams{
		ProjectID:       *authCtx.ProjectID,
		MetaMcpServerID: metaID,
		McpServerID:     mcpServerID,
		SortOrder:       sortOrder,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp server is already a member of this meta mcp server").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "add meta mcp member").LogError(ctx, logger)
	}

	// Best-effort consent wiring: when both sides have the identity pieces —
	// the gateway an issuer, the member a stamped upstream AS with a client —
	// bind that client to the gateway's issuer so consent offers the member's
	// provider without a manual attach ceremony.
	wiredGatewayIssuer := false
	if meta.UserSessionIssuerID.Valid && server.RemoteSessionIssuerID.Valid && server.UserSessionIssuerID.Valid {
		// No DB constraint enforces one client per (issuer, upstream); every
		// client-binding writer serializes on this advisory lock instead.
		if lerr := remotesessionsrepo.New(dbtx).LockRemoteSessionIssuerForClientBinding(ctx, server.RemoteSessionIssuerID.UUID); lerr != nil {
			return nil, oops.E(oops.CodeUnexpected, lerr, "lock remote session issuer for client binding").LogError(ctx, logger)
		}
		attached, aerr := txRepo.AutoAttachMemberProviderClient(ctx, repo.AutoAttachMemberProviderClientParams{
			GatewayIssuerID: meta.UserSessionIssuerID.UUID,
			ProjectID:       *authCtx.ProjectID,
			MemberIssuerID:  server.UserSessionIssuerID.UUID,
			RemoteIssuerID:  server.RemoteSessionIssuerID.UUID,
		})
		if aerr != nil {
			return nil, oops.E(oops.CodeUnexpected, aerr, "attach member provider client").LogError(ctx, logger)
		}
		if attached > 0 {
			logger.InfoContext(ctx, "attached member provider client to meta mcp issuer",
				attr.SlogMetaMcpServerID(meta.ID.String()),
				attr.SlogMcpServerID(server.ID.String()))
		} else {
			// No bindable client, or the upstream is already bound: either
			// way the skip should be visible, matching autoConfigureAuth's
			// every-skip-has-a-reason posture.
			logger.InfoContext(ctx, "no member provider client attached to meta mcp issuer",
				attr.SlogMetaMcpServerID(meta.ID.String()),
				attr.SlogMcpServerID(server.ID.String()))
		}
		wiredGatewayIssuer = true
	}

	if err := s.audit.LogMetaMcpMemberAdd(ctx, dbtx, audit.LogMetaMcpMemberEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(meta.ID),
		Name:             meta.Name,
		MembershipURN:    urn.NewMetaMcpServerMember(member.ID),
		McpServerURN:     urn.NewMcpServer(member.McpServerID),
		SortOrder:        member.SortOrder,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log meta mcp member addition").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	// Every client-binding writer follows its commit with this resync so the
	// denormalized mcp_servers.remote_session_issuer_id cannot go stale when
	// the gateway issuer is shared with a server row.
	if wiredGatewayIssuer {
		remotesessions.BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{meta.UserSessionIssuerID.UUID})
	}

	return mv.BuildMetaMcpMemberViewFromParts(member, server), nil
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

	txRepo := repo.New(dbtx)

	existing, err := txRepo.LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "meta mcp membership not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock meta mcp membership").LogError(ctx, logger)
	}

	meta, err := txRepo.GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
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

	updated, err := txRepo.UpdateMetaMCPMemberSortOrder(ctx, repo.UpdateMetaMCPMemberSortOrderParams{
		SortOrder: sortOrder,
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update meta mcp membership").LogError(ctx, logger)
	}

	server, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        updated.McpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "member mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get member mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogMetaMcpMemberUpdate(ctx, dbtx, audit.LogMetaMcpMemberEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(meta.ID),
		Name:             meta.Name,
		MembershipURN:    urn.NewMetaMcpServerMember(updated.ID),
		McpServerURN:     urn.NewMcpServer(updated.McpServerID),
		SortOrder:        updated.SortOrder,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log meta mcp member update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

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

	txRepo := repo.New(dbtx)

	existing, err := txRepo.LockMetaMCPMember(ctx, repo.LockMetaMCPMemberParams{
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "meta mcp membership not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock meta mcp membership").LogError(ctx, logger)
	}

	meta, err := txRepo.GetMetaMCPServer(ctx, repo.GetMetaMCPServerParams{
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

	deleted, err := txRepo.DeleteMetaMCPMember(ctx, repo.DeleteMetaMCPMemberParams{
		ID:        memberID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove meta mcp member").LogError(ctx, logger)
	}

	if err := s.audit.LogMetaMcpMemberRemove(ctx, dbtx, audit.LogMetaMcpMemberEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		MetaMcpServerURN: urn.NewMetaMcpServer(meta.ID),
		Name:             meta.Name,
		MembershipURN:    urn.NewMetaMcpServerMember(deleted.ID),
		McpServerURN:     urn.NewMcpServer(deleted.McpServerID),
		SortOrder:        deleted.SortOrder,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log meta mcp member removal").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

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
