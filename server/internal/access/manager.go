package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type FeatureChecker interface {
	IsFeatureEnabled(ctx context.Context, organizationID string, feature productfeatures.Feature) (bool, error)
}

type Manager struct {
	logger   *slog.Logger
	db       accessrepo.DBTX
	features FeatureChecker
}

func NewManager(logger *slog.Logger, db accessrepo.DBTX, features FeatureChecker) *Manager {
	return &Manager{
		logger:   logger.With(attr.SlogComponent("access")),
		db:       db,
		features: features,
	}
}

func (m *Manager) PrepareContext(ctx context.Context) (context.Context, error) {
	if grants, ok := GrantsFromContext(ctx); ok && grants != nil {
		return ctx, nil
	}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return ctx, nil
	}

	if authCtx.AccountType != "enterprise" {
		return ctx, nil
	}

	principals := []urn.Principal{urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)}
	// TODO: once we have role mapping we need to also add grants for roles here.

	grants, err := LoadGrants(ctx, m.db, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		m.logger.ErrorContext(
			ctx,
			"failed to load access grants",
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogUserID(authCtx.UserID),
			attr.SlogError(err),
		)
		return ctx, fmt.Errorf("load access grants: %w", err)
	}

	return GrantsToContext(ctx, grants), nil
}

func (m *Manager) Require(ctx context.Context, checks ...Check) error {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return m.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return m.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return m.mapError(ctx, err)
		}

		if !grants.satisfies(check.Expand()) {
			return m.mapError(ctx, Denied(check.Scope, check.ResourceID))
		}
	}

	return nil
}

func (m *Manager) RequireAny(ctx context.Context, checks ...Check) error {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return m.mapError(ctx, ErrNoChecks)
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return m.mapError(ctx, ErrMissingGrants)
	}

	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return m.mapError(ctx, err)
		}
	}

	if slices.ContainsFunc(checks, func(c Check) bool { return grants.satisfies(c.Expand()) }) {
		return nil
	}

	return m.mapError(ctx, Denied(checks[0].Scope, checks[0].ResourceID))
}

func (m *Manager) Filter(ctx context.Context, scope Scope, resourceIDs []string) ([]string, error) {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		return resourceIDs, nil
	}

	grants, ok := GrantsFromContext(ctx)
	if !ok || grants == nil {
		return nil, m.mapError(ctx, ErrMissingGrants)
	}

	allowed := make([]string, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if err := validateInput(Check{Scope: scope, ResourceID: resourceID}); err != nil {
			return nil, m.mapError(ctx, err)
		}

		if grants.satisfies(Check{Scope: scope, ResourceID: resourceID}.Expand()) {
			allowed = append(allowed, resourceID)
		}
	}

	return allowed, nil
}

func (m *Manager) shouldEnforce(ctx context.Context) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return false, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.AccountType != "enterprise" || authCtx.APIKeyID != "" || authCtx.SessionID == nil {
		return false, nil
	}

	enabled, err := m.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureRBAC)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check RBAC feature").Log(ctx, m.logger)
	}

	return enabled, nil
}

func (m *Manager) mapError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrDenied):
		return oops.C(oops.CodeForbidden)
	case errors.Is(err, ErrMissingGrants):
		return oops.E(oops.CodeUnexpected, err, "access grants missing from prepared context").Log(ctx, m.logger)
	case errors.Is(err, ErrInvalidCheck), errors.Is(err, ErrNoChecks):
		return oops.E(oops.CodeUnexpected, err, "invalid access check").Log(ctx, m.logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "check access").Log(ctx, m.logger)
	}
}
