package identityproviderconnections

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"weak"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/identity_provider_connections/server"
	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	jwksrepo "github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/okta"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/xaareadiness"
)

const (
	// discoveryTimeout bounds the issuer metadata probe of an admin-supplied host.
	discoveryTimeout = 10 * time.Second

	// verifyTimeout bounds one verification's Okta round trips under the row lock.
	verifyTimeout = 15 * time.Second

	// verifyRatePerMinute caps verification per organization; every run reads the customer's tenant.
	verifyRatePerMinute = 5

	// createRatePerMinute and createRateBurst bound creates per organization; every create mints a KMS key.
	createRatePerMinute = 1
	createRateBurst     = 3

	// createDailyCap bounds creates per organization per day, tombstones included.
	createDailyCap    = 5
	createDailyWindow = 24 * time.Hour

	// verifyMaxPages caps the confirmation reads at one page each.
	verifyMaxPages = 1

	// syncRatePerMinute caps manual applications syncs per organization.
	syncRatePerMinute = 1

	// listApplicationsLimit bounds one snapshot listing.
	listApplicationsLimit = 2000
)

// oktaClientIDPattern matches Okta application client ids.
var oktaClientIDPattern = regexp.MustCompile(`^0oa[A-Za-z0-9]{17,}$`)

// oktaObjectIDPattern bounds the admin-entered agent and app ids.
var oktaObjectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ApplicationSyncTrigger runs the applications snapshot coordinator promptly.
type ApplicationSyncTrigger interface {
	TriggerApplicationSync(ctx context.Context) error
}

// Discoverer fetches an issuer's authorization server metadata.
type Discoverer func(ctx context.Context, issuerURL string) (remotesessions.DiscoveredIssuerMetadata, error)

// NewDiscoverer resolves issuer metadata through Guardian's outbound policy.
func NewDiscoverer(policy *guardian.Policy) Discoverer {
	return func(ctx context.Context, issuerURL string) (remotesessions.DiscoveredIssuerMetadata, error) {
		return remotesessions.DiscoverIssuerMetadata(ctx, policy, issuerURL)
	}
}

type Service struct {
	tracer        trace.Tracer
	logger        *slog.Logger
	db            *pgxpool.Pool
	auth          *auth.Auth
	authz         *authz.Engine
	audit         *audit.Logger
	features      feature.Provider
	provisioner   *Provisioner
	oktaClients   okta.ClientFactory
	discover      Discoverer
	verifyLimiter *ratelimit.Limiter
	createLimiter *ratelimit.Limiter
	syncLimiter   *ratelimit.Limiter
	syncTrigger   ApplicationSyncTrigger
	createSlots   chan struct{}
	metrics       *serviceMetrics
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

// Share admission across service instances using the same pool. Weak keys and
// cleanup avoid retaining closed pools (notably short-lived test instances).
var createAdmissions sync.Map // weak.Pointer[pgxpool.Pool] -> chan struct{}

func createAdmission(db *pgxpool.Pool) chan struct{} {
	key := weak.Make(db)
	// Each admitted saga holds a lock connection and needs another connection
	// to make progress. A one-connection pool cannot safely run a create.
	slots := make(chan struct{}, db.Config().MaxConns/2)
	actual, loaded := createAdmissions.LoadOrStore(key, slots)
	if !loaded {
		runtime.AddCleanup(db, func(key weak.Pointer[pgxpool.Pool]) {
			createAdmissions.Delete(key)
		}, key)
	}
	admission, ok := actual.(chan struct{})
	if !ok {
		panic("unexpected create admission type")
	}
	return admission
}

// NewService wires the connection management API. A nil provisioner reports the feature unavailable.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	features feature.Provider,
	provisioner *Provisioner,
	oktaClients okta.ClientFactory,
	discover Discoverer,
	limitStore ratelimit.Store,
	syncTrigger ApplicationSyncTrigger,
) *Service {
	logger = logger.With(attr.SlogComponent("identityproviderconnections.api"))
	return &Service{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/identityproviderconnections"),
		logger:      logger,
		db:          db,
		auth:        auth.New(logger, db, sessions, authzEngine),
		authz:       authzEngine,
		audit:       auditLogger,
		features:    features,
		provisioner: provisioner,
		oktaClients: oktaClients,
		discover:    discover,
		verifyLimiter: ratelimit.New(limitStore, "identity-provider-connection-verify",
			ratelimit.PerMinute(verifyRatePerMinute),
			ratelimit.WithMetrics(meterProvider)),
		createLimiter: ratelimit.New(limitStore, "identity-provider-connection-create",
			ratelimit.PerMinute(createRatePerMinute).WithBurst(createRateBurst),
			ratelimit.WithMetrics(meterProvider)),
		syncLimiter: ratelimit.New(limitStore, "identity-provider-connection-sync-applications",
			ratelimit.PerMinute(syncRatePerMinute),
			ratelimit.WithMetrics(meterProvider)),
		syncTrigger: syncTrigger,
		createSlots: createAdmission(db),
		metrics:     newServiceMetrics(logger, meterProvider),
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

// authorize runs RBAC; a mutation additionally refuses support sessions and non-principal API keys.
func (s *Service) authorize(ctx context.Context, scope authz.Scope, mutation bool) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}
	if mutation {
		if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "identity provider connections cannot be changed with an API key").LogError(ctx, logger)
		}
		if contextvalues.IsSupportSession(ctx) {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "identity provider connections cannot be changed from a support session").LogError(ctx, logger)
		}
	}
	if s.provisioner == nil {
		return nil, logger, oops.E(oops.CodeUnavailable, nil, "identity provider connections are not configured on this deployment").LogError(ctx, logger)
	}
	return authCtx, logger, nil
}

