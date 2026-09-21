package oktaresourceconnections

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/http/okta_resource_connections/server"
	srv "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Client binding states reported per row.
const (
	ClientBindingBound     = "bound"
	ClientBindingSingle    = "single"
	ClientBindingAmbiguous = "ambiguous"
	ClientBindingMissing   = "missing"
)

// Okta-operated org domains; deep links are only built for these because a
// custom domain has no derivable admin host.
var oktaOrgHostSuffixes = []string{"okta.com", "oktapreview.com", "okta-emea.com", "okta.mil"}

// Okta application instance ids.
var oktaAppIDPattern = regexp.MustCompile(`^0oa[A-Za-z0-9]{10,40}$`)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	audit    *audit.Logger
	features feature.Provider
	now      func() time.Time
}

var (
	_ srv.Service = (*Service)(nil)
	_ srv.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("oktaresourceconnections"))
	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessionManager, authzEngine),
		authz:    authzEngine,
		audit:    auditLogger,
		features: features,
		now:      time.Now,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := srv.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	gen.Mount(
		mux,
		gen.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// DeleteForConnection removes every resource connection recorded for an
// identity provider connection; the revoke path calls it inside its own
// transaction.
func DeleteForConnection(ctx context.Context, dbtx repo.DBTX, organizationID string, connectionID uuid.UUID) error {
	_, err := repo.New(dbtx).DeleteResourceConnectionsForConnection(ctx, repo.DeleteResourceConnectionsForConnectionParams{OrganizationID: organizationID, IdentityProviderConnectionID: connectionID})
	if err != nil {
		return fmt.Errorf("delete resource connections: %w", err)
	}
	return nil
}

// ExistForConnection reports whether any resource connection is recorded
// for an identity provider connection.
func ExistForConnection(ctx context.Context, dbtx repo.DBTX, organizationID string, connectionID uuid.UUID) (bool, error) {
	has, err := repo.New(dbtx).HasResourceConnections(ctx, repo.HasResourceConnectionsParams{OrganizationID: organizationID, IdentityProviderConnectionID: connectionID})
	if err != nil {
		return false, fmt.Errorf("read resource connections: %w", err)
	}
	return has, nil
}

// authorize runs RBAC; a mutation additionally refuses support sessions and non-principal API keys.
func (s *Service) authorize(ctx context.Context, mutation bool) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}
	if mutation {
		if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "resource connections cannot be changed with an API key").LogWarn(ctx, logger)
		}
		if contextvalues.IsSupportSession(ctx) {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "resource connections cannot be changed from a support session").LogWarn(ctx, logger)
		}
	}
	return authCtx, logger, nil
}

// readable keeps the servers the caller may read, as the MCP server lists do:
// org:admin does not imply mcp:read.
func (s *Service) readable(ctx context.Context, servers []repo.ListEligibleServersRow) ([]repo.ListEligibleServersRow, error) {
	checks := make([]authz.Check, len(servers))
	for i, sv := range servers {
		checks[i] = authz.MCPCheck(authz.ScopeMCPRead, sv.ID.String(), sv.ProjectID.String())
	}
	allowedIDs, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, fmt.Errorf("filter readable servers: %w", err)
	}
	allowed := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowed[id] = struct{}{}
	}
	out := make([]repo.ListEligibleServersRow, 0, len(allowedIDs))
	for _, sv := range servers {
		if _, ok := allowed[sv.ID.String()]; ok {
			out = append(out, sv)
		}
	}
	return out, nil
}

