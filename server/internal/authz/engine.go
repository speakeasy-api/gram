package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
// platform admins can. Returns nil, false when overrides are absent or disallowed.
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

	// Admins impersonating a customer org have no WorkOS membership in that
	// org, so the normal role-resolution path would yield zero grants and
	// every Require() call would 403. Grant all scopes — matching the
	// carve-out in access.ListGrants.
	if authCtx.IsAdmin {
		if _, ok := contextvalues.GetAdminOverrideFromContext(ctx); ok {
			return GrantsToContext(ctx, allScopeGrants()), nil
		}
	}

	principals, err := ResolveUserPrincipals(ctx, e.db, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		if errors.Is(err, ErrPrincipalInvalid) {
			return ctx, oops.E(oops.CodeUnauthorized, err, "invalid user principal")
		}
		if errors.Is(err, ErrPrincipalNotFound) {
			return GrantsToContext(ctx, nil), nil
		}
		e.logger.ErrorContext(
			ctx,
			"failed to resolve principals for authz grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("resolve principals: %w", err)
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

	return e.EvaluateLoadedGrants(ctx, grants, checks...)
}

// EvaluateLoadedGrants evaluates explicit grants against checks without
// consulting ShouldEnforce or reading grants from context. Request handlers
// should use Require so normal request enforcement semantics apply.
func (e *Engine) EvaluateLoadedGrants(ctx context.Context, grants []Grant, checks ...Check) error {
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
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

		evaluation, err := evaluateGrantCheck(grants, check)
		if err != nil {
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
		if evaluation.Grant == nil {
			reason := authzrepo.ReasonScopeUnsatisfied
			switch {
			case evaluation.Denied:
				reason = authzrepo.ReasonDenyGrant
			case len(grants) == 0:
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
		matches = append(matches, grantMatch{Grant: *evaluation.Grant, ViaCheck: *evaluation.Check})
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

	anyDenied := false
	for _, check := range checks {
		evaluation, err := evaluateGrantCheck(grants, check)
		if err != nil {
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
		if evaluation.Denied {
			anyDenied = true
			continue
		}
		if evaluation.Grant != nil {
			challengeLogger{
				Operation:            authzrepo.OperationRequireAny,
				Outcome:              authzrepo.OutcomeAllow,
				Reason:               authzrepo.ReasonGrantMatched,
				Checks:               checks,
				Focus:                &check,
				Matches:              []grantMatch{{Grant: *evaluation.Grant, ViaCheck: *evaluation.Check}},
				EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
			return nil
		}
	}

	reason := authzrepo.ReasonScopeUnsatisfied
	switch {
	case anyDenied:
		reason = authzrepo.ReasonDenyGrant
	case len(grants) == 0:
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

// Allowed reports whether the caller satisfies every check, WITHOUT recording an
// authz challenge. Unlike Require, a negative result is not a denial to surface
// in the challenges/diagnostics UI — it is a routine branch used by handlers
// that gracefully degrade rather than reject. The canonical case is chat session
// listing: members legitimately hold no chat:read grant and simply see only
// their own sessions, so probing that grant with Require would stamp an expected
// "denial" on every members' page load and mislead admins into granting a scope
// that was never actually required.
//
// Returns true when RBAC is not enforced for the request (matching Require's
// short-circuit). A non-nil error is returned only for genuine evaluation
// failures (missing prepared grants, invalid check); a merely unsatisfied check
// yields (false, nil).
func (e *Engine) Allowed(ctx context.Context, checks ...Check) (bool, error) {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return false, err
	}
	if !enforce {
		return true, nil
	}
	if len(checks) == 0 {
		return false, e.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return false, e.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return false, e.mapError(ctx, err)
		}
		evaluation, err := evaluateGrantCheck(grants, check)
		if err != nil {
			return false, e.mapError(ctx, err)
		}
		if evaluation.Grant == nil {
			return false, nil
		}
	}

	return true, nil
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
	anyDenied := false
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

		evaluation, err := evaluateGrantCheck(grants, c)
		if err != nil {
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
		if evaluation.Denied {
			anyDenied = true
		}
		if evaluation.Grant != nil {
			allowed = append(allowed, c.ResourceID)
			matches = append(matches, grantMatch{Grant: *evaluation.Grant, ViaCheck: *evaluation.Check})
		}
	}

	if len(checks) > 0 {
		outcome := authzrepo.OutcomeDeny
		reason := authzrepo.ReasonScopeUnsatisfied
		switch {
		case len(allowed) > 0:
			outcome = authzrepo.OutcomeAllow
			reason = authzrepo.ReasonGrantMatched
		case anyDenied:
			reason = authzrepo.ReasonDenyGrant
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

// FindMatched evaluates each check and returns a parallel slice of match
// indicators aligned with the input order — matched[i] is true when checks[i]
// is authorized for the caller. It exists alongside [Filter] for cases where
// the caller needs per-check granularity that the ResourceID-keyed Filter
// return can't express — for example, filtering an MCP tools/list response
// where every check carries the same toolset/server ResourceID and per-tool
// granularity lives in the Tool dimension.
//
// When RBAC is not enforced every entry is true. An empty input returns an
// empty slice, no log. A single challenge-log entry is emitted for the batch
// (same as [Filter]); per-check logs are intentionally avoided so callers can
// safely use this with large input sets like a full tools/list.
func (e *Engine) FindMatched(ctx context.Context, checks []Check) ([]bool, error) {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return nil, err
	}

	if !enforce {
		out := make([]bool, len(checks))
		for i := range out {
			out[i] = true
		}
		return out, nil
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return nil, e.mapError(ctx, ErrMissingGrants)
	}

	matched := make([]bool, len(checks))
	matches := make([]grantMatch, 0, len(checks))
	allowedCount := 0
	anyDenied := false
	for i, c := range checks {
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

		evaluation, err := evaluateGrantCheck(grants, c)
		if err != nil {
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
		if evaluation.Denied {
			anyDenied = true
		}
		if evaluation.Grant != nil {
			matched[i] = true
			matches = append(matches, grantMatch{Grant: *evaluation.Grant, ViaCheck: *evaluation.Check})
			allowedCount++
		}
	}

	if len(checks) > 0 {
		outcome := authzrepo.OutcomeDeny
		reason := authzrepo.ReasonScopeUnsatisfied
		switch {
		case allowedCount > 0:
			outcome = authzrepo.OutcomeAllow
			reason = authzrepo.ReasonGrantMatched
		case anyDenied:
			reason = authzrepo.ReasonDenyGrant
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
			EvaluatedGrantCount:  uint32(len(grants)), //nolint:gosec // grant count is small
			FilterCandidateCount: uint32(len(checks)), //nolint:gosec // candidate count is small
			FilterAllowedCount:   uint32(allowedCount),
		}.Log(ctx, e.chDB, e.logger, e.challengeLoggingEnabled)
	}

	return matched, nil
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
		return false, oops.E(oops.CodeUnexpected, err, "check RBAC feature").LogError(ctx, e.logger)
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
		return oops.E(oops.CodeUnexpected, err, "authz grants missing from prepared context").LogError(ctx, e.logger)
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return oops.E(oops.CodeUnexpected, err, "invalid authz check").LogError(ctx, e.logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "check authz").LogError(ctx, e.logger)
	}
}
