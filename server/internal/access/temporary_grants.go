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
	"github.com/speakeasy-api/gram/server/internal/agents"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"

	chrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

const (
	// TemporaryGrantMinDuration is the shortest window a temporary grant can
	// be given or renewed for, measured from the request.
	TemporaryGrantMinDuration = 5 * time.Minute
	// TemporaryGrantMaxDuration is the longest window a temporary grant can be
	// given or renewed for, measured from the request. Longer access is a
	// role assignment, not a temporary grant.
	TemporaryGrantMaxDuration = 24 * time.Hour

	temporaryGrantStatusActive  = "active"
	temporaryGrantStatusExpired = "expired"
	temporaryGrantStatusRevoked = "revoked"

	// challengeResolutionTemporaryGrant marks a denied challenge answered by a
	// temporary grant given with createTemporaryGrant.
	challengeResolutionTemporaryGrant = "temporary_grant"
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
		return time.Time{}, errors.New("expires_at must be at most 24 hours from now; assign a role for longer access")
	}
	return deadline, nil
}

// temporaryGrantRecipient parses and checks the principal a temporary grant is
// given to: an active organization member, everyone (user:all), an active role,
// or an agent that may be given new access. API keys, workloads and system
// principals cannot hold one.
func (s *Service) temporaryGrantRecipient(ctx context.Context, organizationID string, raw string) (urn.Principal, error) {
	principal, err := urn.ParsePrincipal(raw)
	if err != nil {
		return urn.Principal{}, oops.E(oops.CodeBadRequest, err, "invalid principal urn").LogError(ctx, s.logger)
	}
	err = authz.ValidatePrincipal(ctx, s.db, organizationID, principal)
	switch {
	case errors.Is(err, authz.ErrPrincipalInvalid):
		return urn.Principal{}, oops.E(oops.CodeBadRequest, err, "principal %q cannot hold a temporary grant", principal.String()).LogError(ctx, s.logger)
	case errors.Is(err, authz.ErrPrincipalNotFound):
		return urn.Principal{}, oops.E(oops.CodeNotFound, err, "principal not found").LogError(ctx, s.logger)
	case err != nil:
		return urn.Principal{}, oops.E(oops.CodeUnexpected, err, "validate principal").LogError(ctx, s.logger)
	}

	if principal.Type == urn.PrincipalTypeAgent {
		// A suspended or revoked agent keeps the access it has but cannot be
		// given more, the same rule audiences follow.
		assignable, err := s.assignableAgentPrincipals(ctx, organizationID)
		if err != nil {
			return urn.Principal{}, err
		}
		if _, ok := assignable[principal.String()]; !ok {
			return urn.Principal{}, oops.E(oops.CodeBadRequest, nil, "agent %q is suspended or revoked and cannot be given access", principal.String()).LogError(ctx, s.logger)
		}
	}

	return principal, nil
}

// temporaryGrantActor is the principal a temporary grant change is attributed
// to: the authenticated agent when one is acting, otherwise the signed-in
// user. The audit logger attributes the entry the same way, so self_granted
// and the self-revoke check compare the recipient with that same actor.
func temporaryGrantActor(ctx context.Context, userID string) urn.Principal {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		return actor
	}
	return urn.NewPrincipal(urn.PrincipalTypeUser, userID)
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
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID))

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

	recipientPrincipal, err := s.temporaryGrantRecipient(ctx, ac.ActiveOrganizationID, payload.PrincipalUrn)
	if err != nil {
		return nil, err
	}

	// ClickHouse holds the denial facts, so the challenges are read and
	// checked before the transaction opens.
	answered, err := s.challengesAnsweredByTemporaryGrant(ctx, ac.ActiveOrganizationID, payload.ChallengeIds, recipientPrincipal, scope, selector)
	if err != nil {
		return nil, err
	}

	actor := temporaryGrantActor(ctx, ac.UserID)

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

	recipientEmail, err := temporaryGrantRecipientEmail(ctx, dbtx, recipientPrincipal)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get recipient").LogError(ctx, logger)
	}

	if err := s.audit.LogAccessTemporaryGrantCreate(ctx, dbtx, audit.LogAccessTemporaryGrantCreateEvent{
		OrganizationID:              ac.ActiveOrganizationID,
		Actor:                       actor,
		ActorDisplayName:            ac.Email,
		ActorSlug:                   nil,
		AccessGrantURN:              urn.NewAccessGrant(row.ID),
		RecipientEmail:              recipientEmail,
		Reason:                      strings.TrimSpace(conv.PtrValOr(payload.Reason, "")),
		TemporaryGrantSnapshotAfter: result,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log temporary grant creation").LogError(ctx, logger)
	}

	if err := s.resolveChallengesWithTemporaryGrant(ctx, dbtx, ac.ActiveOrganizationID, ac.UserID, ac.Email, answered, row.ID); err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit temporary grant").LogError(ctx, logger)
	}

	return result, nil
}

