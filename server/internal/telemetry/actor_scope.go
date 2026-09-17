package telemetry

import (
	"context"
	"slices"
	"strings"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// Observability reads are gated on logs:read in addition to the project or
// organization scope the endpoint already required. The scope exists to be
// narrowed: a grant carrying an actor_department, actor_group, or actor_role
// selector covers only the people that dimension describes, and the reads that
// can filter by actor return just their activity.
//
// Two rules keep that safe:
//
//   - A read that cannot filter by actor requires the scope unrestricted, so a
//     narrowed grant is refused rather than over-served. That is what
//     [unrestrictedLogsReadCheck] expresses.
//   - A caller holding no logs:read grant at all sees only their own activity,
//     the same graceful degradation the chat session list applies to members
//     without chat:read. Absence of the scope never means unrestricted.
//
// The actor dimensions describe current identity-provider state: they resolve
// through the directory profiles and role assignments Gram syncs, not through
// the identity snapshot stored on each event. Someone who changes department
// takes their history with them, which is the behaviour an administrator
// scoping access to "Engineering" expects.

// logsReadCheck is the check a caller must satisfy to read observability data
// for an organization. It constrains no actor dimension, so a narrowed grant
// satisfies it — the narrowing is applied to the rows, not the request.
func logsReadCheck(organizationID string) authz.Check {
	return authz.Check{
		Scope:        authz.ScopeLogsRead,
		ResourceKind: "",
		ResourceID:   organizationID,
		Dimensions:   nil,
	}
}

// unrestrictedLogsReadCheck only accepts a logs:read grant that names no actor
// dimension. Strict selector matching is what makes a narrowed grant fail it.
func unrestrictedLogsReadCheck(organizationID string) authz.Check {
	return logsReadCheck(organizationID).WithStrictSelectorMatch()
}

// requireProjectLogRead authorizes a project-scoped telemetry read that
// returns actor data it cannot filter: the caller needs project:read plus
// unrestricted log access for the organization.
func (s *Service) requireProjectLogRead(ctx context.Context, authCtx *contextvalues.AuthContext) error {
	if authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	return s.authz.Require(ctx,
		authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil},
		unrestrictedLogsReadCheck(authCtx.ActiveOrganizationID),
	)
}

// requireOrgLogRead is requireProjectLogRead for the organization-wide
// analytics endpoints.
func (s *Service) requireOrgLogRead(ctx context.Context, authCtx *contextvalues.AuthContext) error {
	if authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return s.authz.Require(ctx,
		authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil},
		unrestrictedLogsReadCheck(authCtx.ActiveOrganizationID),
	)
}

// resolveActorScope returns the actor restriction to apply to a read that can
// filter by actor. A nil scope means unrestricted.
func (s *Service) resolveActorScope(ctx context.Context, authCtx *contextvalues.AuthContext) (*repo.ActorScope, error) {
	if authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	constraints, err := s.authz.ScopeConstraints(ctx, logsReadCheck(authCtx.ActiveOrganizationID), authz.ActorSelectorKeys...)
	if err != nil {
		return nil, err
	}
	if constraints.Unrestricted {
		return nil, nil
	}

	// Everyone reads their own activity, so a caller whose grant covers nobody
	// else still gets a usable page instead of an empty one.
	scope := &repo.ActorScope{Emails: nil, UserIDs: nil}
	if authCtx.UserID != "" {
		scope.UserIDs = append(scope.UserIDs, authCtx.UserID)
	}
	if authCtx.Email != nil && *authCtx.Email != "" {
		scope.Emails = append(scope.Emails, normalizeActorEmail(*authCtx.Email))
	}

	if !constraints.Granted {
		return scope, nil
	}

	covered, err := s.resolveCoveredActors(ctx, authCtx.ActiveOrganizationID, constraints.Narrowed)
	if err != nil {
		return nil, err
	}
	for _, actor := range covered {
		if actor.Email != "" {
			scope.Emails = append(scope.Emails, actor.Email)
		}
		if actor.UserID != "" {
			scope.UserIDs = append(scope.UserIDs, actor.UserID)
		}
	}

	slices.Sort(scope.Emails)
	scope.Emails = slices.Compact(scope.Emails)
	slices.Sort(scope.UserIDs)
	scope.UserIDs = slices.Compact(scope.UserIDs)

	return scope, nil
}

type coveredActor struct {
	Email  string
	UserID string
}

// resolveCoveredActors expands the actor dimensions of a grant set into the
// organization members they describe. Dimensions inside one selector are ANDed
// — a grant naming both a department and a group covers the people in both —
// and the selectors are ORed.
func (s *Service) resolveCoveredActors(ctx context.Context, organizationID string, narrowed []authz.Selector) ([]coveredActor, error) {
	if len(narrowed) == 0 {
		return nil, nil
	}

	params := accessrepo.ListActorScopeIdentitiesParams{
		OrganizationID: organizationID,
		Departments:    []string{},
		GroupNames:     []string{},
		RoleSlugs:      []string{},
	}
	for _, selector := range narrowed {
		if value, ok := selector[authz.SelectorKeyActorDepartment]; ok {
			params.Departments = append(params.Departments, normalizeActorValue(value))
		}
		if value, ok := selector[authz.SelectorKeyActorGroup]; ok {
			params.GroupNames = append(params.GroupNames, normalizeActorValue(value))
		}
		if value, ok := selector[authz.SelectorKeyActorRole]; ok {
			params.RoleSlugs = append(params.RoleSlugs, normalizeActorValue(value))
		}
	}

	rows, err := accessrepo.New(s.db).ListActorScopeIdentities(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to resolve the members a log access grant covers")
	}

	covered := make([]coveredActor, 0, len(rows))
	for _, row := range rows {
		if !actorMatchesAny(row, narrowed) {
			continue
		}
		covered = append(covered, coveredActor{Email: row.Email, UserID: row.UserID})
	}

	return covered, nil
}

// actorMatchesAny reports whether a member satisfies every dimension of at
// least one selector. The query matched members on any dimension, which is the
// union the selectors need; this is the per-selector intersection.
func actorMatchesAny(row accessrepo.ListActorScopeIdentitiesRow, narrowed []authz.Selector) bool {
	for _, selector := range narrowed {
		if actorMatches(row, selector) {
			return true
		}
	}
	return false
}

func actorMatches(row accessrepo.ListActorScopeIdentitiesRow, selector authz.Selector) bool {
	if value, ok := selector[authz.SelectorKeyActorDepartment]; ok {
		if row.Department != normalizeActorValue(value) {
			return false
		}
	}
	if value, ok := selector[authz.SelectorKeyActorGroup]; ok {
		if !slices.Contains(row.GroupNames, normalizeActorValue(value)) {
			return false
		}
	}
	if value, ok := selector[authz.SelectorKeyActorRole]; ok {
		if !slices.Contains(row.RoleSlugs, normalizeActorValue(value)) {
			return false
		}
	}
	return true
}

// normalizeActorValue matches the case and whitespace folding the identity
// query applies, so provider-controlled values compare predictably.
func normalizeActorValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeActorEmail(email string) string {
	return normalizeActorValue(email)
}
