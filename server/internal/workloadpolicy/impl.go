// Package workloadpolicy implements the management API for an organization's
// workload identity trust policy: the external issuers it trusts, the subjects
// those issuers may present, and the agent each admitted workload inherits its
// policy from.
//
// Named for what it manages rather than after the Goa service (workloadIdentities)
// or the table prefix, so it cannot be mistaken for the workloadidentity package
// it sits beside. That one is the verification path's lookups, is imported by the
// token endpoint, and must stay free of the auth, audit and mv dependencies this
// package needs.
package workloadpolicy

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/catalog"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// Tier names in audit snapshots. A subject can be admitted at both tiers
// independently and withdrawing one leaves the other in force, so an entry that
// does not say which tier it describes cannot be acted on.
const (
	tierOrganization = "organization"
	tierProject      = "project"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	repo   *repo.Queries

	// catalog is the platforms the Access Hub offers to trust.
	catalog catalog.Source

	// sharedHosts are the hosts the deployment serves shared authorization
	// servers on, which the token endpoints it lists are derived from.
	sharedHosts authserver.Hosts
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
	sharedHosts authserver.Hosts,
) *Service {
	logger = logger.With(attr.SlogComponent("workloadpolicy.api"))
	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/workloadpolicy"),
		logger:  logger,
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		audit:   auditLogger,
		repo:    repo.New(db),
		catalog: catalog.Embedded(),

		sharedHosts: sharedHosts,
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

// tenancy is the caller's organization and selected project, resolved once per
// request. Both are needed on every query: organization_id is the tenancy
// boundary, and project_id narrows within it rather than replacing it, because
// the trust policy has an organization tier whose rows carry no project. A
// dashboard session never selects a project, so its projectID is NULL and it
// reads and writes the organization tier alone; only an API key names a project.
type tenancy struct {
	organizationID string
	projectID      uuid.NullUUID
	actor          urn.Principal
	actorEmail     *string
}

func (s *Service) resolve(ctx context.Context, scope authz.Scope) (tenancy, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return tenancy{}, oops.C(oops.CodeUnauthorized)
	}

	// The trust policy is configured for the organization as a whole, so the
	// organization is the resource the check names. There is no per-workload
	// resource a grant could narrow to, which is why the dashboard treats this
	// scope as unrestricted.
	if err := s.authz.Require(ctx, authz.Check{
		Scope:        scope,
		ResourceKind: "",
		ResourceID:   authCtx.ActiveOrganizationID,
		Dimensions:   nil,
	}); err != nil {
		return tenancy{}, err
	}

	// Only an API key selects a project. Every other caller, a dashboard
	// session included, resolves to the organization tier even when its auth
	// context carries a project, so the tier never depends on which security
	// schemes happened to populate ProjectID.
	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if _, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && authCtx.ProjectID != nil {
		projectID = conv.ToNullUUID(*authCtx.ProjectID)
	}

	return tenancy{
		organizationID: authCtx.ActiveOrganizationID,
		projectID:      projectID,
		actor:          urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		actorEmail:     authCtx.Email,
	}, nil
}

// tier names the scope a row was written at, for an audit snapshot.
func tier(projectID uuid.NullUUID) string {
	if projectID.Valid {
		return tierProject
	}
	return tierOrganization
}

// requestedTier resolves the project_id a write should carry. project_scoped is
// opt-in: the organization tier is the default because that is what a federated
// platform's issuer is, and a caller that has not thought about tiers should not
// silently write a row only one project can see.
// The contract declares Default(false), so the value arrives decided and this
// takes a plain bool rather than re-deriving the default from a nil pointer.
func (t tenancy) requestedTier(projectScoped bool) (uuid.NullUUID, error) {
	if !projectScoped {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	if !t.projectID.Valid {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, nil, "project_scoped requires a caller that names a project, and a dashboard session does not: omit it to write at the organization tier")
	}
	return t.projectID, nil
}

// loadPolicy reads the whole visible policy. Every write returns it, so a client
// replaces its view rather than merging into it and cannot show a stale list
// after a mutation.
func (s *Service) loadPolicy(ctx context.Context, dbtx repo.DBTX, t tenancy) (*gen.WorkloadIdentityPolicy, error) {
	q := repo.New(dbtx)

	issuers, err := q.ListWorkloadIssuers(ctx, repo.ListWorkloadIssuersParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing workload issuers").LogError(ctx, s.logger)
	}

	admissions, err := q.ListWorkloadAdmissions(ctx, repo.ListWorkloadAdmissionsParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing admitted workload subjects").LogError(ctx, s.logger)
	}

	return &gen.WorkloadIdentityPolicy{
		Issuers:    mv.BuildWorkloadIssuerListView(issuers),
		Admissions: mv.BuildWorkloadAdmissionListView(admissions),
	}, nil
}

