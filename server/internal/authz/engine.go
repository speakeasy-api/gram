package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

type IsRBACEnabled func(ctx context.Context, organizationID string) (bool, error)

// MembershipFetcher retrieves a WorkOS membership for a user+org pair.
type MembershipFetcher interface {
	GetOrgMembership(ctx context.Context, workOSUserID, workOSOrgID string) (*workos.Member, error)
}

type EngineOpts struct {
	DevMode bool
}

// ChallengeLoggingEnabled checks whether authz challenge logging to ClickHouse
// is enabled for a given organization. Same signature as IsRBACEnabled.
type ChallengeLoggingEnabled func(ctx context.Context, organizationID string) (bool, error)

type Engine struct {
	logger                  *slog.Logger
	db                      *pgxpool.Pool
	chDB                    clickhouse.Conn
	isEnabled               IsRBACEnabled
	challengeLoggingEnabled ChallengeLoggingEnabled
	isDev                   bool
	membership              MembershipFetcher
}

func NewEngine(logger *slog.Logger, db *pgxpool.Pool, chDB clickhouse.Conn, isEnabled IsRBACEnabled, challengeLogging ChallengeLoggingEnabled, membership MembershipFetcher, opts ...EngineOpts) *Engine {
	var devMode bool
	if len(opts) > 0 {
		devMode = opts[0].DevMode
	}

	authzLogger := logger.With(attr.SlogComponent("authz"))

	return &Engine{
		logger:                  authzLogger,
		db:                      db,
		chDB:                    chDB,
		isEnabled:               isEnabled,
		challengeLoggingEnabled: challengeLogging,
		isDev:                   devMode,
		membership:              membership,
	}
}

// GetScopeOverrides returns the parsed scope overrides from the request context
// if they are present AND the caller is authorised to use them. In local dev
// any authenticated user may use the override header; in production only
// superadmins can. Returns nil, false when overrides are absent or disallowed.
func (e *Engine) GetScopeOverrides(ctx context.Context) ([]RoleGrant, bool) {
	overrides, ok := readScopeOverrides(ctx)
	if !ok {
		return nil, false
	}
	if e.isDev {
		return overrides, true
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !authCtx.IsAdmin {
		return nil, false
	}

	return overrides, true
}

func (e *Engine) PrepareContext(ctx context.Context) (context.Context, error) {
	if _, ok := GrantsFromContext(ctx); ok {
		return ctx, nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ctx, nil
	}

	// Assistant-token auth has no session but should resolve grants against
	// the owning user stamped as UserID on the context.
	_, isAssistant := contextvalues.GetAssistantPrincipal(ctx)
	if authCtx.SessionID == nil && !isAssistant {
		return ctx, nil
	}

	if overrides, ok := e.GetScopeOverrides(ctx); ok {
		return GrantsToContext(ctx, GrantsFromOverrides(overrides)), nil
	}

	if authCtx.AccountType != "enterprise" {
		return ctx, nil
	}

	enabled, err := e.isEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		e.logger.WarnContext(ctx, "failed to check RBAC feature flag, skipping grant loading",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogError(err),
		)
		return ctx, nil
	}
	if !enabled {
		return ctx, nil
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)}

	roleSlug, err := e.resolveRoleSlug(ctx, authCtx.UserID, authCtx.ActiveOrganizationID)
	if err != nil {
		e.logger.ErrorContext(
			ctx,
			"failed to resolve role for authz grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("resolve role slug: %w", err)
	}
	if roleSlug != "" {
		principals = append(principals, urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug))
	}

	grants, err := LoadGrants(ctx, e.db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		e.logger.ErrorContext(
			ctx,
			"failed to load authz grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("load authz grants: %w", err)
	}

	return GrantsToContext(ctx, grants), nil
}