// requireEnabled checks the rollout flag; a lookup failure reads as unavailable, never forbidden.
func (s *Service) requireEnabled(ctx context.Context, logger *slog.Logger, organizationID string) error {
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "okta connections availability could not be determined").LogError(ctx, logger)
	}
	enabled, err := s.features.IsFlagEnabled(ctx, feature.FlagOktaConnections, organizationID, feature.OrgProjectGroups(org.Slug, ""))
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "okta connections availability could not be determined").LogError(ctx, logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "okta connections are not enabled for this organization")
	}
	return nil
}

func (s *Service) load(ctx context.Context, logger *slog.Logger, organizationID string, id uuid.NullUUID) (*connectionRows, error) {
	row, err := repo.New(s.db).GetOktaIdentityProviderConnection(ctx, repo.GetOktaIdentityProviderConnectionParams{
		OrganizationID: organizationID,
		ID:             id,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connection").LogError(ctx, logger)
	}
	return s.withManagedClient(ctx, logger, s.db, row.IdentityProviderConnection, row.OktaIdentityProviderConnection)
}

// view renders the connection with what the last applications sync saw of the
// agent's linked app and whether a resource connection is recorded. A failed
// read leaves those steps unchecked.
func (s *Service) view(ctx context.Context, logger *slog.Logger, dbtx repo.DBTX, rows connectionRows) *gen.OktaIdentityProviderConnection {
	q := repo.New(dbtx)
	signal := agentSignal{App: nil, ConnectionRecorded: nil}

	recorded, err := q.HasOktaResourceConnection(ctx, repo.HasOktaResourceConnectionParams{
		OrganizationID:               rows.Connection.OrganizationID,
		IdentityProviderConnectionID: rows.Connection.ID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "read resource connections", attr.SlogError(err))
	} else {
		signal.ConnectionRecorded = &recorded
	}

	appID := rows.Okta.AgentAppID.String
	if appID == "" || !rows.Okta.ApplicationsSyncedAt.Valid {
		return buildConnectionView(rows, signal)
	}
	state, err := q.GetOktaAgentAppState(ctx, repo.GetOktaAgentAppStateParams{
		OrganizationID:               rows.Connection.OrganizationID,
		IdentityProviderConnectionID: rows.Connection.ID,
		OktaAppID:                    appID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		signal.App = &AgentAppSignal{Found: false, Active: false, Assigned: false}
	case err != nil:
		logger.ErrorContext(ctx, "read agent app state", attr.SlogError(err))
	default:
		signal.App = &AgentAppSignal{Found: true, Active: state.Status == "ACTIVE", Assigned: state.AssignmentCount > 0}
	}
	return buildConnectionView(rows, signal)
}

// withManagedClient reads on dbtx so a caller holding a transaction never waits on a second pool connection.
func (s *Service) withManagedClient(ctx context.Context, logger *slog.Logger, dbtx repo.DBTX, connection repo.IdentityProviderConnection, oktaRow repo.OktaIdentityProviderConnection) (*connectionRows, error) {
	managed, err := s.provisioner.GetManagedClientTx(ctx, dbtx, connection.OrganizationID, connection.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load connection credential").LogError(ctx, logger)
	}
	return &connectionRows{Connection: connection, Okta: oktaRow, Managed: managed}, nil
}

// withRevokedManagedClient tolerates managed rows the organization removed after revocation.
func (s *Service) withRevokedManagedClient(ctx context.Context, logger *slog.Logger, dbtx repo.DBTX, connection repo.IdentityProviderConnection, oktaRow repo.OktaIdentityProviderConnection) (*connectionRows, error) {
	managed, err := s.provisioner.GetManagedClientTx(ctx, dbtx, connection.OrganizationID, connection.ID)
	switch {
	case errors.Is(err, ErrNotProvisioned):
		managed = nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connection credential").LogError(ctx, logger)
	}
	return &connectionRows{Connection: connection, Okta: oktaRow, Managed: managed}, nil
}

func (s *Service) lock(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, organizationID string, id uuid.UUID) (*connectionRows, error) {
	row, err := repo.New(dbtx).LockOktaIdentityProviderConnection(ctx, repo.LockOktaIdentityProviderConnectionParams{
		ID:             id,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "lock connection").LogError(ctx, logger)
	}
	return s.withManagedClient(ctx, logger, dbtx, row.IdentityProviderConnection, row.OktaIdentityProviderConnection)
}

func parseConnectionID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid connection id")
	}
	return id, nil
}

func (s *Service) auditEvent(authCtx *contextvalues.AuthContext, id uuid.UUID, before, after *audit.IdentityProviderConnectionSnapshot) audit.LogIdentityProviderConnectionEvent {
	return audit.LogIdentityProviderConnectionEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ConnectionURN:    urn.NewIdentityProviderConnection(id),
		Provider:         ProviderOkta,
		SnapshotBefore:   before,
		SnapshotAfter:    after,
	}
}