func (s *Service) List(ctx context.Context, payload *gen.ListPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadRead)
	if err != nil {
		return nil, err
	}

	return s.loadPolicy(ctx, s.db, t)
}

func (s *Service) ListPlatforms(ctx context.Context, payload *gen.ListPlatformsPayload) (*gen.WorkloadPlatformCatalog, error) {
	if _, err := s.resolve(ctx, authz.ScopeWorkloadRead); err != nil {
		return nil, err
	}

	platforms, err := s.catalog.Platforms(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to load the platform catalog").LogError(ctx, s.logger)
	}

	return mv.BuildWorkloadPlatformCatalogView(platforms), nil
}

func (s *Service) ListTokenEndpoints(ctx context.Context, payload *gen.ListTokenEndpointsPayload) (*types.WorkloadTokenEndpoints, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadRead)
	if err != nil {
		return nil, err
	}

	rows, err := usersessions_repo.New(s.db).ListSharedUserSessionIssuersInOrganization(ctx, t.organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list token endpoints").LogError(ctx, s.logger)
	}

	// MCP servers are served on the server URL's host, whichever host their
	// issuer's authorization server is on.
	serverURL, err := url.Parse(s.sharedHosts.ServerURL)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to read the server URL").LogError(ctx, s.logger)
	}

	items := make([]*types.WorkloadTokenEndpoint, 0, len(rows))
	for _, row := range rows {
		issuer := row.UserSessionIssuer
		issuerURL, err := s.sharedHosts.SharedIssuerURL(issuer)
		if err != nil {
			// The issuer's MCP servers fall back to per-endpoint authorization
			// servers, so there is no shared token endpoint to point a platform at.
			s.logger.WarnContext(ctx, "skip user session issuer without a served shared authorization server", attr.SlogError(err))
			continue
		}
		tokenEndpoint, err := authserver.SharedTokenEndpoint(issuerURL)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to build token endpoint").LogError(ctx, s.logger)
		}
		items = append(items, mv.BuildWorkloadTokenEndpointView(issuer, row.ProjectName.String, issuerURL, tokenEndpoint, serverURL.Host))
	}

	return &types.WorkloadTokenEndpoints{Items: items}, nil
}

// requireTrustDomain enforces the WIMSE identifier draft's guidance that a trust
// domain is a fully qualified domain name: never an IP address, never a
// single-label host. An issuer is a trust anchor for machine identity, and both
// of those forms name something whose ownership cannot be established.
func requireTrustDomain(raw string, field string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "%s is not a valid URL", field)
	}

	host := parsed.Hostname()
	if net.ParseIP(host) != nil {
		return oops.E(oops.CodeInvalid, nil, "%s must name a domain, not an IP address", field)
	}
	if !strings.Contains(strings.TrimSuffix(host, "."), ".") {
		return oops.E(oops.CodeInvalid, nil, "%s must be a fully qualified domain name", field)
	}

	return nil
}

const (
	maxTags                 = 40
	maxTagRunes             = 64
	maxIssuerDescriptionLen = 500
)

// normalizeTags trims, rejects blanks, and drops duplicates, keeping the order
// the operator entered. Issuers and admissions share the limits, which match the
// CHECK on each table's column, so a list this accepts is one the insert can
// store.
func normalizeTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return []string{}, nil
	}

	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			return nil, oops.E(oops.CodeInvalid, nil, "tags must not be blank")
		}
		// Postgres text cannot hold a NUL byte, so the insert would fail.
		if strings.ContainsRune(tag, 0) {
			return nil, oops.E(oops.CodeInvalid, nil, "tags must not contain a NUL character")
		}
		if utf8.RuneCountInString(tag) > maxTagRunes {
			return nil, oops.E(oops.CodeInvalid, nil, "tags must be at most %d characters", maxTagRunes)
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}

	if len(normalized) > maxTags {
		return nil, oops.E(oops.CodeInvalid, nil, "at most %d tags are allowed", maxTags)
	}

	return normalized, nil
}

