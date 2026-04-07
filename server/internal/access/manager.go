package access

import (
	"context"
	"errors"
	"log/slog"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
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

func (m *Manager) LoadIntoContext(ctx context.Context) (context.Context, error) {
	return LoadIntoContext(ctx, m.logger, m.db)
}

func (m *Manager) Require(ctx context.Context, checks ...Check) error {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}

	if err := Require(ctx, checks...); err != nil {
		return m.mapError(ctx, err)
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

	if err := RequireAny(ctx, checks...); err != nil {
		return m.mapError(ctx, err)
	}

	return nil
}

func (m *Manager) Filter(ctx context.Context, scope Scope, resourceIDs []string) ([]string, error) {
	enforce, err := m.shouldEnforce(ctx)
	if err != nil {
		return nil, err
	}
	if !enforce {
		return resourceIDs, nil
	}

	resourceIDs, err = Filter(ctx, scope, resourceIDs)
	if err != nil {
		return nil, m.mapError(ctx, err)
	}

	return resourceIDs, nil
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
