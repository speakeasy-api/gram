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

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
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
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
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
) *Service {
	logger = logger.With(attr.SlogComponent("workloadpolicy.api"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/workloadpolicy"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
		repo:   repo.New(db),
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
// the trust policy has an organization tier whose rows carry no project.
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

	projectID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if authCtx.ProjectID != nil {
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
func (t tenancy) requestedTier(projectScoped *bool) (uuid.NullUUID, error) {
	if !conv.PtrValOr(projectScoped, false) {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	if !t.projectID.Valid {
		return uuid.NullUUID{}, oops.E(oops.CodeInvalid, nil, "project_scoped requires a project to be selected")
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
	if _, err := issuerurl.ParseHTTPSOnly(payload.JwksURI); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "jwks_uri must be an https URL")
	}
	if err := requireTrustDomain(payload.Issuer, "issuer"); err != nil {
		return nil, err
	}
	if err := requireTrustDomain(payload.JwksURI, "jwks_uri"); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "name must not be blank")
	}

	allowWildcard := conv.PtrValOr(payload.AllowWildcardAdmission, false)

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
		OrganizationID:   t.organizationID,
		ProjectID:        projectID,
		Actor:            t.actor,
		ActorDisplayName: t.actorEmail,
		ActorSlug:        nil,
		IssuerURN:        urn.NewWorkloadIssuer(row.ID),
		IssuerName:       row.Name,
		IssuerSnapshotAfter: &audit.WorkloadIssuerSnapshot{
			Name:                   row.Name,
			Issuer:                 row.Issuer,
			JwksURI:                row.JwksUri,
			AllowWildcardAdmission: row.AllowWildcardAdmission,
			Tier:                   tier(projectID),
		},
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
			OrganizationID:       t.organizationID,
			ProjectID:            admission.ProjectID,
			Actor:                t.actor,
			ActorDisplayName:     t.actorEmail,
			ActorSlug:            nil,
			AdmissionURN:         urn.NewWorkloadAdmission(admission.ID),
			AdmissionDisplayName: admission.Subject,
			AdmissionSnapshotBefore: &audit.WorkloadAdmissionSnapshot{
				Issuer:          existing.Issuer,
				IssuerName:      existing.Name,
				Subject:         admission.Subject,
				MatchKind:       admission.MatchKind,
				AssignedAgentID: assignedAgent,
				Tier:            tier(admission.ProjectID),
			},
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
		OrganizationID:   t.organizationID,
		ProjectID:        deleted.ProjectID,
		Actor:            t.actor,
		ActorDisplayName: t.actorEmail,
		ActorSlug:        nil,
		IssuerURN:        urn.NewWorkloadIssuer(deleted.ID),
		IssuerName:       deleted.Name,
		IssuerSnapshotBefore: &audit.WorkloadIssuerSnapshot{
			Name:                   deleted.Name,
			Issuer:                 deleted.Issuer,
			JwksURI:                deleted.JwksUri,
			AllowWildcardAdmission: deleted.AllowWildcardAdmission,
			Tier:                   tier(deleted.ProjectID),
		},
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

	matchKind, err := workloadidentity.ParseMatchKind(conv.PtrValOr(payload.MatchKind, ""))
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

	admission, err := q.CreateWorkloadAdmission(ctx, repo.CreateWorkloadAdmissionParams{
		OrganizationID:   t.organizationID,
		ProjectID:        projectID,
		WorkloadIssuerID: issuerRow.ID,
		Subject:          payload.Subject,
		MatchKind:        string(matchKind),
		Name:             conv.PtrToPGTextEmpty(payload.Name),
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
		OrganizationID:       t.organizationID,
		ProjectID:            projectID,
		Actor:                t.actor,
		ActorDisplayName:     t.actorEmail,
		ActorSlug:            nil,
		AdmissionURN:         urn.NewWorkloadAdmission(admission.ID),
		AdmissionDisplayName: admission.Subject,
		AdmissionSnapshotAfter: &audit.WorkloadAdmissionSnapshot{
			Issuer:          issuerRow.Issuer,
			IssuerName:      issuerRow.Name,
			Subject:         admission.Subject,
			MatchKind:       admission.MatchKind,
			AssignedAgentID: agent.ID.String(),
			Tier:            tier(projectID),
		},
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

	assignedAgent := ""
	if remaining == 0 {
		assignments, err := q.SoftDeleteWorkloadAgentAssignmentForSubject(ctx, repo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
			OrganizationID:   t.organizationID,
			WorkloadIssuerID: existing.WorkloadIssuerID,
			MatchKind:        existing.MatchKind,
			Subject:          existing.Subject,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error clearing the workload's agent assignment").LogError(ctx, s.logger)
		}
		if len(assignments) > 0 {
			assignedAgent = assignments[0].AgentID.String()
		}
	}

	if err := s.audit.LogWorkloadAdmissionWithdraw(ctx, dbtx, audit.LogWorkloadAdmissionWithdrawEvent{
		OrganizationID:       t.organizationID,
		ProjectID:            withdrawn.ProjectID,
		Actor:                t.actor,
		ActorDisplayName:     t.actorEmail,
		ActorSlug:            nil,
		AdmissionURN:         urn.NewWorkloadAdmission(withdrawn.ID),
		AdmissionDisplayName: withdrawn.Subject,
		AdmissionSnapshotBefore: &audit.WorkloadAdmissionSnapshot{
			Issuer:          issuerRow.Issuer,
			IssuerName:      issuerRow.Name,
			Subject:         withdrawn.Subject,
			MatchKind:       withdrawn.MatchKind,
			AssignedAgentID: assignedAgent,
			Tier:            tier(withdrawn.ProjectID),
		},
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