// allow applies a limiter; an unreachable store fails open since the limiters protect capacity, not authorization.
func (s *Service) allow(ctx context.Context, logger *slog.Logger, limiter *ratelimit.Limiter, key, message string) error {
	res, err := limiter.Allow(ctx, key)
	switch {
	case err != nil:
		logger.WarnContext(ctx, "connection rate limiter unavailable, allowing", attr.SlogError(err))
	case !res.Allowed:
		return oops.E(oops.CodeRateLimitExceeded, nil, "%s", message)
	}
	return nil
}

func (s *Service) Create(ctx context.Context, payload *gen.CreatePayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	listingMode := conv.PtrValOr(payload.ListingMode, ListingModeCustomApp)
	if listingMode != ListingModeCustomApp && listingMode != ListingModeOIN {
		return nil, oops.E(oops.CodeBadRequest, nil, "listing_mode must be custom_app or oin")
	}
	orgURL, err := NormalizeOktaOrgURL(payload.OrgURL)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}

	// Admit without waiting and before borrowing the lock connection. Different
	// organizations must not fill the pool with locks and starve the saga writes.
	select {
	case s.createSlots <- struct{}{}:
		defer func() { <-s.createSlots }()
	default:
		return nil, oops.E(oops.CodeUnavailable, nil, "connection creation is busy or the database pool is too small; try again shortly")
	}

	// Serialize the entire create saga, including recovery, durable limits,
	// external KMS provisioning and subtype attachment, across all instances.
	createLock, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin create serialization").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return createLock.Rollback(context.WithoutCancel(ctx)) })
	// Do not queue holding pool connections: provisioning needs its own database
	// transactions and could otherwise be starved by waiting duplicate requests.
	locked, err := repo.New(createLock).LockIdentityProviderConnectionCreate(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "serialize connection creation").LogError(ctx, logger)
	}
	if !locked {
		return nil, oops.E(oops.CodeConflict, nil, "connection creation is already in progress")
	}

	if err := s.allow(ctx, logger, s.createLimiter, authCtx.ActiveOrganizationID, "create rate limit exceeded, try again shortly"); err != nil {
		return nil, err
	}
	q := repo.New(s.db)
	recent, err := q.CountIdentityProviderConnectionsCreatedSince(ctx, repo.CountIdentityProviderConnectionsCreatedSinceParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		Provider:       ProviderOkta,
		Since:          conv.ToPGTimestamptz(time.Now().Add(-createDailyWindow)),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count recent connections").LogError(ctx, logger)
	}
	if recent >= createDailyCap {
		return nil, oops.E(oops.CodeRateLimitExceeded, nil, "too many connections created in the last 24 hours")
	}

	if err := s.requireNoLiveConnection(ctx, logger, q, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	metadata, err := s.discoverOktaIssuer(ctx, logger, orgURL)
	if err != nil {
		s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeDiscoveryFailed)
		return nil, err
	}

	connection, issuerID, err := s.createConnectionRows(ctx, logger, authCtx.ActiveOrganizationID, orgURL, metadata)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(connection.ID.String()))

	managed, err := s.provisioner.ProvisionClient(ctx, ProvisionClientParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ConnectionID:   connection.ID,
		Provider:       ProviderOkta,
		IssuerID:       issuerID,
	})
	if err != nil {
		s.abandonConnection(ctx, logger, authCtx.ActiveOrganizationID, connection.ID, conv.ToNullUUID(issuerID))
		s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeProvisionFailed)
		if errors.Is(err, ErrSigningCredentialUnusable) {
			return nil, oops.E(oops.CodeUnavailable, err, "the signing credential for identity provider connections is unavailable").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "provision connection credential").LogError(ctx, logger)
	}

	rows, err := s.attachOktaRows(ctx, logger, authCtx, connection, managed, orgURL, listingMode)
	if err != nil {
		s.abandonConnection(ctx, logger, authCtx.ActiveOrganizationID, connection.ID, conv.ToNullUUID(issuerID))
		s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeProvisionFailed)
		return nil, err
	}

	s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeCreated)
	return s.view(ctx, logger, s.db, *rows), nil
}

// requireNoLiveConnection conflicts on a live connection; a parent whose create never wrote its Okta details is abandoned instead.
func (s *Service) requireNoLiveConnection(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string) error {
	live, err := q.GetLiveOktaIdentityProviderConnectionForOrganization(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "check for existing connection").LogError(ctx, logger)
	case live.OktaIdentityProviderConnectionID.Valid:
		s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeConflict)
		return oops.E(oops.CodeConflict, nil, "the organization already has an Okta connection; revoke it first")
	}
	logger.WarnContext(ctx, "abandoning connection left behind by an incomplete create", attr.SlogIdentityProviderConnectionID(live.IdentityProviderConnection.ID.String()))
	s.abandonConnection(ctx, logger, organizationID, live.IdentityProviderConnection.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	return nil
}