// actor is the canonical principal for the audit log; a principal credential
// carries no user id, so its URN stands in.
func actor(ctx context.Context, authCtx *contextvalues.AuthContext) urn.Principal {
	if p, ok := contextvalues.AuthenticatedActor(ctx); ok {
		return p
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)
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

// upstreamKey identifies the resource the administrator connects in Okta.
type upstreamKey struct {
	issuerID uuid.UUID
	resource string
}

// record is a resource connection row with the app instance label from the
// snapshot, when it still has one.
type record struct {
	repo.OktaResourceConnection
	label string
}

// row is one server joined with its readiness inputs.
type row struct {
	server     repo.ListEligibleServersRow
	connection *record
	clientID   string
	binding    string
	scopes     []string
	resource   string
	state      State
	reason     string
}

func (r row) key() upstreamKey {
	return upstreamKey{issuerID: r.server.IssuerID, resource: r.resource}
}

// snapshot is the org's connection, every derived row, and the lookups
// needed to derive one more.
type snapshot struct {
	connection   repo.GetLiveConnectionRow
	rows         []row
	total        int
	pending      int
	undiscovered int
	clients      map[uuid.UUID][]repo.ListIssuerClientsRow
	bindings     map[uuid.UUID][]repo.ListEMABindingsRow
	records      map[upstreamKey]*record
	deepLink     string
}

func (snap *snapshot) agentRecorded() bool {
	return snap.connection.AgentID.Valid && snap.connection.AgentID.String != ""
}

// derive fills the computed fields of a row from the snapshot's lookups.
func (snap *snapshot) derive(sv repo.ListEligibleServersRow) row {
	r := row{server: sv, connection: nil, clientID: "", binding: "", scopes: nil, resource: resourceIndicator(sv), state: "", reason: ""}
	r.connection = snap.records[r.key()]
	r.clientID, r.scopes, r.binding = resolveClient(sv, r.resource, snap.clients[sv.IssuerID], snap.bindings[sv.IssuerID])
	in := inputsFor(sv, r.connection, snap.agentRecorded())
	r.state = Derive(in)
	r.reason = NotApplicableReason(in)
	return r
}

// load reads the connection, the eligible servers, and the recorded resource
// connections, and derives every state. It reads outside any transaction.
func (s *Service) load(ctx context.Context, logger *slog.Logger, organizationID string) (*snapshot, error) {
	q := repo.New(s.db)
	connection, err := q.GetLiveConnection(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "connect an identity provider before checking readiness")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load identity provider connection").LogError(ctx, logger)
	}

	all, err := q.ListEligibleServers(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list servers").LogError(ctx, logger)
	}
	all, err = s.readable(ctx, all)
	if err != nil {
		return nil, err
	}
	snap := &snapshot{
		connection:   connection,
		rows:         make([]row, 0, len(all)),
		total:        0,
		pending:      0,
		undiscovered: 0,
		clients:      map[uuid.UUID][]repo.ListIssuerClientsRow{},
		bindings:     map[uuid.UUID][]repo.ListEMABindingsRow{},
		records:      map[upstreamKey]*record{},
		deepLink:     deepLink(connection),
	}
	servers := make([]repo.ListEligibleServersRow, 0, len(all))
	seen := map[uuid.UUID]bool{}
	issuerIDs := make([]uuid.UUID, 0, len(all))
	for _, sv := range all {
		if !sv.MetadataFetchedAt.Valid {
			snap.undiscovered++
			continue
		}
		servers = append(servers, sv)
		if !seen[sv.IssuerID] {
			seen[sv.IssuerID] = true
			issuerIDs = append(issuerIDs, sv.IssuerID)
		}
	}
	snap.total = len(servers)

	clients, err := q.ListIssuerClients(ctx, repo.ListIssuerClientsParams{IssuerIds: issuerIDs, OrganizationID: organizationID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list issuer clients").LogError(ctx, logger)
	}
	for _, c := range clients {
		snap.clients[c.RemoteSessionIssuerID] = append(snap.clients[c.RemoteSessionIssuerID], c)
	}
	bindings, err := q.ListEMABindings(ctx, repo.ListEMABindingsParams{OrganizationID: organizationID, IssuerIds: issuerIDs})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list identity chaining bindings").LogError(ctx, logger)
	}
	for _, b := range bindings {
		snap.bindings[b.RemoteSessionIssuerID] = append(snap.bindings[b.RemoteSessionIssuerID], b)
	}
	records, err := q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: organizationID, IdentityProviderConnectionID: connection.ID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list resource connections").LogError(ctx, logger)
	}
	for _, rec := range records {
		rc := &record{OktaResourceConnection: rec.OktaResourceConnection, label: rec.OktaApplicationLabel.String}
		snap.records[upstreamKey{issuerID: rc.RemoteSessionIssuerID, resource: rc.Resource}] = rc
	}

	for _, sv := range servers {
		r := snap.derive(sv)
		if r.state.Pending() {
			snap.pending++
		}
		snap.rows = append(snap.rows, r)
	}
	return snap, nil
}

func inputsFor(sv repo.ListEligibleServersRow, rc *record, agentRecorded bool) Inputs {
	return Inputs{
		AdvertisesIDJAG: AdvertisesIDJAG(sv.GrantTypesSupported, sv.AuthorizationGrantProfilesSupported),
		AgentRecorded:   agentRecorded,
		Confirmed:       rc != nil,
	}
}