// temporaryGrantConflict explains why a temporary grant could not be written:
// the principal already holds the same grant, permanently or temporarily.
func (s *Service) temporaryGrantConflict(ctx context.Context, dbtx database.DBTX, identity repo.GetPrincipalGrantByIdentityParams) error {
	existing, err := repo.New(dbtx).GetPrincipalGrantByIdentity(ctx, identity)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get conflicting grant").LogError(ctx, s.logger)
	}
	if !existing.ExpiresAt.Valid {
		return oops.E(oops.CodeConflict, nil, "the principal already holds this grant permanently")
	}
	return oops.E(oops.CodeConflict, nil, "the principal already holds this grant temporarily until %s; renew it instead", existing.ExpiresAt.Time.UTC().Format(time.RFC3339))
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

	principal, err := urn.ParsePrincipal(payload.PrincipalUrn)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid principal urn").LogError(ctx, s.logger)
	}
	// A member who is not an administrator sees the boundary and remaining
	// time of grants given directly to them only.
	if !isAdmin && principal.String() != urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID).String() {
		return nil, oops.C(oops.CodeForbidden)
	}
	principalURN := principal.String()

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
	// The holder must still be able to receive access: a member who left, a
	// deleted role or a suspended agent cannot have its grant extended.
	if _, err := s.temporaryGrantRecipient(ctx, ac.ActiveOrganizationID, before.PrincipalUrn.String()); err != nil {
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
		Actor:                        temporaryGrantActor(ctx, ac.UserID),
		ActorDisplayName:             ac.Email,
		ActorSlug:                    nil,
		AccessGrantURN:               urn.NewAccessGrant(after.ID),
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

	actor := temporaryGrantActor(ctx, ac.UserID)
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
	// other access the holder keeps once it ends.
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
		AccessGrantURN:               urn.NewAccessGrant(after.ID),
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

// temporaryGrantRecipientEmail names a member recipient in the audit log. Other
// principals have no email; the audit entry names them by URN instead.
func temporaryGrantRecipientEmail(ctx context.Context, dbtx database.DBTX, principal urn.Principal) (string, error) {
	if principal.Type != urn.PrincipalTypeUser || principal.ID == urn.AllUsersPrincipalID {
		return "", nil
	}
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
// the holder's other grants already give the same access, so ending the grant
// is explainable: access through those routes remains. Grants are loaded once
// per holder.
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
		principals, err := temporaryGrantHolderPrincipals(ctx, e.db, row.OrganizationID, row.PrincipalUrn)
		switch {
		case errors.Is(err, authz.ErrPrincipalNotFound), errors.Is(err, agents.ErrPrincipalNotFound):
			// The holder is gone from the organization: nothing gives it access.
			e.grants[row.PrincipalUrn] = nil
			return nil, nil
		case err != nil:
			return nil, fmt.Errorf("resolve holder principals: %w", err)
		}
		grants, err = authz.LoadGrants(ctx, e.db, row.OrganizationID, principals)
		if err != nil {
			return nil, fmt.Errorf("load holder grants: %w", err)
		}
		e.grants[row.PrincipalUrn] = grants
	}

	return otherAccessFor(grants, row.PrincipalUrn.String(), authz.Scope(row.Scope), selector), nil
}