func (e *Engine) resolveRoleSlug(ctx context.Context, userID, orgID string) (string, error) {
	user, err := usersrepo.New(e.db).GetUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get user: %w", err)
	}
	if !user.WorkosID.Valid || user.WorkosID.String == "" {
		return "", nil
	}

	roleSlugs, err := accessrepo.New(e.db).ListMemberRoleSlugsByWorkosUser(ctx, accessrepo.ListMemberRoleSlugsByWorkosUserParams{
		OrganizationID: orgID,
		WorkosUserID:   user.WorkosID.String,
	})
	if err != nil {
		return "", fmt.Errorf("list member role slugs: %w", err)
	}
	if len(roleSlugs) == 0 {
		return "", nil
	}

	return roleSlugs[0], nil
}

// InvalidateRoleCache is retained for callers that used to clear the Redis role cache.
// Role resolution now reads Postgres directly, so this is intentionally a no-op.
func (e *Engine) InvalidateRoleCache(ctx context.Context, userID, orgID string) {
}

// InvalidateAllRoleCaches is retained for callers that used to clear the Redis role cache.
// Role resolution now reads Postgres directly, so this is intentionally a no-op.
func (e *Engine) InvalidateAllRoleCaches(ctx context.Context, orgID string) {
}

func (e *Engine) Require(ctx context.Context, checks ...Check) error {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return e.mapError(ctx, ErrMissingGrants)
	}

	matches := make([]grantMatch, 0, len(checks))
	for _, check := range checks {
		if err := validateInput(check); err != nil {
			challengeLogger{
				Operation:            authzrepo.OperationRequire,
				Outcome:              authzrepo.OutcomeError,
				Reason:               authzrepo.ReasonInvalidCheck,
				Checks:               checks,
				Focus:                &check,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return e.mapError(ctx, err)
		}

		expanded := check.expand()

		matchedGrant, matchedCheck := findMatchingGrant(grants, expanded)
		if matchedGrant == nil {
			reason := authzrepo.ReasonScopeUnsatisfied
			if len(grants) == 0 {
				reason = authzrepo.ReasonNoGrants
			}
			challengeLogger{
				Operation:            authzrepo.OperationRequire,
				Outcome:              authzrepo.OutcomeDeny,
				Reason:               reason,
				Checks:               checks,
				Focus:                &check,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return e.mapError(ctx, Denied(check.Scope, check.selector()))
		}
		matches = append(matches, grantMatch{Grant: *matchedGrant, ViaCheck: *matchedCheck})
	}

	challengeLogger{
		Operation:            authzrepo.OperationRequire,
		Outcome:              authzrepo.OutcomeAllow,
		Reason:               authzrepo.ReasonGrantMatched,
		Checks:               checks,
		Focus:                &checks[0],
		Matches:              matches,
		EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
	return nil
}

func (e *Engine) RequireAny(ctx context.Context, checks ...Check) error {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return e.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			challengeLogger{
				Operation:            authzrepo.OperationRequireAny,
				Outcome:              authzrepo.OutcomeError,
				Reason:               authzrepo.ReasonInvalidCheck,
				Checks:               checks,
				Focus:                &check,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return e.mapError(ctx, err)
		}
	}

	for _, check := range checks {
		if matchedGrant, matchedCheck := findMatchingGrant(grants, check.expand()); matchedGrant != nil {
			challengeLogger{
				Operation:            authzrepo.OperationRequireAny,
				Outcome:              authzrepo.OutcomeAllow,
				Reason:               authzrepo.ReasonGrantMatched,
				Checks:               checks,
				Focus:                &check,
				Matches:              []grantMatch{{Grant: *matchedGrant, ViaCheck: *matchedCheck}},
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return nil
		}
	}

	reason := authzrepo.ReasonScopeUnsatisfied
	if len(grants) == 0 {
		reason = authzrepo.ReasonNoGrants
	}
	challengeLogger{
		Operation:            authzrepo.OperationRequireAny,
		Outcome:              authzrepo.OutcomeDeny,
		Reason:               reason,
		Checks:               checks,
		Focus:                &checks[0],
		Matches:              nil,
		EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
		FilterCandidateCount: 0,
		FilterAllowedCount:   0,
	}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
	return e.mapError(ctx, Denied(checks[0].Scope, checks[0].selector()))
}

// Filter evaluates each check and returns the resource IDs of those the caller
// is authorized for. When RBAC is not enforced all resource IDs are returned.
func (e *Engine) Filter(ctx context.Context, checks []Check) ([]string, error) {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}

	if !enforce {
		ids := make([]string, len(checks))
		for i, c := range checks {
			ids[i] = c.ResourceID
		}
		return ids, nil
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}

	allowed := make([]string, 0, len(checks))
	matches := make([]grantMatch, 0, len(checks))
	for _, c := range checks {
		if err := validateInput(c); err != nil {
			focus := c
			challengeLogger{
				Operation:            authzrepo.OperationFilter,
				Outcome:              authzrepo.OutcomeError,
				Reason:               authzrepo.ReasonInvalidCheck,
				Checks:               checks,
				Focus:                &focus,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: uint32(len(checks)), //nolint:gosec // candidate count is small
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return nil, e.mapError(ctx, err)
		}

		if matchedGrant, matchedCheck := findMatchingGrant(grants, c.expand()); matchedGrant != nil {
			allowed = append(allowed, c.ResourceID)
			matches = append(matches, grantMatch{Grant: *matchedGrant, ViaCheck: *matchedCheck})
		}
	}

	if len(checks) > 0 {
		outcome := authzrepo.OutcomeDeny
		reason := authzrepo.ReasonScopeUnsatisfied
		switch {
		case len(allowed) > 0:
			outcome = authzrepo.OutcomeAllow
			reason = authzrepo.ReasonGrantMatched
		case len(grants) == 0:
			reason = authzrepo.ReasonNoGrants
		}
		challengeLogger{
			Operation:            authzrepo.OperationFilter,
			Outcome:              outcome,
			Reason:               reason,
			Checks:               checks,
			Focus:                nil,
			Matches:              matches,
			EvaluatedGrantCount:  uint32(len(grants)),  //nolint:gosec // grant count is small
			FilterCandidateCount: uint32(len(checks)),  //nolint:gosec // candidate count is small
			FilterAllowedCount:   uint32(len(allowed)), //nolint:gosec // allowed count is small
		}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
	}

	return allowed, nil
}

func (e *Engine) ShouldEnforce(ctx context.Context) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return false, oops.C(oops.CodeUnauthorized)
	}

	// Never enforce RBAC on API key requests — they have their own scoping.
	if authCtx.APIKeyID != "" {
		return false, nil
	}

	// When the caller has active scope overrides, enforce so the override scopes
	// take effect regardless of account type or feature flag. Checked after
	// API key exclusion so the toolbar doesn't interfere with API key auth flows.
	if _, ok := e.GetScopeOverrides(ctx); ok {
		return true, nil
	}

	if authCtx.AccountType != "enterprise" {
		return false, nil
	}

	_, isAssistant := contextvalues.GetAssistantPrincipal(ctx)
	if authCtx.SessionID == nil && !isAssistant {
		return false, nil
	}

	enabled, err := e.isEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check RBAC feature").Log(ctx, e.logger)
	}

	return enabled, nil
}

func validateInput(c Check) error {
	switch c.ResourceID {
	case "":
		return InvalidCheck(c.Scope, c.ResourceID)
	case WildcardResource:
		return InvalidCheck(c.Scope, c.ResourceID)
	default:
		return nil
	}
}

func (e *Engine) mapError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrDenied):
		return oops.C(oops.CodeForbidden)
	case errors.Is(err, ErrMissingGrants):
		return oops.E(oops.CodeUnexpected, err, "authz grants missing from prepared context").Log(ctx, e.logger)
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return oops.E(oops.CodeUnexpected, err, "invalid authz check").Log(ctx, e.logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "check authz").Log(ctx, e.logger)
	}
}