// discoverOktaIssuer probes the org URL and hard-fails on anything that would
// let the discovered document steer a signed assertion somewhere else.
func (s *Service) discoverOktaIssuer(ctx context.Context, logger *slog.Logger, orgURL string) (remotesessions.DiscoveredIssuerMetadata, error) {
	probeCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	var none remotesessions.DiscoveredIssuerMetadata
	metadata, err := s.discover(probeCtx, orgURL)
	if err != nil {
		return none, oops.E(oops.CodeFailedPrecondition, err, "could not discover the Okta org's authorization server metadata").LogError(ctx, logger)
	}
	if metadata.Issuer != orgURL {
		return none, oops.E(oops.CodeFailedPrecondition, nil, "the discovered issuer does not match the org url").LogError(ctx, logger)
	}
	tokenEndpoint, err := url.Parse(metadata.TokenEndpoint)
	if err != nil || tokenEndpoint.Scheme != "https" || tokenEndpoint.Host == "" {
		return none, oops.E(oops.CodeFailedPrecondition, err, "the discovered token endpoint is not an https URL").LogError(ctx, logger)
	}
	if strings.ToLower(tokenEndpoint.Host) != strings.TrimPrefix(orgURL, "https://") {
		return none, oops.E(oops.CodeFailedPrecondition, nil, "the discovered token endpoint is not on the org url host").LogError(ctx, logger)
	}
	supportsPrivateKeyJWT := slices.Contains(metadata.TokenEndpointAuthMethodsSupported, string(remotesessions.TokenEndpointAuthMethodPrivateKeyJWT))
	if !supportsPrivateKeyJWT {
		return none, oops.E(oops.CodeFailedPrecondition, nil, "the Okta org's authorization server does not advertise private_key_jwt client authentication").LogError(ctx, logger)
	}
	return metadata, nil
}

func connectionIssuerSlug(connectionID uuid.UUID) string {
	return "okta-connection-" + connectionID.String()
}

// createConnectionRows commits the pending connection and its organization issuer for the out-of-transaction provisioner.
func (s *Service) createConnectionRows(ctx context.Context, logger *slog.Logger, organizationID, orgURL string, metadata remotesessions.DiscoveredIssuerMetadata) (repo.IdentityProviderConnection, uuid.UUID, error) {
	var none repo.IdentityProviderConnection
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return none, uuid.Nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	connection, err := repo.New(dbtx).CreateIdentityProviderConnection(ctx, repo.CreateIdentityProviderConnectionParams{
		OrganizationID: organizationID,
		Provider:       ProviderOkta,
	})
	if err != nil {
		if isUniqueViolation(err) {
			s.metrics.recordCreate(ctx, ProviderOkta, createOutcomeConflict)
			return none, uuid.Nil, oops.E(oops.CodeConflict, err, "the organization already has an Okta connection; revoke it first")
		}
		return none, uuid.Nil, oops.E(oops.CodeUnexpected, err, "create connection").LogError(ctx, logger)
	}

	now := time.Now().UTC()
	issuer, err := remotesessionsrepo.New(dbtx).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		OrganizationID:                    conv.ToPGText(organizationID),
		Slug:                              connectionIssuerSlug(connection.ID),
		Issuer:                            orgURL,
		Name:                              conv.ToPGText("Okta (" + strings.TrimPrefix(orgURL, "https://") + ")"),
		LogoAssetID:                       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClientSetupDocumentationUrl:       pgtype.Text{String: "", Valid: false},
		AuthorizationEndpoint:             conv.ToPGTextEmpty(metadata.AuthorizationEndpoint),
		TokenEndpoint:                     conv.ToPGText(metadata.TokenEndpoint),
		RevocationEndpoint:                pgtype.Text{String: "", Valid: false},
		RegistrationEndpoint:              conv.ToPGTextEmpty(metadata.RegistrationEndpoint),
		JwksUri:                           pgtype.Text{String: "", Valid: false},
		ServiceDocumentation:              pgtype.Text{String: "", Valid: false},
		OpPolicyUri:                       pgtype.Text{String: "", Valid: false},
		OpTosUri:                          pgtype.Text{String: "", Valid: false},
		ScopesSupported:                   orEmpty(metadata.ScopesSupported),
		GrantTypesSupported:               orEmpty(metadata.GrantTypesSupported),
		ResponseTypesSupported:            orEmpty(metadata.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmpty(metadata.TokenEndpointAuthMethodsSupported),
		CodeChallengeMethodsSupported:     orEmpty(metadata.CodeChallengeMethodsSupported),
		ClientIDMetadataDocumentSupported: metadata.ClientIDMetadataDocumentSupported,
		UserinfoEndpoint:                  conv.ToPGTextEmpty(metadata.UserinfoEndpoint),
		IntrospectionEndpoint:             conv.ToPGTextEmpty(metadata.IntrospectionEndpoint),
		IntrospectionEndpointAuthMethodsSupported:  orEmpty(metadata.IntrospectionEndpointAuthMethodsSupported),
		IDTokenSigningAlgValuesSupported:           orEmpty(metadata.IDTokenSigningAlgValuesSupported),
		ClaimsSupported:                            orEmpty(metadata.ClaimsSupported),
		BackchannelLogoutSupported:                 pgtype.Bool{Bool: metadata.BackchannelLogoutSupported, Valid: true},
		AuthorizationResponseIssParameterSupported: pgtype.Bool{Bool: metadata.AuthorizationResponseIssParameterSupported, Valid: true},
		ScopeOverride:                              nil,
		ResourceIndicatorSupported:                 pgtype.Bool{Bool: false, Valid: false},
		Metadata:                                   metadata.Metadata,
		MetadataFetchedAt:                          conv.ToPGTimestamptz(now),
		MetadataLastError:                          metadata.UnreadableMessage,
		MetadataLastErrorUrl:                       metadata.UnreadableURL,
		Oidc:                                       false,
		Passthrough:                                false,
		TunneledMcpServerID:                        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	if err != nil {
		return none, uuid.Nil, oops.E(oops.CodeUnexpected, err, "create connection issuer").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return none, uuid.Nil, oops.E(oops.CodeUnexpected, err, "commit connection").LogError(ctx, logger)
	}
	return connection, issuer.ID, nil
}

// attachOktaRows records the Okta details once the credential exists and audits the creation.
func (s *Service) attachOktaRows(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, connection repo.IdentityProviderConnection, managed *ManagedClient, orgURL, listingMode string) (*connectionRows, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	oktaRow, err := repo.New(dbtx).CreateOktaIdentityProviderConnection(ctx, repo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connection.ID,
		OrganizationID:               authCtx.ActiveOrganizationID,
		OrgUrl:                       orgURL,
		IssuerUrl:                    orgURL,
		RemoteSessionIssuerID:        managed.IssuerID,
		RemoteSessionClientID:        managed.ClientRowID,
		ListingMode:                  listingMode,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, "this Okta org is already connected to another organization")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create okta connection details").LogError(ctx, logger)
	}

	rows := &connectionRows{Connection: connection, Okta: oktaRow, Managed: managed}
	if err := s.audit.LogIdentityProviderConnectionCreate(ctx, dbtx, s.auditEvent(authCtx, connection.ID, nil, snapshot(*rows))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log connection creation").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit okta connection details").LogError(ctx, logger)
	}
	return rows, nil
}

