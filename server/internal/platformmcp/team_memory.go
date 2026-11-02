package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Source kinds name what a team memory item was read from.
const (
	TeamMemorySourceChat   = "chat"
	TeamMemorySourceMemo   = "memo"
	TeamMemorySourceDigest = "digest"
)

var (
	errTeamMemoryInvalid    = errors.New("invalid team memory request")
	errTeamMemoryNotEnabled = errors.New("team memory is not enabled")
)

// TeamMemorySource points at the record a team memory item came from, so an
// agent can cite it and a reader can open it.
type TeamMemorySource struct {
	// Kind is chat, memo or digest.
	Kind string `json:"kind"`

	ID string `json:"id"`

	// ChatID is the chat a memo was saved from, when it names one.
	ChatID string `json:"chat_id,omitempty"`
}

// Ref renders the source as kind:id, the form audit entries and citations use.
func (s TeamMemorySource) Ref() string {
	return s.Kind + ":" + s.ID
}

// TextRedactor masks detected secrets in text before team memory stores or
// returns it. changed reports that something was masked or withheld.
type TextRedactor interface {
	Text(ctx context.Context, text string) (string, bool, error)
}

// TeamMemoryService reads and writes a project's shared team memory: masked
// chat summaries, memos and digests. Reads need memory:read on the project
// and writes need memory:write. Neither grant implies transcript access.
type TeamMemoryService struct {
	db            *pgxpool.Pool
	engine        *authz.Engine
	audit         *audit.Logger
	flags         feature.Provider
	organizations OrganizationSlugResolver
	budget        OperationBudget
	redactor      TextRedactor
}

// NewTeamMemoryService composes the team memory tools. Missing dependencies
// leave stable unavailable tools registered on the server.
func NewTeamMemoryService(db *pgxpool.Pool, engine *authz.Engine, auditLogger *audit.Logger, flags feature.Provider, organizations OrganizationSlugResolver, budget OperationBudget, redactor TextRedactor) *TeamMemoryService {
	service := &TeamMemoryService{db: db, engine: engine, audit: auditLogger, flags: flags, organizations: organizations, budget: budget, redactor: redactor}
	if !service.valid() {
		return nil
	}
	return service
}

func (s *TeamMemoryService) valid() bool {
	return s != nil && s.db != nil && s.engine != nil && s.audit != nil && s.flags != nil && s.organizations != nil && s.budget.valid() && s.redactor != nil
}

// authorizeRead resolves the project and requires memory:read on it.
func (s *TeamMemoryService) authorizeRead(ctx context.Context, principal Principal, projectID, projectSlug string) (ResolvedProject, error) {
	return s.authorize(ctx, principal, projectID, projectSlug, authz.ScopeMemoryRead)
}

// authorizeWrite resolves the project and requires memory:write on it.
func (s *TeamMemoryService) authorizeWrite(ctx context.Context, principal Principal, projectID, projectSlug string) (ResolvedProject, error) {
	return s.authorize(ctx, principal, projectID, projectSlug, authz.ScopeMemoryWrite)
}

// authorize admits only an authenticated person on the external endpoint,
// requires scope on the exact project, checks the rollout flag, and then
// meters the call. Memory is project-scoped, so the grant check follows resolution;
// an unknown project and a missing grant both refuse without content.
func (s *TeamMemoryService) authorize(ctx context.Context, principal Principal, projectID, projectSlug string, scope authz.Scope) (ResolvedProject, error) {
	var zero ResolvedProject
	if !s.valid() {
		return zero, ErrUnavailable
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if principal.surface() != SurfacePlatformMCP || principal.UserID == "" || principal.OrganizationID == "" || !ok || authCtx == nil || authCtx.UserID != principal.UserID || authCtx.ActiveOrganizationID != principal.OrganizationID || authCtx.SupportOrganizationID != "" || authCtx.APIKeyID != "" {
		return zero, ErrForbidden
	}
	if (projectID == "") == (projectSlug == "") {
		return zero, errTeamMemoryInvalid
	}
	enforce, err := s.engine.ShouldEnforce(ctx)
	if err != nil || !enforce {
		return zero, ErrUnavailable
	}
	if _, ok := authz.GrantsFromContext(ctx); !ok {
		return zero, ErrUnavailable
	}
	// An unknown project refuses exactly like a missing grant, so the refusal
	// never tells a caller without the grant whether the project exists.
	project, err := (postgresRiskProjectResolver{queries: platformrepo.New(s.db)}).Resolve(ctx, principal.OrganizationID, projectID, projectSlug)
	if errors.Is(err, ErrRiskReadNotFound) {
		return zero, &ExternalAuthorizationError{RequiredScope: string(scope), RequestAccessURL: "", cause: err}
	}
	if err != nil {
		return zero, err
	}
	if err := s.engine.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: project.ID.String(), Dimensions: nil}); err != nil {
		return zero, &ExternalAuthorizationError{RequiredScope: string(scope), RequestAccessURL: "", cause: err}
	}
	enabled, err := s.enabled(ctx, principal.OrganizationID, project.Slug)
	if err != nil {
		return zero, err
	}
	if !enabled {
		return zero, errTeamMemoryNotEnabled
	}
	// Charged last, so a caller without the grant or the flag cannot drain the
	// allowance for people who have both.
	if err := s.budget.Allow(ctx, principal); err != nil {
		return zero, err
	}
	return project, nil
}

// enabled evaluates the team memory rollout flag. Disabled, missing and
// indeterminate read as off; an evaluation error is unavailable.
func (s *TeamMemoryService) enabled(ctx context.Context, organizationID, projectSlug string) (bool, error) {
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, organizationID)
	if err != nil || organizationSlug == "" {
		return false, fmt.Errorf("%w: organization slug is unavailable", ErrUnavailable)
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagTeamMemory, organizationID, feature.OrgProjectGroups(organizationSlug, projectSlug))
	if err != nil {
		return false, fmt.Errorf("%w: evaluate team memory flag: %w", ErrUnavailable, err)
	}
	return evaluation == feature.EvaluationEnabled, nil
}

// recordRead writes one audit entry for a memory read, naming every source
// the caller was shown.
func (s *TeamMemoryService) recordRead(ctx context.Context, principal Principal, project ResolvedProject, tool string, sources []TeamMemorySource) error {
	refs := make([]string, len(sources))
	for i, source := range sources {
		refs[i] = source.Ref()
	}
	if err := s.audit.LogTeamMemoryRead(ctx, s.db, audit.LogTeamMemoryReadEvent{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, ProjectSlug: project.Slug,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), ActorDisplayName: nil, ActorSlug: nil,
		Tool: tool, Sources: refs,
	}); err != nil {
		return fmt.Errorf("audit team memory read: %w", err)
	}
	return nil
}

func parseOptionalUUID(value string) (uuid.NullUUID, error) {
	if value == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.NullUUID{}, errTeamMemoryInvalid
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