// validateJWKSURI applies the rules a JWKS URI is held to wherever one is
// written. It is the only field the verification path reads, so an http
// spelling would put key retrieval on the network in the clear.
func validateJWKSURI(raw string) error {
	if _, err := issuerurl.ParseHTTPSOnly(raw); err != nil {
		return oops.E(oops.CodeInvalid, err, "jwks_uri must be an https URL")
	}
	return requireTrustDomain(raw, "jwks_uri")
}

// normalizeIssuerName trims a name and refuses one that is blank after it or
// holds a NUL byte, which Postgres text cannot store.
func normalizeIssuerName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", oops.E(oops.CodeInvalid, nil, "name must not be blank")
	}
	if strings.ContainsRune(name, 0) {
		return "", oops.E(oops.CodeInvalid, nil, "name must not contain a NUL character")
	}
	return name, nil
}

// normalizeIssuerDescription trims a description and checks it as it will be
// stored. An empty result is stored as none.
func normalizeIssuerDescription(raw string) (string, error) {
	description := strings.TrimSpace(raw)
	if utf8.RuneCountInString(description) > maxIssuerDescriptionLen {
		return "", oops.E(oops.CodeInvalid, nil, "description must be at most %d characters", maxIssuerDescriptionLen)
	}
	if strings.ContainsRune(description, 0) {
		return "", oops.E(oops.CodeInvalid, nil, "description must not contain a NUL character")
	}
	return description, nil
}

// normalizeAdmissionName trims an admission's label. An empty result is stored
// as none, which is how an edit clears it.
func normalizeAdmissionName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if strings.ContainsRune(name, 0) {
		return "", oops.E(oops.CodeInvalid, nil, "name must not contain a NUL character")
	}
	return name, nil
}

// IssuerSnapshot is the audited state of an issuer row.
func IssuerSnapshot(row repo.WorkloadIssuer) *audit.WorkloadIssuerSnapshot {
	return &audit.WorkloadIssuerSnapshot{
		Name:                   row.Name,
		Issuer:                 row.Issuer,
		JwksURI:                row.JwksUri,
		Description:            conv.FromPGTextOrEmpty[string](row.Description),
		Tags:                   conv.DefaultSlice(row.Tags, []string{}),
		AllowWildcardAdmission: row.AllowWildcardAdmission,
		Tier:                   tier(row.ProjectID),
	}
}

// AdmissionSnapshot is the audited state of an admission row, under the issuer
// it names and the agent its (issuer, match_kind, subject) tuple is assigned.
func AdmissionSnapshot(row repo.WorkloadIdentityAdmission, issuer repo.WorkloadIssuer, assignedAgentID string) *audit.WorkloadAdmissionSnapshot {
	return &audit.WorkloadAdmissionSnapshot{
		Issuer:          issuer.Issuer,
		IssuerName:      issuer.Name,
		Subject:         row.Subject,
		MatchKind:       row.MatchKind,
		Name:            conv.FromPGTextOrEmpty[string](row.Name),
		Tags:            conv.DefaultSlice(row.Tags, []string{}),
		AssignedAgentID: assignedAgentID,
		Tier:            tier(row.ProjectID),
	}
}