// abandonConnection withdraws any minted keys, then tombstones the managed
// client, its set, the connection, and its issuer in one transaction so no
// live client is left on a deleted issuer. Best-effort: the caller is already
// returning an error, and the next create retries a leftover.
func (s *Service) abandonConnection(ctx context.Context, logger *slog.Logger, organizationID string, connectionID uuid.UUID, issuerID uuid.NullUUID) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()

	managed, err := s.provisioner.GetManagedClient(cleanupCtx, organizationID, connectionID)
	switch {
	case errors.Is(err, ErrNotProvisioned):
		managed = nil
	case err != nil:
		logger.ErrorContext(ctx, "failed to load credential of abandoned connection; revoke it by hand", attr.SlogError(err))
		return
	default:
		s.oktaClients.Forget(managed.ClientRowID)
		if _, err := s.provisioner.RevokeClient(cleanupCtx, organizationID, connectionID); err != nil {
			logger.ErrorContext(ctx, "failed to revoke credential of abandoned connection; revoke it by hand", attr.SlogError(err))
			return
		}
		issuerID = conv.ToNullUUID(managed.IssuerID)
	}

	dbtx, err := s.db.Begin(cleanupCtx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to begin cleanup of abandoned connection", attr.SlogError(err))
		return
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(cleanupCtx) })

	q := repo.New(dbtx)
	rq := remotesessionsrepo.New(dbtx)
	if !issuerID.Valid {
		found, err := q.GetConnectionIssuerBySlug(cleanupCtx, repo.GetConnectionIssuerBySlugParams{
			OrganizationID: conv.ToPGText(organizationID),
			Slug:           connectionIssuerSlug(connectionID),
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			logger.ErrorContext(ctx, "failed to find issuer of abandoned connection", attr.SlogError(err))
			return
		default:
			issuerID = conv.ToNullUUID(found)
		}
	}
	if issuerID.Valid {
		if err := rq.LockRemoteSessionIssuerForClientBinding(cleanupCtx, issuerID.UUID); err != nil {
			logger.ErrorContext(ctx, "failed to lock issuer of abandoned connection", attr.SlogError(err))
			return
		}
	}
	if managed != nil {
		if _, err := rq.DeleteOrganizationRemoteSessionClient(cleanupCtx, remotesessionsrepo.DeleteOrganizationRemoteSessionClientParams{
			ID:             managed.ClientRowID,
			OrganizationID: conv.ToPGText(organizationID),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to tombstone client of abandoned connection", attr.SlogError(err))
			return
		}
		if _, err := jwksrepo.New(dbtx).SoftDeleteJsonWebKeySet(cleanupCtx, jwksrepo.SoftDeleteJsonWebKeySetParams{
			ID:             managed.JSONWebKeySetID,
			OrganizationID: organizationID,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to tombstone key set of abandoned connection", attr.SlogError(err))
			return
		}
	}
	if _, err := q.SoftDeleteIdentityProviderConnection(cleanupCtx, repo.SoftDeleteIdentityProviderConnectionParams{
		ID:             connectionID,
		OrganizationID: organizationID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ErrorContext(ctx, "failed to tombstone abandoned connection", attr.SlogError(err))
		return
	}
	if issuerID.Valid {
		if _, err := rq.DeleteOrganizationRemoteSessionIssuer(cleanupCtx, remotesessionsrepo.DeleteOrganizationRemoteSessionIssuerParams{
			ID:             issuerID.UUID,
			OrganizationID: conv.ToPGText(organizationID),
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			logger.ErrorContext(ctx, "failed to tombstone abandoned connection issuer", attr.SlogError(err))
			return
		}
	}
	if err := dbtx.Commit(cleanupCtx); err != nil {
		logger.ErrorContext(ctx, "failed to commit cleanup of abandoned connection", attr.SlogError(err))
	}
}

func (s *Service) SubmitClientID(ctx context.Context, payload *gen.SubmitClientIDPayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	clientID := strings.TrimSpace(payload.ClientID)
	if !oktaClientIDPattern.MatchString(clientID) {
		return nil, oops.E(oops.CodeBadRequest, nil, "client_id must be an Okta application client id (0oa followed by at least 17 alphanumerics)")
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	if err := s.allow(ctx, logger, s.verifyLimiter, authCtx.ActiveOrganizationID, "verify rate limit exceeded, try again shortly"); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	before, err := s.lock(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}
	if before.clientIDSubmitted() {
		return nil, oops.E(oops.CodeConflict, nil, "the client id was already submitted; revoke the connection to change it")
	}
	if before.Connection.Status != StatusPending {
		return nil, oops.E(oops.CodeConflict, nil, "the client id can only be submitted while the connection is pending")
	}

	managed, err := s.provisioner.SetClientID(ctx, dbtx, SetClientIDParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ConnectionID:     id,
		Provider:         ProviderOkta,
		ClientID:         clientID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
	})
	switch {
	case errors.Is(err, ErrClientIDAlreadySet):
		return nil, oops.E(oops.CodeConflict, err, "the client id was already submitted; revoke the connection to change it")
	case errors.Is(err, ErrClientIDInUse):
		return nil, oops.E(oops.CodeConflict, err, "this client id is already registered against the Okta org")
	case errors.Is(err, ErrNotProvisioned):
		return nil, oops.E(oops.CodeFailedPrecondition, err, "the connection has no credential to bind the client id to")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "record client id").LogError(ctx, logger)
	}

	// The factory memoizes per client row; drop the placeholder configuration.
	s.oktaClients.Forget(managed.ClientRowID)
	current := connectionRows{Connection: before.Connection, Okta: before.Okta, Managed: managed}
	outcome, err := s.runVerification(ctx, logger, current)
	if err != nil {
		// The client id is only persisted once Okta answered for it.
		if rollbackErr := dbtx.Rollback(ctx); rollbackErr != nil {
			logger.ErrorContext(ctx, "failed to roll back rejected client id submission", attr.SlogError(rollbackErr))
		}
		s.recordFailure(ctx, logger, authCtx, before, err, true)
		return nil, s.mapVerificationError(ctx, logger, err)
	}

	after, err := s.persistVerification(ctx, logger, q, current, outcome)
	if err != nil {
		s.oktaClients.Forget(managed.ClientRowID)
		return nil, err
	}
	if err := s.audit.LogIdentityProviderConnectionSubmitClientID(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(*after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log client id submission").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit client id submission").LogError(ctx, logger)
	}
	s.metrics.recordVerify(ctx, ProviderOkta, outcome.Status)
	return s.view(ctx, logger, s.db, *after), nil
}

func (s *Service) Verify(ctx context.Context, payload *gen.VerifyPayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	if err := s.allow(ctx, logger, s.verifyLimiter, authCtx.ActiveOrganizationID, "verify rate limit exceeded, try again shortly"); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	before, err := s.lock(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}
	if !before.clientIDSubmitted() {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "submit the Okta client id before verifying")
	}

	outcome, err := s.runVerification(ctx, logger, *before)
	if err != nil {
		if rollbackErr := dbtx.Rollback(ctx); rollbackErr != nil {
			logger.ErrorContext(ctx, "failed to release connection lock after verification failure", attr.SlogError(rollbackErr))
		}
		s.recordFailure(ctx, logger, authCtx, before, err, false)
		return nil, s.mapVerificationError(ctx, logger, err)
	}

	after, err := s.persistVerification(ctx, logger, q, *before, outcome)
	if err != nil {
		return nil, err
	}
	if err := s.audit.LogIdentityProviderConnectionVerify(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(*after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log connection verification").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit connection verification").LogError(ctx, logger)
	}
	s.metrics.recordVerify(ctx, ProviderOkta, outcome.Status)
	return s.view(ctx, logger, s.db, *after), nil
}

// runVerification verifies the connection's current credential; any failure forgets the memoized client.
func (s *Service) runVerification(ctx context.Context, logger *slog.Logger, rows connectionRows) (*verificationOutcome, error) {
	client, err := s.oktaClients.Client(okta.Config{
		OrgURL:                rows.Okta.OrgUrl,
		ClientID:              rows.Managed.ClientID,
		AudienceFormat:        string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint),
		RemoteSessionClientID: rows.Managed.ClientRowID,
		OrganizationID:        rows.Connection.OrganizationID,
		JSONWebKeySetID:       rows.Managed.JSONWebKeySetID,
		MaxPages:              verifyMaxPages,
	})
	if err != nil {
		s.oktaClients.Forget(rows.Managed.ClientRowID)
		return nil, fmt.Errorf("build okta client: %w", err)
	}

	verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	outcome, err := verifyConnection(verifyCtx, client)
	if err != nil {
		s.oktaClients.Forget(rows.Managed.ClientRowID)
		logger.WarnContext(ctx, "okta connection verification failed", attr.SlogError(err))
		return nil, err
	}
	return outcome, nil
}

// recordFailure stores the typed failure outside the rolled-back attempt and
// audits it, in one transaction. The write is a compare-and-swap on the row
// the attempt locked: a concurrent run that committed in between wins. A
// rejected credential degrades a verified connection.
func (s *Service) recordFailure(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, rows *connectionRows, cause error, submittedClientID bool) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), keyCleanupTimeout)
	defer cancel()

	status := rows.Connection.Status
	outcome := verifyOutcomeUnreachable
	if errors.Is(cause, ErrCredentialRejected) {
		outcome = verifyOutcomeCredentialRejected
		if status != StatusPending {
			status = StatusDegraded
		}
	}
	s.metrics.recordVerify(ctx, ProviderOkta, outcome)

	dbtx, err := s.db.Begin(writeCtx)
	if err != nil {
		logger.ErrorContext(ctx, "failed to begin recording connection verification failure", attr.SlogError(err))
		return
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(writeCtx) })

	connection, err := repo.New(dbtx).RecordIdentityProviderConnectionVerificationFailure(writeCtx, repo.RecordIdentityProviderConnectionVerificationFailureParams{
		Status:            status,
		LastError:         conv.ToPGText(failureLastError(cause)),
		ID:                rows.Connection.ID,
		OrganizationID:    rows.Connection.OrganizationID,
		ExpectedUpdatedAt: rows.Connection.UpdatedAt,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.InfoContext(ctx, "connection changed since the failed verification started; failure not recorded")
		return
	case err != nil:
		logger.ErrorContext(ctx, "failed to record connection verification failure", attr.SlogError(err))
		return
	}
	after := connectionRows{Connection: connection, Okta: rows.Okta, Managed: rows.Managed}
	logFailure := s.audit.LogIdentityProviderConnectionVerify
	if submittedClientID {
		logFailure = s.audit.LogIdentityProviderConnectionSubmitClientID
	}
	if err := logFailure(writeCtx, dbtx, s.auditEvent(authCtx, rows.Connection.ID, snapshot(*rows), snapshot(after))); err != nil {
		logger.ErrorContext(ctx, "failed to audit connection verification failure", attr.SlogError(err))
		return
	}
	if err := dbtx.Commit(writeCtx); err != nil {
		logger.ErrorContext(ctx, "failed to commit connection verification failure", attr.SlogError(err))
	}
}