// resourceIndicator is the RFC 9728 identifier when the server declares one,
// else the server URL.
func resourceIndicator(sv repo.ListEligibleServersRow) string {
	switch {
	case sv.TunneledResourceIdentifier.Valid && sv.TunneledResourceIdentifier.String != "":
		return strings.TrimRight(sv.TunneledResourceIdentifier.String, "/")
	case sv.RemoteUrl.Valid:
		return strings.TrimRight(sv.RemoteUrl.String, "/")
	case sv.UnproxiedUrl.Valid:
		return strings.TrimRight(sv.UnproxiedUrl.String, "/")
	default:
		return ""
	}
}

// resolveClient picks Speakeasy's client at the server's authorization server:
// bindings for the resource that agree on one client win; else the single
// attached client in the server's project or the organization, preferring
// one registered for this resource; else ambiguous or missing.
func resolveClient(sv repo.ListEligibleServersRow, resource string, clients []repo.ListIssuerClientsRow, bindings []repo.ListEMABindingsRow) (string, []string, string) {
	byID := make(map[uuid.UUID]repo.ListIssuerClientsRow, len(clients))
	for _, c := range clients {
		byID[c.ID] = c
	}
	resource = strings.TrimRight(resource, "/")
	var bound *repo.ListEMABindingsRow
	var boundScopes []string
	for i := range bindings {
		b := &bindings[i]
		if b.RemoteSessionIssuerID != sv.IssuerID || b.ProjectID != sv.ProjectID || strings.TrimRight(b.Resource, "/") != resource || !b.RemoteSessionClientID.Valid {
			continue
		}
		if sv.UserSessionIssuerID.Valid && b.UserSessionIssuerID != sv.UserSessionIssuerID.UUID {
			continue
		}
		if _, ok := byID[b.RemoteSessionClientID.UUID]; !ok {
			continue
		}
		c := byID[b.RemoteSessionClientID.UUID]
		for _, scope := range scopesOr(b.RequestedScopes, requestedScopes(c)) {
			if !slices.Contains(boundScopes, scope) {
				boundScopes = append(boundScopes, scope)
			}
		}
		if bound == nil {
			bound = b
			continue
		}
		if bound.RemoteSessionClientID.UUID != b.RemoteSessionClientID.UUID {
			return "", nil, ClientBindingAmbiguous
		}
	}
	if bound != nil {
		c := byID[bound.RemoteSessionClientID.UUID]
		slices.Sort(boundScopes)
		return c.ClientID, boundScopes, ClientBindingBound
	}
	var candidates, forResource []repo.ListIssuerClientsRow
	for _, c := range clients {
		if c.RemoteSessionIssuerID != sv.IssuerID || (c.ProjectID.Valid && c.ProjectID.UUID != sv.ProjectID) {
			continue
		}
		if sv.UserSessionIssuerID.Valid && !slices.Contains(c.UserSessionIssuerIds, sv.UserSessionIssuerID.UUID) {
			continue
		}
		candidates = append(candidates, c)
		if c.ResourceIdentifier.Valid && strings.TrimRight(c.ResourceIdentifier.String, "/") == resource {
			forResource = append(forResource, c)
		}
	}
	if len(forResource) > 0 {
		candidates = forResource
	}
	switch len(candidates) {
	case 0:
		return "", nil, ClientBindingMissing
	case 1:
		return candidates[0].ClientID, requestedScopes(candidates[0]), ClientBindingSingle
	default:
		return "", nil, ClientBindingAmbiguous
	}
}

func requestedScopes(c repo.ListIssuerClientsRow) []string {
	scopes, _ := (remotesessions.Client{ //nolint:exhaustruct // Only scope inputs are used by RequestedScopes.
		ClientScope:           c.Scope,
		IssuerScopeOverride:   c.IssuerScopeOverride,
		IssuerScopesSupported: c.IssuerScopesSupported,
	}).RequestedScopes()
	return scopesOr(nil, scopes)
}

func scopesOr(preferred, fallback []string) []string {
	if len(preferred) > 0 {
		return preferred
	}
	if fallback == nil {
		return []string{}
	}
	return fallback
}

