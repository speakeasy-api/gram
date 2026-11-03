package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

const (
	// TemporaryGrantMinDuration is the shortest window a temporary grant can
	// be given or renewed for, measured from the request.
	TemporaryGrantMinDuration = 5 * time.Minute
	// TemporaryGrantMaxDuration is the longest window a temporary grant can be
	// given or renewed for, measured from the request. Longer access is a
	// role assignment, not a temporary grant.
	TemporaryGrantMaxDuration = 30 * 24 * time.Hour

	temporaryGrantStatusActive  = "active"
	temporaryGrantStatusExpired = "expired"
	temporaryGrantStatusRevoked = "revoked"
)

// temporaryGrantScopes are the scopes that can be given temporarily: the
// read, use and manage permissions an onboarding or investigation needs.
// org:admin is left out: it authorizes role, member and audience changes, so a
// temporary holder could turn it into permanent access before it expires.
// Blocklist scopes only withdraw access. mcp:auth is provisioned through auth
// client audiences and its backfill. Risk policy, plugin, inference, agent
// credential and machine scopes are left out because other subsystems derive
// standing configuration from who holds them, which an expiring row would
// leave stale.
var temporaryGrantScopes = []authz.Scope{
	authz.ScopeOrgRead,
	authz.ScopeProjectRead,
	authz.ScopeProjectWrite,
	authz.ScopeMCPRead,
	authz.ScopeMCPWrite,
	authz.ScopeMCPConnect,
	authz.ScopeEnvironmentRead,
	authz.ScopeEnvironmentWrite,
	authz.ScopeSkillRead,
	authz.ScopeSkillWrite,
	authz.ScopeAssistantRead,
	authz.ScopeAssistantWrite,
	authz.ScopeChatRead,
	authz.ScopeChatWrite,
	authz.ScopeLogRead,
	authz.ScopeAgentRead,
	authz.ScopeWorkloadRead,
}

// temporaryGrantRow is the shape every temporary grant query returns. The
// per-query row types sqlc generates are identical and convert to it.
type temporaryGrantRow = repo.ListTemporaryGrantsRow

// ValidateTemporaryGrantScope reports whether scope can be given temporarily.
func ValidateTemporaryGrantScope(scope authz.Scope) error {
	if !slices.Contains(temporaryGrantScopes, scope) {
		return fmt.Errorf("scope %q cannot be given temporarily", scope)
	}
	return nil
}

// validateTemporaryGrantDeadline parses expiresAt and checks it is within the
// allowed window from now.
func validateTemporaryGrantDeadline(expiresAt string, now time.Time) (time.Time, error) {
	deadline, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return time.Time{}, errors.New("expires_at must be an RFC 3339 timestamp")
	}
	if deadline.Before(now.Add(TemporaryGrantMinDuration)) {
		return time.Time{}, fmt.Errorf("expires_at must be at least %s from now", TemporaryGrantMinDuration)
	}
	if deadline.After(now.Add(TemporaryGrantMaxDuration)) {
		return time.Time{}, errors.New("expires_at must be at most 30 days from now; assign a role for longer access")
	}
	return deadline, nil
}

func (s *Service) requireTemporaryGrantAdmin(ctx context.Context, organizationID string) error {
	return s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: organizationID, Dimensions: nil})
}