func (s *Service) mapVerificationError(ctx context.Context, logger *slog.Logger, err error) error {
	if errors.Is(err, ErrCredentialRejected) {
		return oops.E(oops.CodeFailedPrecondition, err, "Okta rejected the connection credential; check the client id and that the app fetches keys from the connection's JWKS URL")
	}
	return oops.E(oops.CodeUnavailable, err, "Okta could not be reached to verify the connection").LogError(ctx, logger)
}

// persistVerification records the outcome; last_verified_at only advances on a verified outcome.
func (s *Service) persistVerification(ctx context.Context, logger *slog.Logger, q *repo.Queries, rows connectionRows, outcome *verificationOutcome) (*connectionRows, error) {
	lastVerifiedAt := rows.Connection.LastVerifiedAt
	if outcome.Status == StatusVerified {
		lastVerifiedAt = conv.ToPGTimestamptz(outcome.VerifiedAt)
	}
	connection, err := q.UpdateIdentityProviderConnectionVerification(ctx, repo.UpdateIdentityProviderConnectionVerificationParams{
		Status:         outcome.Status,
		LastVerifiedAt: lastVerifiedAt,
		LastError:      conv.ToPGTextEmpty(outcome.lastError()),
		ID:             rows.Connection.ID,
		OrganizationID: rows.Connection.OrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record verification").LogError(ctx, logger)
	}
	oktaRow, err := q.UpdateOktaIdentityProviderConnectionVerification(ctx, repo.UpdateOktaIdentityProviderConnectionVerificationParams{
		OwnershipClaimed:             outcome.CredentialProven,
		DpopRequired:                 outcome.DPoPBound,
		GrantedScopes:                outcome.Granted,
		IdentityProviderConnectionID: rows.Connection.ID,
		OrganizationID:               rows.Connection.OrganizationID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == "okta_identity_provider_connections_issuer_url_key" {
			return nil, oops.E(oops.CodeConflict, nil, "this Okta org is already connected to another organization")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "record verification details").LogError(ctx, logger)
	}
	return &connectionRows{Connection: connection, Okta: oktaRow, Managed: rows.Managed}, nil
}

func (s *Service) Get(ctx context.Context, payload *gen.GetPayload) (*gen.GetIdentityProviderConnectionResult, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgRead, false)
	if err != nil {
		return nil, err
	}
	id := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if payload.ID != nil {
		parsed, err := parseConnectionID(*payload.ID)
		if err != nil {
			return nil, err
		}
		id = conv.ToNullUUID(parsed)
	}
	rows, err := s.load(ctx, logger, authCtx.ActiveOrganizationID, id)
	switch {
	case err != nil && !id.Valid && isOopsCode(err, oops.CodeNotFound):
		return &gen.GetIdentityProviderConnectionResult{Connection: nil}, nil
	case err != nil:
		return nil, err
	}
	return &gen.GetIdentityProviderConnectionResult{Connection: s.view(ctx, logger, s.db, *rows)}, nil
}

func (s *Service) RecordAgent(ctx context.Context, payload *gen.RecordAgentPayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	agentID := strings.TrimSpace(conv.PtrValOr(payload.AgentID, ""))
	agentAppID := strings.TrimSpace(conv.PtrValOr(payload.AgentAppID, ""))
	if agentID != "" && !oktaObjectIDPattern.MatchString(agentID) {
		return nil, oops.E(oops.CodeBadRequest, nil, "agent_id must be an Okta object id")
	}
	if agentAppID != "" && !oktaObjectIDPattern.MatchString(agentAppID) {
		return nil, oops.E(oops.CodeBadRequest, nil, "agent_app_id must be an Okta object id")
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	before, err := s.lock(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}
	if before.Okta.AgentID.String == agentID && before.Okta.AgentAppID.String == agentAppID {
		return s.view(ctx, logger, dbtx, *before), nil
	}
	oktaRow, err := q.UpdateOktaIdentityProviderConnectionAgent(ctx, repo.UpdateOktaIdentityProviderConnectionAgentParams{
		AgentID:                      conv.ToPGTextEmpty(agentID),
		AgentAppID:                   conv.ToPGTextEmpty(agentAppID),
		IdentityProviderConnectionID: id,
		OrganizationID:               authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record agent").LogError(ctx, logger)
	}
	// Resource confirmations belong to the recorded agent, not its replacement.
	if err := xaareadiness.DeleteConnectionReadiness(ctx, dbtx, authCtx.ActiveOrganizationID, id); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete readiness rows").LogError(ctx, logger)
	}
	after := connectionRows{Connection: before.Connection, Okta: oktaRow, Managed: before.Managed}
	if err := s.audit.LogIdentityProviderConnectionRecordAgent(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log agent record").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit agent record").LogError(ctx, logger)
	}
	return s.view(ctx, logger, s.db, after), nil
}

func (s *Service) Revoke(ctx context.Context, payload *gen.RevokePayload) (*gen.OktaIdentityProviderConnection, error) {
	authCtx, logger, err := s.authorize(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	id, err := parseConnectionID(payload.ID)
	if err != nil {
		return nil, err
	}
	logger = logger.With(attr.SlogIdentityProviderConnectionID(id.String()))

	existing, err := s.loadRevocable(ctx, logger, s.db, authCtx.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}

	// Key material goes first: a tombstone without it would strand live keys
	// behind a connection nothing can reach, whereas revoked keys under a
	// still-visible connection are repaired by a retry.
	if _, err := s.provisioner.RevokeClient(ctx, authCtx.ActiveOrganizationID, id); err != nil && !errors.Is(err, ErrNotProvisioned) {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke connection credential").LogError(ctx, logger)
	}
	s.oktaClients.Forget(existing.OktaIdentityProviderConnection.RemoteSessionClientID)

	if existing.IdentityProviderConnection.Deleted {
		return s.revokedView(ctx, logger, s.db, existing)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)

	before, err := s.lock(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
	if err != nil {
		if !isOopsCode(err, oops.CodeNotFound) {
			return nil, err
		}
		// A concurrent revoke won the lock; report its outcome.
		raced, err := s.loadRevocable(ctx, logger, dbtx, authCtx.ActiveOrganizationID, id)
		if err != nil {
			return nil, err
		}
		return s.revokedView(ctx, logger, dbtx, raced)
	}
	connection, err := q.RevokeIdentityProviderConnection(ctx, repo.RevokeIdentityProviderConnectionParams{
		ID:             id,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "revoke connection").LogError(ctx, logger)
	}
	oktaRow, err := q.SoftDeleteOktaIdentityProviderConnection(ctx, repo.SoftDeleteOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: id,
		OrganizationID:               authCtx.ActiveOrganizationID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "tombstone okta connection details").LogError(ctx, logger)
	}
	// Resource connections reference snapshot rows, so they go first.
	if err := xaareadiness.DeleteConnectionReadiness(ctx, dbtx, authCtx.ActiveOrganizationID, id); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete readiness rows").LogError(ctx, logger)
	}
	if err := oktaapplications.DeleteConnectionSnapshot(ctx, dbtx, authCtx.ActiveOrganizationID, id); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "delete applications snapshot").LogError(ctx, logger)
	}
	after := connectionRows{Connection: connection, Okta: oktaRow, Managed: before.Managed}
	if err := s.audit.LogIdentityProviderConnectionRevoke(ctx, dbtx, s.auditEvent(authCtx, id, snapshot(*before), snapshot(after))); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log connection revocation").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit connection revocation").LogError(ctx, logger)
	}

	// The managed client lookup reflects the revoked key set.
	rows, err := s.withRevokedManagedClient(ctx, logger, s.db, connection, oktaRow)
	if err != nil {
		return nil, err
	}
	return buildConnectionView(*rows, agentSignal{App: nil, ConnectionRecorded: nil}), nil
}

