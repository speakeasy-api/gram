package jsonwebkeysets

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gensvc "github.com/speakeasy-api/gram/server/gen/json_web_key_sets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/emaissuer"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

// EnterpriseIssuerManager provisions and maintains an organization's
// Speakeasy-managed ID-JAG issuer.
type EnterpriseIssuerManager interface {
	EnsureEMAIssuer(ctx context.Context, organizationID string) (uuid.UUID, error)
	DisableEMAIssuer(ctx context.Context, organizationID string) error
	RotateEMAIssuer(ctx context.Context, organizationID, activeKid string) error
	ReplaceEMAIssuerKey(ctx context.Context, organizationID, kid string) error
}

// SetEnterpriseIssuer installs issuer management before serving requests.
// origin must be the same pinned origin discovery and signing use.
func (s *Service) SetEnterpriseIssuer(manager EnterpriseIssuerManager, signer *emaissuer.Signer, origin *url.URL) {
	s.issuerManager = manager
	s.issuerSigner = signer
	s.issuerOrigin = origin
}

// issuerAccess checks RBAC and the SSO entitlement; the issuer does not need
// customer-managed keys.
func (s *Service) issuerAccess(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, false, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, false, err
	}
	entitled, err := s.productFeatures.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureSSO)
	if err != nil {
		return nil, logger, false, oops.E(oops.CodeUnexpected, err, "error checking enterprise authorization availability").LogError(ctx, logger)
	}
	return authCtx, logger, entitled && s.issuerManager != nil && s.issuerOrigin != nil, nil
}

func (s *Service) requireIssuerAdmin(ctx context.Context) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, logger, available, err := s.issuerAccess(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, logger, err
	}
	if !available {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "enterprise authorization is not available for this organization")
	}
	return authCtx, logger, nil
}

func (s *Service) GetOrganizationEnterpriseIssuer(ctx context.Context, _ *gensvc.GetOrganizationEnterpriseIssuerPayload) (*gensvc.OrganizationEnterpriseIssuer, error) {
	authCtx, logger, available, err := s.issuerAccess(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}
	return s.issuerView(ctx, logger, authCtx.ActiveOrganizationID, available, nil)
}

func (s *Service) EnableOrganizationEnterpriseIssuer(ctx context.Context, _ *gensvc.EnableOrganizationEnterpriseIssuerPayload) (*gensvc.OrganizationEnterpriseIssuer, error) {
	authCtx, logger, err := s.requireIssuerAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.allowMint(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	if _, err := s.issuerManager.EnsureEMAIssuer(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "could not enable enterprise authorization; retry shortly").LogError(ctx, logger)
	}
	return s.issuerView(ctx, logger, authCtx.ActiveOrganizationID, true, nil)
}

func (s *Service) DisableOrganizationEnterpriseIssuer(ctx context.Context, _ *gensvc.DisableOrganizationEnterpriseIssuerPayload) (*gensvc.OrganizationEnterpriseIssuer, error) {
	authCtx, logger, err := s.requireIssuerAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.issuerManager.DisableEMAIssuer(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, issuerError(ctx, logger, err, "could not disable enterprise authorization")
	}
	return s.issuerView(ctx, logger, authCtx.ActiveOrganizationID, true, nil)
}

func (s *Service) RotateOrganizationEnterpriseIssuer(ctx context.Context, payload *gensvc.RotateOrganizationEnterpriseIssuerPayload) (*gensvc.OrganizationEnterpriseIssuer, error) {
	authCtx, logger, err := s.requireIssuerAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.allowMint(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	var readyAt *time.Time
	if err := s.issuerManager.RotateEMAIssuer(ctx, authCtx.ActiveOrganizationID, payload.ActiveKid); err != nil {
		pending, ok := errors.AsType[*emaissuer.RotationPendingError](err)
		if !ok {
			return nil, issuerError(ctx, logger, err, "could not rotate the enterprise authorization key")
		}
		readyAt = &pending.ReadyAt
	}
	return s.issuerView(ctx, logger, authCtx.ActiveOrganizationID, true, readyAt)
}

func (s *Service) ReplaceOrganizationEnterpriseIssuerKey(ctx context.Context, payload *gensvc.ReplaceOrganizationEnterpriseIssuerKeyPayload) (*gensvc.OrganizationEnterpriseIssuer, error) {
	authCtx, logger, err := s.requireIssuerAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.allowMint(ctx, logger, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}
	if err := s.issuerManager.ReplaceEMAIssuerKey(ctx, authCtx.ActiveOrganizationID, payload.Kid); err != nil {
		return nil, issuerError(ctx, logger, err, "could not replace the enterprise authorization key")
	}
	return s.issuerView(ctx, logger, authCtx.ActiveOrganizationID, true, nil)
}

func issuerError(ctx context.Context, logger *slog.Logger, err error, msg string) error {
	switch {
	case errors.Is(err, emaissuer.ErrNotFound):
		return oops.E(oops.CodeNotFound, err, "enterprise authorization is not enabled")
	case errors.Is(err, emaissuer.ErrKeyNotFound):
		return oops.E(oops.CodeNotFound, err, "issuer key not found")
	case errors.Is(err, emaissuer.ErrUnknownKid):
		return oops.E(oops.CodeConflict, err, "the active key changed; read the issuer again")
	default:
		return oops.E(oops.CodeUnavailable, err, "%s; retry shortly", msg).LogError(ctx, logger)
	}
}

// issuerView is the public state of an organization's issuer: never signing material.
func (s *Service) issuerView(ctx context.Context, logger *slog.Logger, organizationID string, available bool, readyAt *time.Time) (*gensvc.OrganizationEnterpriseIssuer, error) {
	view := &gensvc.OrganizationEnterpriseIssuer{
		Available:       available,
		Enabled:         false,
		SigningEnabled:  false,
		Issuer:          nil,
		JwksURI:         nil,
		Keys:            []*gensvc.OrganizationEnterpriseIssuerKey{},
		RotationReadyAt: nil,
	}
	q := repo.New(s.db)
	set, err := q.GetOrganizationEMAIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return view, nil
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error loading enterprise authorization issuer").LogError(ctx, logger)
	}
	issuer, err := emaissuer.URL(s.issuerOrigin, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "enterprise authorization issuer origin is unavailable").LogError(ctx, logger)
	}
	keys, err := q.ListJsonWebKeys(ctx, repo.ListJsonWebKeysParams{JsonWebKeySetID: set.ID, OrganizationID: organizationID, IncludeRevoked: false})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading enterprise authorization keys").LogError(ctx, logger)
	}
	published, err := emaissuer.PublicKeys(ctx, s.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error loading enterprise authorization keys").LogError(ctx, logger)
	}
	live := make(map[string]bool, len(published.Keys))
	for _, key := range published.Keys {
		live[key.KeyID] = true
	}
	for _, key := range keys {
		if !live[key.Kid] {
			continue
		}
		view.Keys = append(view.Keys, &gensvc.OrganizationEnterpriseIssuerKey{Kid: key.Kid, State: key.State})
	}
	view.Enabled = true
	view.SigningEnabled = s.issuerSigner.Enabled()
	view.Issuer = new(issuer.String())
	view.JwksURI = new(issuer.JoinPath("jwks").String())
	if readyAt != nil {
		view.RotationReadyAt = new(readyAt.UTC().Format(time.RFC3339))
	}
	return view, nil
}