func (s *Service) CreateTemporaryGrant(ctx context.Context, payload *gen.CreateTemporaryGrantPayload) (*gen.TemporaryGrant, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.requireTemporaryGrantAdmin(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attr.OrganizationID(ac.ActiveOrganizationID),
		attr.UserID(ac.UserID),
		attr.AccessMemberID(payload.UserID),
	)
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID), attr.SlogAccessMemberID(payload.UserID))

	scope := authz.Scope(payload.Scope)
	if err := ValidateTemporaryGrantScope(scope); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, logger)
	}
	selector := authz.NewSelector(scope, authz.WildcardResource)
	if payload.Selector != nil {
		selector = genSelectorToAuthz(payload.Selector)
	}
	if err := authz.ValidateSelector(scope, selector); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid selector: %s", err.Error()).LogError(ctx, logger)
	}
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal selector").LogError(ctx, logger)
	}
	deadline, err := validateTemporaryGrantDeadline(payload.ExpiresAt, time.Now())
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, logger)
	}

	recipient, err := connectedUser(ctx, s.db, ac.ActiveOrganizationID, payload.UserID)
	switch {
	case errors.Is(err, errConnectedUserNotFound):
		return nil, oops.E(oops.CodeNotFound, nil, "member not found").LogError(ctx, logger)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get member").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	recipientPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, recipient.ID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	inserted, err := repo.New(dbtx).InsertTemporaryGrant(ctx, repo.InsertTemporaryGrantParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   recipientPrincipal,
		Scope:          string(scope),
		Selectors:      selectorJSON,
		ExpiresAt:      pgtype.Timestamptz{Time: deadline, InfinityModifier: pgtype.Finite, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, s.temporaryGrantConflict(ctx, dbtx, repo.GetPrincipalGrantByIdentityParams{
			OrganizationID: ac.ActiveOrganizationID,
			PrincipalUrn:   recipientPrincipal,
			Scope:          string(scope),
			Selectors:      selectorJSON,
		})
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create temporary grant").LogError(ctx, logger)
	}
	row := temporaryGrantRow(inserted)

	otherAccess, err := newTemporaryGrantExplainer(dbtx).otherAccess(ctx, row)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "explain other access").LogError(ctx, logger)
	}
	result, err := temporaryGrantView(row, temporaryGrantStatus(row), otherAccess)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, logger)
	}

	if err := s.audit.LogAccessTemporaryGrantCreate(ctx, dbtx, audit.LogAccessTemporaryGrantCreateEvent{
		OrganizationID:              ac.ActiveOrganizationID,
		Actor:                       actor,
		ActorDisplayName:            ac.Email,
		ActorSlug:                   nil,
		TemporaryGrantURN:           urn.NewTemporaryGrant(row.ID),
		RecipientEmail:              recipient.Email,
		Reason:                      strings.TrimSpace(conv.PtrValOr(payload.Reason, "")),
		TemporaryGrantSnapshotAfter: result,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log temporary grant creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit temporary grant").LogError(ctx, logger)
	}

	return result, nil
}

// temporaryGrantConflict explains why a temporary grant could not be written:
// the member already holds the same grant, permanently or temporarily.
func (s *Service) temporaryGrantConflict(ctx context.Context, dbtx database.DBTX, identity repo.GetPrincipalGrantByIdentityParams) error {
	existing, err := repo.New(dbtx).GetPrincipalGrantByIdentity(ctx, identity)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get conflicting grant").LogError(ctx, s.logger)
	}
	if !existing.ExpiresAt.Valid {
		return oops.E(oops.CodeConflict, nil, "the member already holds this grant permanently")
	}
	return oops.E(oops.CodeConflict, nil, "the member already holds this grant temporarily until %s; renew it instead", existing.ExpiresAt.Time.UTC().Format(time.RFC3339))
}

func (s *Service) ListTemporaryGrants(ctx context.Context, payload *gen.ListTemporaryGrantsPayload) (*gen.ListTemporaryGrantsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))

	isAdmin, err := s.authz.Evaluate(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check organization admin").LogError(ctx, s.logger)
	}

	userID := conv.PtrValOr(payload.UserID, "")
	if !isAdmin {
		// A member sees the boundary and remaining time of their own grants
		// only.
		if userID != "" && userID != ac.UserID {
			return nil, oops.C(oops.CodeForbidden)
		}
		userID = ac.UserID
	}
	principalURN := ""
	if userID != "" {
		principalURN = urn.NewPrincipal(urn.PrincipalTypeUser, userID).String()
	}

	rows, err := repo.New(s.db).ListTemporaryGrants(ctx, repo.ListTemporaryGrantsParams{
		OrganizationID: ac.ActiveOrganizationID,
		PrincipalUrn:   principalURN,
		IncludeExpired: payload.IncludeExpired,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list temporary grants").LogError(ctx, s.logger)
	}

	grants := make([]*gen.TemporaryGrant, 0, len(rows))
	explainer := newTemporaryGrantExplainer(s.db)
	for _, row := range rows {
		otherAccess, err := explainer.otherAccess(ctx, row)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "explain other access").LogError(ctx, s.logger)
		}
		view, err := temporaryGrantView(row, temporaryGrantStatus(row), otherAccess)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, s.logger)
		}
		grants = append(grants, view)
	}

	return &gen.ListTemporaryGrantsResult{Grants: grants}, nil
}