func (s *Service) List(ctx context.Context, payload *srv.ListPayload) (*srv.ListOktaResourceConnectionsResult, error) {
	authCtx, logger, err := s.authorize(ctx, false)
	if err != nil {
		return nil, err
	}
	snap, err := s.load(ctx, logger, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	servers := make([]*srv.OktaResourceConnectionServer, 0, len(snap.rows))
	for _, r := range orderRows(snap.rows) {
		if !payload.IncludeAll && !r.state.Pending() {
			continue
		}
		servers = append(servers, buildRow(snap, r))
	}
	return &srv.ListOktaResourceConnectionsResult{
		Servers:           servers,
		PendingCount:      snap.pending,
		TotalCount:        snap.total,
		UndiscoveredCount: snap.undiscovered,
		AgentRecorded:     snap.agentRecorded(),
		ConnectionID:      conv.PtrEmpty(snap.connection.ID.String()),
		DeepLink:          conv.PtrEmpty(snap.deepLink),
	}, nil
}

// lockConnection holds the live connection for the write; a pending or
// revoked one cannot take confirmations.
func lockConnection(ctx context.Context, q *repo.Queries, organizationID string, connectionID uuid.UUID, logger *slog.Logger) error {
	_, err := q.LockLiveConnection(ctx, repo.LockLiveConnectionParams{ID: connectionID, OrganizationID: organizationID})
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.E(oops.CodeFailedPrecondition, nil, "verify the identity provider connection first")
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock identity provider connection").LogError(ctx, logger)
	}
	return nil
}

// confirmation is one validated item of a confirm request.
type confirmation struct {
	serverID uuid.UUID
	audience string
	appID    pgtype.Text
}

// parseConfirmations validates and dedupes the request; the last item for a
// server wins. Items are sorted so concurrent calls lock rows in one order.
func parseConfirmations(items []*srv.OktaResourceConnectionConfirmation) ([]confirmation, error) {
	byServer := make(map[uuid.UUID]confirmation, len(items))
	for _, item := range items {
		if item == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "empty confirmation")
		}
		id, err := uuid.Parse(item.McpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid server id")
		}
		audience, err := normalizeAudience(item.Audience)
		if err != nil {
			return nil, err
		}
		var appID pgtype.Text
		if item.OktaApplicationID != nil && *item.OktaApplicationID != "" {
			if !oktaAppIDPattern.MatchString(*item.OktaApplicationID) {
				return nil, oops.E(oops.CodeBadRequest, nil, "invalid identity provider application id")
			}
			appID = conv.ToPGText(*item.OktaApplicationID)
		}
		byServer[id] = confirmation{serverID: id, audience: audience, appID: appID}
	}
	out := make([]confirmation, 0, len(byServer))
	for _, c := range byServer {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b confirmation) int { return bytes.Compare(a.serverID[:], b.serverID[:]) })
	return out, nil
}

// normalizeAudience accepts the issuer URL as Okta shows it: https, a host,
// an optional path, nothing else. Okta compares audiences byte for byte, so
// only whitespace is trimmed.
func normalizeAudience(raw string) (string, error) {
	if utf8.RuneCountInString(raw) > 512 {
		return "", oops.E(oops.CodeBadRequest, nil, "audience must be at most 512 characters")
	}
	audience := strings.TrimSpace(raw)
	u, err := url.Parse(audience)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.ForceQuery || strings.ContainsAny(audience, "?#") || u.Opaque != "" {
		return "", oops.E(oops.CodeBadRequest, nil, "audience must be an https URL without query or fragment")
	}
	return audience, nil
}