// temporaryGrantHolderPrincipals returns the principals whose grants reach the
// holder of a temporary grant, the same set a request made as the holder is
// evaluated against. A member or agent inherits from its roles and from
// everyone (user:all); a role or everyone holds only its own grants.
func temporaryGrantHolderPrincipals(ctx context.Context, db database.DBTX, organizationID string, holder urn.Principal) ([]urn.Principal, error) {
	switch {
	case holder.Type == urn.PrincipalTypeUser && holder.ID != urn.AllUsersPrincipalID:
		principals, err := authz.ResolveUserPrincipals(ctx, db, organizationID, holder.ID)
		if err != nil {
			return nil, fmt.Errorf("resolve user principals: %w", err)
		}
		return principals, nil
	case holder.Type == urn.PrincipalTypeAgent:
		principals, err := authz.ResolveAgentPrincipals(ctx, db, organizationID, holder)
		if err != nil {
			return nil, fmt.Errorf("resolve agent principals: %w", err)
		}
		return principals, nil
	default:
		return []urn.Principal{holder}, nil
	}
}

// otherAccessFor returns the principals whose grants, other than the one
// temporary grant described by (holder, scope, selector), contain that
// grant's boundary. grants is the holder's full loaded set, which holds the
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

// challengesAnsweredByTemporaryGrant reads the denied challenges a new temporary
// grant is meant to answer and checks each one is a deny of the grant's scope,
// raised by the grant's recipient, on a resource the grant's selector covers.
// It returns nil when no challenge IDs were given.
func (s *Service) challengesAnsweredByTemporaryGrant(ctx context.Context, organizationID string, rawIDs []string, recipient urn.Principal, scope authz.Scope, selector authz.Selector) ([]chrepo.ChallengeSummary, error) {
	if len(rawIDs) == 0 {
		return nil, nil
	}
	challengeIDs, err := uniqueChallengeIDs(rawIDs)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "challenge_ids must contain distinct, non-empty IDs").LogError(ctx, s.logger)
	}

	challenges, err := chrepo.New(s.chConn).ListChallengesByIDs(ctx, organizationID, challengeIDs)
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "denied challenges could not be verified yet").LogError(ctx, s.logger)
	}
	if err := validateChallengesForTemporaryGrant(challenges, challengeIDs, recipient, scope, selector); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogError(ctx, s.logger)
	}
	return challenges, nil
}

func validateChallengesForTemporaryGrant(challenges []chrepo.ChallengeSummary, challengeIDs []string, recipient urn.Principal, scope authz.Scope, selector authz.Selector) error {
	if len(challenges) != len(challengeIDs) {
		return errors.New("one or more challenges are no longer available")
	}
	seen := make(map[string]struct{}, len(challenges))
	for _, challenge := range challenges {
		if _, ok := seen[challenge.ID]; ok {
			return errors.New("duplicate challenge data")
		}
		seen[challenge.ID] = struct{}{}

		if challenge.Outcome != string(chrepo.OutcomeDeny) {
			return fmt.Errorf("challenge %s was not denied", challenge.ID)
		}
		if challenge.PrincipalURN != recipient.String() {
			return fmt.Errorf("challenge %s was raised by another principal", challenge.ID)
		}
		if challenge.Scope != string(scope) {
			return fmt.Errorf("challenge %s is for scope %q, not %q", challenge.ID, challenge.Scope, scope)
		}
		if challenge.Selector == "" {
			return fmt.Errorf("challenge %s is too old to be answered by a grant; retry the denied action", challenge.ID)
		}
		checkSelector, err := challengeSelector(challenge)
		if err != nil {
			return fmt.Errorf("challenge %s: %w", challenge.ID, err)
		}
		// The same strict check the role-assignment resolution uses: the grant
		// alone must authorize exactly the check that was denied.
		dimensions := make(map[string]string, len(checkSelector))
		for key, value := range checkSelector {
			if key != authz.SelectorKeyResourceKind && key != authz.SelectorKeyResourceID {
				dimensions[key] = value
			}
		}
		check := authz.Check{Scope: scope, ResourceKind: challenge.ResourceKind, ResourceID: challenge.ResourceID, Dimensions: dimensions}
		allowed, err := authz.GrantsAuthorize([]authz.Grant{authz.NewGrantWithSelector(scope, selector)}, check.WithStrictSelectorMatch())
		if err != nil {
			return fmt.Errorf("challenge %s cannot be evaluated against the grant: %w", challenge.ID, err)
		}
		if !allowed {
			return fmt.Errorf("the grant's selector does not cover the resource of challenge %s", challenge.ID)
		}
	}
	return nil
}