func (s *Service) RenewTemporaryGrant(ctx context.Context, payload *gen.RenewTemporaryGrantPayload) (*gen.TemporaryGrant, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.requireTemporaryGrantAdmin(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid temporary grant id").LogError(ctx, logger)
	}
	deadline, err := validateTemporaryGrantDeadline(payload.ExpiresAt, time.Now())
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	before, err := lockActiveTemporaryGrant(ctx, dbtx, ac.ActiveOrganizationID, id)
	if err != nil {
		return nil, err
	}

	renewed, err := repo.New(dbtx).RenewTemporaryGrant(ctx, repo.RenewTemporaryGrantParams{
		ExpiresAt:      pgtype.Timestamptz{Time: deadline, InfinityModifier: pgtype.Finite, Valid: true},
		ID:             id,
		OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The deadline passed between the lock and the update.
		return nil, temporaryGrantExpiredConflict()
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "renew temporary grant").LogError(ctx, logger)
	}
	after := temporaryGrantRow(renewed)

	// Renewal changes only the deadline, so the other routes to the same
	// access are the same before and after.
	otherAccess, err := newTemporaryGrantExplainer(dbtx).otherAccess(ctx, after)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "explain other access").LogError(ctx, logger)
	}
	beforeView, err := temporaryGrantView(before, temporaryGrantStatusActive, otherAccess)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, logger)
	}
	afterView, err := temporaryGrantView(after, temporaryGrantStatus(after), otherAccess)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, logger)
	}

	recipientEmail, err := temporaryGrantRecipientEmail(ctx, dbtx, after.PrincipalUrn)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get temporary grant recipient").LogError(ctx, logger)
	}
	if err := s.audit.LogAccessTemporaryGrantRenew(ctx, dbtx, audit.LogAccessTemporaryGrantRenewEvent{
		OrganizationID:               ac.ActiveOrganizationID,
		Actor:                        urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:             ac.Email,
		ActorSlug:                    nil,
		TemporaryGrantURN:            urn.NewTemporaryGrant(after.ID),
		RecipientEmail:               recipientEmail,
		TemporaryGrantSnapshotBefore: beforeView,
		TemporaryGrantSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log temporary grant renewal").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit temporary grant renewal").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) RevokeTemporaryGrant(ctx context.Context, payload *gen.RevokeTemporaryGrantPayload) (*gen.TemporaryGrant, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid temporary grant id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID)
	before, err := lockActiveTemporaryGrant(ctx, dbtx, ac.ActiveOrganizationID, id)
	if err != nil {
		// A member who is not an administrator must not learn whether a grant
		// they do not hold exists, so they get the authorization error.
		if adminErr := s.requireTemporaryGrantAdmin(ctx, ac.ActiveOrganizationID); adminErr != nil {
			return nil, adminErr
		}
		return nil, err
	}
	// The recipient may give up their own access early; ending anyone else's
	// takes organization admin.
	if before.PrincipalUrn != actor {
		if err := s.requireTemporaryGrantAdmin(ctx, ac.ActiveOrganizationID); err != nil {
			return nil, err
		}
	}

	// Recorded while the grant is still active, so the snapshot shows which
	// other access the member keeps once it ends.
	otherAccess, err := newTemporaryGrantExplainer(dbtx).otherAccess(ctx, before)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "explain other access").LogError(ctx, logger)
	}

	revoked, err := repo.New(dbtx).RevokeTemporaryGrant(ctx, repo.RevokeTemporaryGrantParams{
		ID:             id,
		OrganizationID: ac.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The deadline passed between the lock and the delete.
		return nil, temporaryGrantExpiredConflict()
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "revoke temporary grant").LogError(ctx, logger)
	}
	after := temporaryGrantRow(revoked)

	beforeView, err := temporaryGrantView(before, temporaryGrantStatusActive, otherAccess)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, logger)
	}
	afterView, err := temporaryGrantView(after, temporaryGrantStatusRevoked, nil)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render temporary grant").LogError(ctx, logger)
	}

	recipientEmail, err := temporaryGrantRecipientEmail(ctx, dbtx, after.PrincipalUrn)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get temporary grant recipient").LogError(ctx, logger)
	}
	if err := s.audit.LogAccessTemporaryGrantRevoke(ctx, dbtx, audit.LogAccessTemporaryGrantRevokeEvent{
		OrganizationID:               ac.ActiveOrganizationID,
		Actor:                        actor,
		ActorDisplayName:             ac.Email,
		ActorSlug:                    nil,
		TemporaryGrantURN:            urn.NewTemporaryGrant(after.ID),
		RecipientEmail:               recipientEmail,
		TemporaryGrantSnapshotBefore: beforeView,
		TemporaryGrantSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log temporary grant revocation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit temporary grant revocation").LogError(ctx, logger)
	}

	return afterView, nil
}

// lockActiveTemporaryGrant locks one temporary grant in the caller's
// organization and returns it while it is still active. A permanent grant is
// not found. An expired grant is a conflict: it can no longer be renewed or
// revoked, and giving it again restarts it.
func lockActiveTemporaryGrant(ctx context.Context, dbtx database.DBTX, organizationID string, id uuid.UUID) (temporaryGrantRow, error) {
	locked, err := repo.New(dbtx).GetTemporaryGrantForUpdate(ctx, repo.GetTemporaryGrantForUpdateParams{ID: id, OrganizationID: organizationID})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return temporaryGrantRow{}, oops.E(oops.CodeNotFound, nil, "temporary grant not found")
	case err != nil:
		return temporaryGrantRow{}, oops.E(oops.CodeUnexpected, err, "get temporary grant")
	}
	row := temporaryGrantRow(locked)
	if temporaryGrantStatus(row) != temporaryGrantStatusActive {
		return temporaryGrantRow{}, temporaryGrantExpiredConflict()
	}
	return row, nil
}