func (s *Service) Confirm(ctx context.Context, payload *srv.ConfirmPayload) (*srv.ConfirmOktaResourceConnectionsResult, error) {
	authCtx, logger, err := s.authorize(ctx, true)
	if err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	items, err := parseConfirmations(payload.Connections)
	if err != nil {
		return nil, err
	}
	snap, err := s.load(ctx, logger, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	principal := actor(ctx, authCtx)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)
	if err := lockConnection(ctx, q, authCtx.ActiveOrganizationID, snap.connection.ID, logger); err != nil {
		return nil, err
	}

	out := make([]*srv.OktaResourceConnectionServer, 0, len(items))
	for _, item := range items {
		sv, err := q.GetEligibleServer(ctx, repo.GetEligibleServerParams{OrganizationID: authCtx.ActiveOrganizationID, McpServerID: item.serverID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "server not found")
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load server").LogError(ctx, logger)
		}
		server := repo.ListEligibleServersRow(sv)
		if visible, err := s.readable(ctx, []repo.ListEligibleServersRow{server}); err != nil {
			return nil, err
		} else if len(visible) == 0 {
			return nil, oops.E(oops.CodeNotFound, nil, "server not found")
		}
		if !AdvertisesIDJAG(server.GrantTypesSupported, server.AuthorizationGrantProfilesSupported) {
			return nil, oops.E(oops.CodeFailedPrecondition, nil, "the server's authorization server does not support Cross App Access")
		}
		resource := resourceIndicator(server)
		if resource == "" {
			return nil, oops.E(oops.CodeFailedPrecondition, nil, "the server has no resource indicator")
		}
		before, err := resourceConnectionForUpdate(ctx, q, authCtx.ActiveOrganizationID, snap.connection.ID, server.IssuerID, resource)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "load resource connection").LogError(ctx, logger)
		}
		after, err := q.UpsertResourceConnection(ctx, repo.UpsertResourceConnectionParams{
			OrganizationID:               authCtx.ActiveOrganizationID,
			IdentityProviderConnectionID: snap.connection.ID,
			RemoteSessionIssuerID:        server.IssuerID,
			Resource:                     resource,
			Audience:                     item.audience,
			OktaApplicationID:            item.appID,
		})
		if isForeignKeyViolation(err) {
			return nil, oops.E(oops.CodeBadRequest, nil, "the identity provider application is not in the latest applications snapshot")
		}
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "confirm connection").LogError(ctx, logger)
		}
		rc := &record{OktaResourceConnection: after, label: ""}
		if after.OktaApplicationID.Valid {
			label, err := q.GetOktaApplicationLabel(ctx, repo.GetOktaApplicationLabelParams{OrganizationID: authCtx.ActiveOrganizationID, IdentityProviderConnectionID: snap.connection.ID, OktaAppID: after.OktaApplicationID.String})
			if errors.Is(err, pgx.ErrNoRows) && item.appID.Valid {
				return nil, oops.E(oops.CodeBadRequest, nil, "the identity provider application is not in the latest applications snapshot")
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeUnexpected, err, "load application label").LogError(ctx, logger)
			}
			rc.label = label
		}
		snap.records[upstreamKey{issuerID: server.IssuerID, resource: resource}] = rc
		r := snap.derive(server)
		if err := s.audit.LogOktaResourceConnectionConfirm(ctx, dbtx, s.auditEvent(authCtx, principal, snap, r, before, rc)); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log confirmation").LogError(ctx, logger)
		}
		out = append(out, buildRow(snap, r))
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit confirmation").LogError(ctx, logger)
	}
	return &srv.ConfirmOktaResourceConnectionsResult{Servers: out}, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation
}

func resourceConnectionForUpdate(ctx context.Context, q *repo.Queries, organizationID string, connectionID, issuerID uuid.UUID, resource string) (*record, error) {
	rc, err := q.GetResourceConnectionForUpdate(ctx, repo.GetResourceConnectionForUpdateParams{OrganizationID: organizationID, IdentityProviderConnectionID: connectionID, RemoteSessionIssuerID: issuerID, Resource: resource})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock resource connection: %w", err)
	}
	return &record{OktaResourceConnection: rc, label: ""}, nil
}

func (s *Service) Reset(ctx context.Context, payload *srv.ResetPayload) (*srv.OktaResourceConnectionServer, error) {
	authCtx, logger, err := s.authorize(ctx, true)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id")
	}
	snap, err := s.load(ctx, logger, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	principal := actor(ctx, authCtx)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)
	if err := lockConnection(ctx, q, authCtx.ActiveOrganizationID, snap.connection.ID, logger); err != nil {
		return nil, err
	}
	sv, err := q.GetEligibleServer(ctx, repo.GetEligibleServerParams{OrganizationID: authCtx.ActiveOrganizationID, McpServerID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeNotFound, nil, "server not found")
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load server").LogError(ctx, logger)
	}
	server := repo.ListEligibleServersRow(sv)
	if visible, err := s.readable(ctx, []repo.ListEligibleServersRow{server}); err != nil {
		return nil, err
	} else if len(visible) == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "server not found")
	}
	resource := resourceIndicator(server)
	before, err := resourceConnectionForUpdate(ctx, q, authCtx.ActiveOrganizationID, snap.connection.ID, server.IssuerID, resource)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load resource connection").LogError(ctx, logger)
	}
	if before == nil {
		return nil, oops.E(oops.CodeFailedPrecondition, nil, "nothing is confirmed for this server")
	}
	if _, err := q.DeleteResourceConnection(ctx, repo.DeleteResourceConnectionParams{OrganizationID: authCtx.ActiveOrganizationID, IdentityProviderConnectionID: snap.connection.ID, RemoteSessionIssuerID: server.IssuerID, Resource: resource}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reset connection").LogError(ctx, logger)
	}
	delete(snap.records, upstreamKey{issuerID: server.IssuerID, resource: resource})
	r := snap.derive(server)
	if err := s.audit.LogOktaResourceConnectionReset(ctx, dbtx, s.auditEvent(authCtx, principal, snap, r, before, nil)); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log reset").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit reset").LogError(ctx, logger)
	}
	return buildRow(snap, r), nil
}