// resolveChallengesWithTemporaryGrant records each answered challenge as
// resolved by the temporary grant, inside the grant's own transaction, so a
// grant and its resolutions are written together or not at all.
func (s *Service) resolveChallengesWithTemporaryGrant(ctx context.Context, dbtx database.DBTX, organizationID string, userID string, actorEmail *string, challenges []chrepo.ChallengeSummary, grantID uuid.UUID) error {
	if len(challenges) == 0 {
		return nil
	}
	queries := repo.New(dbtx)
	if err := queries.LockChallengeResolutions(ctx, organizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock challenge resolutions").LogError(ctx, s.logger)
	}

	ids := make([]string, len(challenges))
	for i, challenge := range challenges {
		ids[i] = challenge.ID
	}
	existing, err := queries.ListChallengeResolutions(ctx, repo.ListChallengeResolutionsParams{
		OrganizationID: organizationID,
		ChallengeIds:   ids,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list challenge resolutions").LogError(ctx, s.logger)
	}
	if len(existing) > 0 {
		return oops.E(oops.CodeConflict, nil, "some challenges were already resolved; refresh and try again").LogError(ctx, s.logger)
	}

	// One insert per resource: a grant covering every chat can answer denials
	// on several chats, and each resolution keeps its own challenge's resource.
	type resourceKey struct{ kind, id string }
	groups := make(map[resourceKey][]chrepo.ChallengeSummary)
	order := make([]resourceKey, 0, len(challenges))
	for _, challenge := range challenges {
		key := resourceKey{kind: challenge.ResourceKind, id: challenge.ResourceID}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], challenge)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, userID)
	for _, key := range order {
		group := groups[key]
		groupIDs := make([]string, len(group))
		for i, challenge := range group {
			groupIDs[i] = challenge.ID
		}
		rows, err := queries.InsertChallengeResolutions(ctx, repo.InsertChallengeResolutionsParams{
			OrganizationID:   organizationID,
			ChallengeIds:     groupIDs,
			PrincipalUrn:     group[0].PrincipalURN,
			Scope:            group[0].Scope,
			ResourceKind:     key.kind,
			ResourceID:       key.id,
			ResolutionType:   challengeResolutionTemporaryGrant,
			RoleSlug:         pgtype.Text{String: "", Valid: false},
			TemporaryGrantID: uuid.NullUUID{UUID: grantID, Valid: true},
			ResolvedBy:       actor.String(),
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "insert challenge resolutions").LogError(ctx, s.logger)
		}
		if len(rows) != len(groupIDs) {
			return oops.E(oops.CodeConflict, nil, "one or more challenges were resolved concurrently").LogError(ctx, s.logger)
		}
		for _, row := range rows {
			if err := s.audit.LogAccessChallengeResolve(ctx, dbtx, audit.LogAccessChallengeResolveEvent{
				OrganizationID:   organizationID,
				Actor:            actor,
				ActorDisplayName: actorEmail,
				ChallengeID:      row.ChallengeID,
				PrincipalURN:     row.PrincipalUrn,
				Scope:            row.Scope,
				ResolutionType:   row.ResolutionType,
				RoleSlug:         nil,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "log access challenge resolve").LogError(ctx, s.logger)
			}
		}
	}
	return nil
}