// loadRevocable reads through the tombstone; only a revoked tombstone is visible.
func (s *Service) loadRevocable(ctx context.Context, logger *slog.Logger, dbtx repo.DBTX, organizationID string, id uuid.UUID) (*repo.GetOktaIdentityProviderConnectionIncludingDeletedRow, error) {
	existing, err := repo.New(dbtx).GetOktaIdentityProviderConnectionIncludingDeleted(ctx, repo.GetOktaIdentityProviderConnectionIncludingDeletedParams{
		ID:             id,
		OrganizationID: organizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "connection not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load connection").LogError(ctx, logger)
	}
	if existing.IdentityProviderConnection.Deleted && existing.IdentityProviderConnection.Status != StatusRevoked {
		return nil, oops.E(oops.CodeNotFound, nil, "connection not found")
	}
	return &existing, nil
}

func (s *Service) revokedView(ctx context.Context, logger *slog.Logger, dbtx repo.DBTX, existing *repo.GetOktaIdentityProviderConnectionIncludingDeletedRow) (*gen.OktaIdentityProviderConnection, error) {
	rows, err := s.withRevokedManagedClient(ctx, logger, dbtx, existing.IdentityProviderConnection, existing.OktaIdentityProviderConnection)
	if err != nil {
		return nil, err
	}
	return buildConnectionView(*rows, agentSignal{App: nil, ConnectionRecorded: nil}), nil
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func isOopsCode(err error, code oops.Code) bool {
	var oopsErr *oops.ShareableError
	return errors.As(err, &oopsErr) && oopsErr.Code == code
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