// orderRows puts pending servers first, then by name.
func orderRows(rows []row) []row {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, func(a, b row) int {
		if a.state.Pending() != b.state.Pending() {
			if a.state.Pending() {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(a.server.Name.String, b.server.Name.String); c != 0 {
			return c
		}
		return cmp.Compare(a.server.ID.String(), b.server.ID.String())
	})
	return out
}

// deepLink opens the add-resource-connection form of the AI agent in the
// identity provider console. Only Okta-operated domains have a derivable
// admin host.
func deepLink(c repo.GetLiveConnectionRow) string {
	if !c.AgentID.Valid || c.AgentID.String == "" {
		return ""
	}
	base, err := url.Parse(c.OrgUrl)
	if err != nil || base.Scheme != "https" {
		return ""
	}
	host := strings.ToLower(base.Hostname())
	for _, suffix := range oktaOrgHostSuffixes {
		label, ok := strings.CutSuffix(host, "."+suffix)
		if !ok || label == "" || strings.Contains(label, ".") {
			continue
		}
		label = strings.TrimSuffix(label, "-admin")
		return "https://" + label + "-admin." + suffix + "/admin/workload-principals/ai-agents/" + url.PathEscape(c.AgentID.String) + "/resource-connections/create"
	}
	return ""
}

func buildRow(snap *snapshot, r row) *srv.OktaResourceConnectionServer {
	out := &srv.OktaResourceConnectionServer{
		McpServerID:          r.server.ID.String(),
		ProjectID:            r.server.ProjectID.String(),
		ProjectSlug:          r.server.ProjectSlug,
		ServerName:           r.server.Name.String,
		ServerSlug:           r.server.Slug.String,
		State:                string(r.state),
		NotApplicableReason:  conv.PtrEmpty(r.reason),
		Pending:              r.state.Pending(),
		ResourceIndicator:    r.resource,
		IssuerID:             new(r.server.IssuerID.String()),
		ClientID:             conv.PtrEmpty(r.clientID),
		ClientBinding:        r.binding,
		Scopes:               scopesOr(nil, r.scopes),
		DeepLink:             conv.PtrEmpty(snap.deepLink),
		Audience:             nil,
		OktaApplicationID:    nil,
		OktaApplicationLabel: nil,
		ConfirmedAt:          nil,
	}
	if rc := r.connection; rc != nil {
		out.Audience = conv.PtrEmpty(rc.Audience)
		out.OktaApplicationID = conv.FromPGText[string](rc.OktaApplicationID)
		out.OktaApplicationLabel = conv.PtrEmpty(rc.label)
		out.ConfirmedAt = conv.PtrEmpty(conv.FromPGTimestamptz(rc.CreatedAt))
	}
	return out
}

func (s *Service) auditEvent(authCtx *contextvalues.AuthContext, principal urn.Principal, snap *snapshot, r row, before, after *record) audit.LogOktaResourceConnectionEvent {
	subject := before
	if after != nil {
		subject = after
	}
	return audit.LogOktaResourceConnectionEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             r.server.ProjectID,
		Actor:                 principal,
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		ResourceConnectionURN: urn.NewOktaResourceConnection(subject.ID),
		ServerName:            r.server.Name.String,
		ServerSlug:            r.server.Slug.String,
		SnapshotBefore:        auditSnapshot(r, before, snap.agentRecorded()),
		SnapshotAfter:         auditSnapshot(r, after, snap.agentRecorded()),
	}
}

func auditSnapshot(r row, rc *record, agentRecorded bool) *audit.OktaResourceConnectionSnapshot {
	if rc == nil {
		return nil
	}
	return &audit.OktaResourceConnectionSnapshot{
		ConnectionID:      rc.IdentityProviderConnectionID.String(),
		IssuerID:          rc.RemoteSessionIssuerID.String(),
		Resource:          rc.Resource,
		Audience:          rc.Audience,
		OktaApplicationID: rc.OktaApplicationID.String,
		State:             string(Derive(inputsFor(r.server, rc, agentRecorded))),
	}
}