func (s *Service) RegisterIssuer(ctx context.Context, payload *gen.RegisterIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	projectID, err := t.requestedTier(payload.ProjectScoped)
	if err != nil {
		return nil, err
	}

	// SEP-1933 requires https for both, and the jwks_uri is the security-critical
	// one: it is the only field the verification path reads, so an http spelling
	// would put key retrieval on the network in the clear.
	if _, err := issuerurl.ParseHTTPSOnly(payload.Issuer); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "issuer must be an https URL with no query or fragment")
	}
	if err := requireTrustDomain(payload.Issuer, "issuer"); err != nil {
		return nil, err
	}
	if err := validateJWKSURI(payload.JwksURI); err != nil {
		return nil, err
	}

	name, err := normalizeIssuerName(payload.Name)
	if err != nil {
		return nil, err
	}

	description, err := normalizeIssuerDescription(conv.PtrValOr(payload.Description, ""))
	if err != nil {
		return nil, err
	}

	// Defaulted here rather than in the design: a Goa default on a bool makes the
	// generated Go client send true for an explicit false.
	allowWildcard := conv.PtrValOr(payload.AllowWildcardAdmission, true)

	tags, err := normalizeTags(payload.Tags)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Stored as supplied, not canonicalized: admission matches the raw column,
	// and rewriting the operator's spelling here would change what an assertion
	// has to present.
	row, err := q.CreateWorkloadIssuer(ctx, repo.CreateWorkloadIssuerParams{
		OrganizationID:         t.organizationID,
		ProjectID:              projectID,
		Name:                   name,
		Description:            conv.ToPGTextEmpty(description),
		Tags:                   tags,
		Issuer:                 strings.TrimSpace(payload.Issuer),
		JwksUri:                strings.TrimSpace(payload.JwksURI),
		AllowWildcardAdmission: allowWildcard,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer named %q already exists at this tier", name)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error registering workload issuer").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadIssuerCreate(ctx, dbtx, audit.LogWorkloadIssuerCreateEvent{
		OrganizationID:      t.organizationID,
		ProjectID:           projectID,
		Actor:               t.actor,
		ActorDisplayName:    t.actorEmail,
		ActorSlug:           nil,
		IssuerURN:           urn.NewWorkloadIssuer(row.ID),
		IssuerName:          row.Name,
		IssuerSnapshotAfter: IssuerSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload issuer registration").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

func (s *Service) UpdateIssuer(ctx context.Context, payload *gen.UpdateIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "id is not a valid uuid")
	}

	// Every supplied field is validated before any row is read, so a bad edit is
	// refused the same way whether or not the issuer exists.
	var name *string
	if payload.Name != nil {
		normalized, err := normalizeIssuerName(*payload.Name)
		if err != nil {
			return nil, err
		}
		name = &normalized
	}

	var jwksURI *string
	if payload.JwksURI != nil {
		if err := validateJWKSURI(*payload.JwksURI); err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(*payload.JwksURI)
		jwksURI = &trimmed
	}

	var description *string
	if payload.Description != nil {
		normalized, err := normalizeIssuerDescription(*payload.Description)
		if err != nil {
			return nil, err
		}
		description = &normalized
	}

	// A nil slice is an omitted field; an empty one clears the tags.
	var tags []string
	if payload.Tags != nil {
		tags, err = normalizeTags(payload.Tags)
		if err != nil {
			return nil, err
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	getParams := repo.GetWorkloadIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             id,
	}

	// Tenancy-scoped, and read before the organization-wide lock, so a caller
	// cannot contend on a sibling project's issuer row it has no access to.
	if _, err := q.GetWorkloadIssuer(ctx, getParams); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "workload issuer not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading workload issuer").LogError(ctx, s.logger)
	}

	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{
		OrganizationID: t.organizationID,
		ID:             id,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "workload issuer not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error locking the workload issuer").LogError(ctx, s.logger)
	}

	// Read again under the lock, so the fields this edit leaves alone are merged
	// from the row as it stands rather than one a concurrent edit has replaced.
	existing, err := q.GetWorkloadIssuer(ctx, getParams)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "workload issuer not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading workload issuer").LogError(ctx, s.logger)
	}

	params := repo.UpdateWorkloadIssuerParams{
		Name:           existing.Name,
		Description:    existing.Description,
		Tags:           conv.DefaultSlice(existing.Tags, []string{}),
		JwksUri:        existing.JwksUri,
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             id,
	}
	if name != nil {
		params.Name = *name
	}
	if description != nil {
		params.Description = conv.ToPGTextEmpty(*description)
	}
	if tags != nil {
		params.Tags = tags
	}
	if jwksURI != nil {
		params.JwksUri = *jwksURI
	}

	row, err := q.UpdateWorkloadIssuer(ctx, params)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, "an issuer named %q already exists at this tier", params.Name)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error updating workload issuer").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadIssuerUpdate(ctx, dbtx, audit.LogWorkloadIssuerUpdateEvent{
		OrganizationID:       t.organizationID,
		ProjectID:            row.ProjectID,
		Actor:                t.actor,
		ActorDisplayName:     t.actorEmail,
		ActorSlug:            nil,
		IssuerURN:            urn.NewWorkloadIssuer(row.ID),
		IssuerName:           row.Name,
		IssuerSnapshotBefore: IssuerSnapshot(existing),
		IssuerSnapshotAfter:  IssuerSnapshot(row),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload issuer update").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