func temporaryGrantExpiredConflict() error {
	return oops.E(oops.CodeConflict, nil, "temporary grant has expired; give it again instead")
}

func temporaryGrantRecipientEmail(ctx context.Context, dbtx database.DBTX, principal urn.Principal) (string, error) {
	user, err := usersrepo.New(dbtx).GetUser(ctx, principal.ID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("get recipient: %w", err)
	}
	return user.Email, nil
}

func temporaryGrantStatus(row temporaryGrantRow) string {
	if !row.ExpiresAt.Time.After(row.ObservedAt.Time) {
		return temporaryGrantStatusExpired
	}
	return temporaryGrantStatusActive
}

func temporaryGrantView(row temporaryGrantRow, status string, otherAccess []string) (*gen.TemporaryGrant, error) {
	selector, err := authz.SelectorFromRow(row.Selectors)
	if err != nil {
		return nil, fmt.Errorf("unmarshal temporary grant selector: %w", err)
	}

	var remaining int64
	if status == temporaryGrantStatusActive {
		remaining = int64(row.ExpiresAt.Time.Sub(row.ObservedAt.Time) / time.Second)
	} else {
		otherAccess = nil
	}
	if otherAccess == nil {
		otherAccess = []string{}
	}

	return &gen.TemporaryGrant{
		ID:               row.ID.String(),
		UserID:           row.PrincipalUrn.ID,
		PrincipalUrn:     row.PrincipalUrn.String(),
		Scope:            row.Scope,
		Selector:         authzSelectorToGen(selector),
		ExpiresAt:        row.ExpiresAt.Time.UTC().Format(time.RFC3339),
		Status:           status,
		RemainingSeconds: remaining,
		OtherAccess:      otherAccess,
		CreatedAt:        row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

// temporaryGrantExplainer answers, for each active temporary grant, which of
// the member's other grants already give the same access, so ending the grant
// is explainable: access through those routes remains. Member grants are
// loaded once per member.
type temporaryGrantExplainer struct {
	db     database.DBTX
	grants map[urn.Principal][]authz.Grant
}

func newTemporaryGrantExplainer(db database.DBTX) *temporaryGrantExplainer {
	return &temporaryGrantExplainer{db: db, grants: make(map[urn.Principal][]authz.Grant)}
}

func (e *temporaryGrantExplainer) otherAccess(ctx context.Context, row temporaryGrantRow) ([]string, error) {
	if temporaryGrantStatus(row) != temporaryGrantStatusActive {
		return nil, nil
	}
	selector, err := authz.SelectorFromRow(row.Selectors)
	if err != nil {
		return nil, fmt.Errorf("unmarshal temporary grant selector: %w", err)
	}

	grants, ok := e.grants[row.PrincipalUrn]
	if !ok {
		principals, err := authz.ResolveUserPrincipals(ctx, e.db, row.OrganizationID, row.PrincipalUrn.ID)
		switch {
		case errors.Is(err, authz.ErrPrincipalNotFound):
			// The member left the organization: nothing gives them access.
			e.grants[row.PrincipalUrn] = nil
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("resolve member principals: %w", err)
		}
		grants, err = authz.LoadGrants(ctx, e.db, row.OrganizationID, principals)
		if err != nil {
			return nil, fmt.Errorf("load member grants: %w", err)
		}
		e.grants[row.PrincipalUrn] = grants
	}

	return otherAccessFor(grants, row.PrincipalUrn.String(), authz.Scope(row.Scope), selector), nil
}

// otherAccessFor returns the principals whose grants, other than the one
// temporary grant described by (holder, scope, selector), contain that
// grant's boundary. grants is the member's full loaded set, which holds the
// temporary grant itself exactly once (one row per target), and that copy is
// left out. Blocks are not evaluated: this names the routes that give the
// same access, and the access explanation decides a specific request.
func otherAccessFor(grants []authz.Grant, holder string, scope authz.Scope, selector authz.Selector) []string {
	byPrincipal := make(map[string][]authz.Grant)
	skipped := false
	for _, grant := range grants {
		if !skipped && grant.PrincipalUrn == holder && grant.Scope == scope && maps.Equal(grant.Selector, selector) {
			skipped = true
			continue
		}
		byPrincipal[grant.PrincipalUrn] = append(byPrincipal[grant.PrincipalUrn], grant)
	}

	var principals []string
	for principal, held := range byPrincipal {
		if authz.GrantsContainSelector(held, scope, selector) {
			principals = append(principals, principal)
		}
	}
	slices.Sort(principals)
	return principals
}