func (s *Service) WithdrawIssuer(ctx context.Context, payload *gen.WithdrawIssuerPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "id is not a valid uuid")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Tenancy-scoped, so an issuer in another organization is a not-found rather
	// than something checked after the fact.
	existing, err := q.GetWorkloadIssuer(ctx, repo.GetWorkloadIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "workload issuer not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading workload issuer").LogError(ctx, s.logger)
	}

	// Held before the cascade, so a concurrent admit cannot insert a child between
	// the children being tombstoned and the issuer being withdrawn.
	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{
		OrganizationID: t.organizationID,
		ID:             id,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking the workload issuer").LogError(ctx, s.logger)
	}

	// ON DELETE CASCADE only fires on a hard delete, so the children have to be
	// tombstoned here or a withdrawn issuer leaves admissions in the active set
	// pointing at a tombstone — invisible in the list, still a row.
	admissions, err := q.SoftDeleteWorkloadAdmissionsByIssuer(ctx, repo.SoftDeleteWorkloadAdmissionsByIssuerParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error withdrawing admitted subjects").LogError(ctx, s.logger)
	}

	assignments, err := q.SoftDeleteWorkloadAgentAssignmentsByIssuer(ctx, repo.SoftDeleteWorkloadAgentAssignmentsByIssuerParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing workload agent assignments").LogError(ctx, s.logger)
	}

	// Which agent each cascaded subject held, so the per-row audit entries
	// describe what was actually revoked rather than only that it was.
	agentBySubject := make(map[string]uuid.UUID, len(assignments))
	for _, assignment := range assignments {
		agentBySubject[assignment.MatchKind+"\x00"+assignment.Subject] = assignment.AgentID
	}

	// One entry per affected row, cause before effect: a single event on the
	// issuer would not let the log answer when a given subject stopped being
	// admitted.
	for _, admission := range admissions {
		assignedAgent := ""
		if agentID, ok := agentBySubject[admission.MatchKind+"\x00"+admission.Subject]; ok {
			assignedAgent = agentID.String()
		}

		if err := s.audit.LogWorkloadAdmissionWithdraw(ctx, dbtx, audit.LogWorkloadAdmissionWithdrawEvent{
			OrganizationID:          t.organizationID,
			ProjectID:               admission.ProjectID,
			Actor:                   t.actor,
			ActorDisplayName:        t.actorEmail,
			ActorSlug:               nil,
			AdmissionURN:            urn.NewWorkloadAdmission(admission.ID),
			AdmissionDisplayName:    admission.Subject,
			AdmissionSnapshotBefore: AdmissionSnapshot(admission, existing, assignedAgent),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error recording cascaded workload withdrawal").LogError(ctx, s.logger)
		}
	}

	deleted, err := q.SoftDeleteWorkloadIssuer(ctx, repo.SoftDeleteWorkloadIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             id,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error withdrawing workload issuer").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadIssuerDelete(ctx, dbtx, audit.LogWorkloadIssuerDeleteEvent{
		OrganizationID:       t.organizationID,
		ProjectID:            deleted.ProjectID,
		Actor:                t.actor,
		ActorDisplayName:     t.actorEmail,
		ActorSlug:            nil,
		IssuerURN:            urn.NewWorkloadIssuer(deleted.ID),
		IssuerName:           deleted.Name,
		IssuerSnapshotBefore: IssuerSnapshot(deleted),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload issuer withdrawal").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

func (s *Service) AdmitSubject(ctx context.Context, payload *gen.AdmitSubjectPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	projectID, err := t.requestedTier(payload.ProjectScoped)
	if err != nil {
		return nil, err
	}

	agentID, err := uuid.Parse(payload.AgentID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "agent_id is not a valid uuid")
	}

	tags, err := normalizeTags(payload.Tags)
	if err != nil {
		return nil, err
	}

	matchKind, err := workloadidentity.ParseMatchKind(payload.MatchKind)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "match_kind is not a recognised subject match kind")
	}

	canonical, err := issuerurl.ParseHTTPSOnly(payload.Issuer)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "issuer must be an https URL with no query or fragment")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Resolve, do not validate: the lookup is tenancy-scoped, so naming an issuer
	// the caller cannot see is impossible rather than guarded. The composite
	// foreign key is the backstop, not the first line.
	//
	// Scoped to the tier being written, not to the selected project. An
	// organization-tier admission that bound itself to a project-tier issuer
	// would be visible to every other project through the admission list's join,
	// and would outlive that project's own view of the issuer.
	issuers, err := q.FindWorkloadIssuersByIssuer(ctx, repo.FindWorkloadIssuersByIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      projectID,
		Issuers:        canonical.MatchCandidates(),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving workload issuer").LogError(ctx, s.logger)
	}
	if len(issuers) == 0 {
		return nil, oops.E(oops.CodeNotFound, nil, "no trusted issuer matches %q at this tier; register it first", payload.Issuer)
	}
	// The same issuer URL may legitimately be registered at both tiers, and the
	// verification path gives the project row precedence, so follow it rather
	// than asking an operator to withdraw a row that is doing its job.
	//
	// Precedence resolves ACROSS tiers only. Within the winning tier the issuer
	// column is not unique — two rows may share a URL under different names — and
	// picking one would silently decide which jwks_uri verifies this subject and
	// whether wildcards are permitted for it. That stays a refusal.
	candidates := issuers
	projectTier := make([]repo.WorkloadIssuer, 0, len(issuers))
	for _, candidate := range issuers {
		if candidate.ProjectID.Valid {
			projectTier = append(projectTier, candidate)
		}
	}
	if len(projectTier) > 0 {
		candidates = projectTier
	}
	if len(candidates) > 1 {
		return nil, oops.E(oops.CodeInvalid, nil, "%q matches %d trusted issuers at the same tier; withdraw the duplicates first", payload.Issuer, len(candidates))
	}
	issuerRow := candidates[0]

	// The early, legible refusal. The issuer's permission is re-checked on every
	// lookup, which is what makes clearing it revoke rules already written.
	if err := workloadidentity.ValidateSubjectRule(matchKind, payload.Subject, issuerRow.AllowWildcardAdmission); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "subject rule refused")
	}

	// Tenancy-scoped for the same reason as the issuer: another organization's
	// agent is a not-found, not a rejected insert.
	agent, err := q.GetOrganizationAgent(ctx, repo.GetOrganizationAgentParams{
		OrganizationID: t.organizationID,
		ID:             agentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "agent not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent").LogError(ctx, s.logger)
	}

	// Held before the children are written, so a concurrent withdrawal of this
	// issuer either happens first — making the insert below a foreign-key failure
	// against a tombstone the caller is told about — or waits and cascades over
	// these rows instead of leaving them behind it.
	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{
		OrganizationID: t.organizationID,
		ID:             issuerRow.ID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "the issuer was withdrawn while admitting this subject")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error locking the workload issuer").LogError(ctx, s.logger)
	}

	admission, err := q.CreateWorkloadAdmission(ctx, repo.CreateWorkloadAdmissionParams{
		OrganizationID:   t.organizationID,
		ProjectID:        projectID,
		WorkloadIssuerID: issuerRow.ID,
		Subject:          payload.Subject,
		MatchKind:        string(matchKind),
		Name:             conv.PtrToPGTextEmpty(payload.Name),
		Tags:             tags,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, oops.E(oops.CodeConflict, err, "that subject is already admitted at this tier")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error admitting workload subject").LogError(ctx, s.logger)
	}

	// Same transaction as the admission: a subject admitted with no agent is
	// refused at the token endpoint for having no agent, so that half-configured
	// state must not be reachable from here.
	if _, err := q.UpsertWorkloadAgentAssignment(ctx, repo.UpsertWorkloadAgentAssignmentParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: issuerRow.ID,
		Subject:          payload.Subject,
		MatchKind:        string(matchKind),
		AgentID:          agent.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error assigning the workload's agent").LogError(ctx, s.logger)
	}

	if err := s.audit.LogWorkloadAdmissionAdmit(ctx, dbtx, audit.LogWorkloadAdmissionAdmitEvent{
		OrganizationID:         t.organizationID,
		ProjectID:              projectID,
		Actor:                  t.actor,
		ActorDisplayName:       t.actorEmail,
		ActorSlug:              nil,
		AdmissionURN:           urn.NewWorkloadAdmission(admission.ID),
		AdmissionDisplayName:   admission.Subject,
		AdmissionSnapshotAfter: AdmissionSnapshot(admission, issuerRow, agent.ID.String()),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload admission").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

func (s *Service) UpdateSubject(ctx context.Context, payload *gen.UpdateSubjectPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "id is not a valid uuid")
	}

	// Every supplied field is validated before any row is read, so a bad edit is
	// refused the same way whether or not the admission exists.
	var name *string
	if payload.Name != nil {
		normalized, err := normalizeAdmissionName(*payload.Name)
		if err != nil {
			return nil, err
		}
		name = &normalized
	}

	// A nil slice is an omitted field; an empty one clears the tags.
	var tags []string
	if payload.Tags != nil {
		tags, err = normalizeTags(payload.Tags)
		if err != nil {
			return nil, err
		}
	}

	var agentID *uuid.UUID
	if payload.AgentID != nil {
		parsed, err := uuid.Parse(*payload.AgentID)
		if err != nil {
			return nil, oops.E(oops.CodeInvalid, err, "agent_id is not a valid uuid")
		}
		agentID = &parsed
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	// Read once to learn which issuer to lock. Tenancy-scoped, so another
	// organization's or a sibling project's admission is a not-found.
	located, err := q.GetWorkloadAdmission(ctx, repo.GetWorkloadAdmissionParams{
		OrganizationID: t.organizationID,
		ID:             id,
		ProjectID:      t.projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "admission not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading admission").LogError(ctx, s.logger)
	}

	// Every write under this issuer serializes here, so the fields this edit
	// leaves alone are merged from the row as it stands, and a concurrent
	// withdrawal cannot strip the assignment this edit repoints.
	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{
		OrganizationID: t.organizationID,
		ID:             located.WorkloadIssuerID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "admission not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error locking the workload issuer").LogError(ctx, s.logger)
	}

	// Re-read under the lock: the first read may predate a withdrawal or an edit
	// that committed while this transaction waited.
	existing, err := q.GetWorkloadAdmission(ctx, repo.GetWorkloadAdmissionParams{
		OrganizationID: t.organizationID,
		ID:             id,
		ProjectID:      t.projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "admission not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading admission").LogError(ctx, s.logger)
	}

	issuerRow, err := q.GetWorkloadIssuer(ctx, repo.GetWorkloadIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             existing.WorkloadIssuerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "the admission's issuer no longer exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading the admission's issuer").LogError(ctx, s.logger)
	}

	live, err := q.GetWorkloadAgentAssignmentForSubject(ctx, repo.GetWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: existing.WorkloadIssuerID,
		MatchKind:        existing.MatchKind,
		Subject:          existing.Subject,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading the workload's agent assignment").LogError(ctx, s.logger)
	}

	// Resolved exactly as admission resolves it: another organization's agent
	// is a not-found, not a rejected write.
	var agent *repo.GetOrganizationAgentRow
	if agentID != nil {
		row, err := q.GetOrganizationAgent(ctx, repo.GetOrganizationAgentParams{
			OrganizationID: t.organizationID,
			ID:             *agentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "agent not found")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent").LogError(ctx, s.logger)
		}
		agent = &row
	}

	agentBefore := ""
	if len(live) > 0 {
		agentBefore = live[0].AgentID.String()
	}
	agentAfter := agentBefore

	params := repo.UpdateWorkloadAdmissionParams{
		Name:           existing.Name,
		Tags:           conv.DefaultSlice(existing.Tags, []string{}),
		OrganizationID: t.organizationID,
		ID:             id,
		ProjectID:      t.projectID,
	}
	if name != nil {
		params.Name = conv.ToPGTextEmpty(*name)
	}
	if tags != nil {
		params.Tags = tags
	}

	updated, err := q.UpdateWorkloadAdmission(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating the admitted subject").LogError(ctx, s.logger)
	}

	if agent != nil {
		// Repointed in place, in the same transaction as the admission, so the
		// workload is never without an agent. The assignment is keyed on the
		// (issuer, match_kind, subject) tuple rather than on this admission, so an
		// admission of the same subject at the other tier follows it too.
		if _, err := q.UpsertWorkloadAgentAssignment(ctx, repo.UpsertWorkloadAgentAssignmentParams{
			OrganizationID:   t.organizationID,
			WorkloadIssuerID: existing.WorkloadIssuerID,
			Subject:          existing.Subject,
			MatchKind:        existing.MatchKind,
			AgentID:          agent.ID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error reassigning the workload's agent").LogError(ctx, s.logger)
		}
		agentAfter = agent.ID.String()
	}

	if err := s.audit.LogWorkloadAdmissionUpdate(ctx, dbtx, audit.LogWorkloadAdmissionUpdateEvent{
		OrganizationID:          t.organizationID,
		ProjectID:               updated.ProjectID,
		Actor:                   t.actor,
		ActorDisplayName:        t.actorEmail,
		ActorSlug:               nil,
		AdmissionURN:            urn.NewWorkloadAdmission(updated.ID),
		AdmissionDisplayName:    updated.Subject,
		AdmissionSnapshotBefore: AdmissionSnapshot(existing, issuerRow, agentBefore),
		AdmissionSnapshotAfter:  AdmissionSnapshot(updated, issuerRow, agentAfter),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload admission update").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

func (s *Service) WithdrawSubject(ctx context.Context, payload *gen.WithdrawSubjectPayload) (*gen.WorkloadIdentityPolicy, error) {
	t, err := s.resolve(ctx, authz.ScopeWorkloadWrite)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "id is not a valid uuid")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error starting transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	q := repo.New(dbtx)

	existing, err := q.GetWorkloadAdmission(ctx, repo.GetWorkloadAdmissionParams{
		OrganizationID: t.organizationID,
		ID:             id,
		ProjectID:      t.projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "admission not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading admission").LogError(ctx, s.logger)
	}

	issuerRow, err := q.GetWorkloadIssuer(ctx, repo.GetWorkloadIssuerParams{
		OrganizationID: t.organizationID,
		ProjectID:      t.projectID,
		ID:             existing.WorkloadIssuerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "the admission's issuer no longer exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading the admission's issuer").LogError(ctx, s.logger)
	}

	// Every write under this issuer serializes here, before any child row is read
	// or written: a concurrent withdrawal of the other tier, and a concurrent
	// admit that would otherwise insert a tier after this one counted none
	// remaining and then lose its agent to the delete below.
	if _, err := q.LockWorkloadIssuerForWrite(ctx, repo.LockWorkloadIssuerForWriteParams{
		OrganizationID: t.organizationID,
		ID:             existing.WorkloadIssuerID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error locking the workload issuer").LogError(ctx, s.logger)
	}

	withdrawn, err := q.SoftDeleteWorkloadAdmission(ctx, repo.SoftDeleteWorkloadAdmissionParams{
		OrganizationID: t.organizationID,
		ID:             id,
		ProjectID:      t.projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error withdrawing the admitted subject").LogError(ctx, s.logger)
	}

	// The assignment is keyed on (issuer, match_kind, subject) rather than on the
	// admission, because an admission is tiered and an assignment is not — so one
	// assignment serves both tiers. It may only go when the last admission naming
	// the tuple has gone: stripping it while the other tier is still admitted
	// would leave that admission with no policy, which the token endpoint refuses
	// for having no agent and which reads as a broken rule rather than a
	// withdrawn one.
	remaining, err := q.CountLiveAdmissionsForSubject(ctx, repo.CountLiveAdmissionsForSubjectParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: existing.WorkloadIssuerID,
		MatchKind:        existing.MatchKind,
		Subject:          existing.Subject,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error counting admissions for the withdrawn subject").LogError(ctx, s.logger)
	}

	// Read before the conditional delete, and unconditionally: the snapshot has to
	// record which agent this admission ran under whether or not the assignment
	// itself goes. Reading it only inside the delete branch left every
	// withdrawal-while-the-other-tier-survives with no agent in its audit record.
	live, err := q.GetWorkloadAgentAssignmentForSubject(ctx, repo.GetWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   t.organizationID,
		WorkloadIssuerID: existing.WorkloadIssuerID,
		MatchKind:        existing.MatchKind,
		Subject:          existing.Subject,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading the workload's agent assignment").LogError(ctx, s.logger)
	}

	assignedAgent := ""
	if len(live) > 0 {
		assignedAgent = live[0].AgentID.String()
	}

	if remaining == 0 {
		if _, err := q.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, repo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
			OrganizationID:   t.organizationID,
			WorkloadIssuerID: existing.WorkloadIssuerID,
			MatchKind:        existing.MatchKind,
			Subject:          existing.Subject,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error clearing the workload's agent assignment").LogError(ctx, s.logger)
		}
	}

	if err := s.audit.LogWorkloadAdmissionWithdraw(ctx, dbtx, audit.LogWorkloadAdmissionWithdrawEvent{
		OrganizationID:          t.organizationID,
		ProjectID:               withdrawn.ProjectID,
		Actor:                   t.actor,
		ActorDisplayName:        t.actorEmail,
		ActorSlug:               nil,
		AdmissionURN:            urn.NewWorkloadAdmission(withdrawn.ID),
		AdmissionDisplayName:    withdrawn.Subject,
		AdmissionSnapshotBefore: AdmissionSnapshot(withdrawn, issuerRow, assignedAgent),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording workload withdrawal").LogError(ctx, s.logger)
	}

	policy, err := s.loadPolicy(ctx, dbtx, t)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error committing transaction").LogError(ctx, s.logger)
	}

	return policy, nil
}

// isUniqueViolation reports a partial unique index rejecting a duplicate, which
// on these tables is always an operator re-entering something that already
// exists rather than a fault.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
