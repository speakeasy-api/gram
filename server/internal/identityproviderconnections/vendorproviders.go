package identityproviderconnections

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/identity_provider_connections"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// authorizeVendorProviders checks scope like authorize, without requiring the
// Okta provisioner: the order also governs the Speakeasy issuer.
func (s *Service) authorizeVendorProviders(ctx context.Context, scope authz.Scope, mutation bool) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))
	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, logger, err
	}
	if mutation {
		if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "identity provider order cannot be changed with an API key").LogError(ctx, logger)
		}
		if contextvalues.IsSupportSession(ctx) {
			return nil, logger, oops.E(oops.CodeForbidden, nil, "identity provider order cannot be changed from a support session").LogError(ctx, logger)
		}
	}
	return authCtx, logger, nil
}

func (s *Service) GetVendorIdentityProviders(ctx context.Context, payload *gen.GetVendorIdentityProvidersPayload) (*gen.VendorIdentityProvidersResult, error) {
	authCtx, logger, err := s.authorizeVendorProviders(ctx, authz.ScopeOrgRead, false)
	if err != nil {
		return nil, err
	}
	vendor, err := remotesessions.NormalizeVendorIssuer(payload.VendorIssuer)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "vendor_issuer must be an absolute http(s) URL with no query or fragment")
	}
	return s.vendorProvidersView(ctx, logger, repo.New(s.db), authCtx.ActiveOrganizationID, vendor)
}

func (s *Service) SetVendorIdentityProviders(ctx context.Context, payload *gen.SetVendorIdentityProvidersPayload) (*gen.VendorIdentityProvidersResult, error) {
	authCtx, logger, err := s.authorizeVendorProviders(ctx, authz.ScopeOrgAdmin, true)
	if err != nil {
		return nil, err
	}
	org := authCtx.ActiveOrganizationID
	vendor, err := remotesessions.NormalizeVendorIssuer(payload.VendorIssuer)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "vendor_issuer must be an absolute http(s) URL with no query or fragment")
	}
	providers, err := parseVendorProviders(payload.Providers)
	if err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)
	if err := q.LockOrganizationVendorIdentityProviders(ctx, org); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock identity provider order").LogError(ctx, logger)
	}
	for i, p := range providers {
		if p.kind == remotesessions.EMAProviderSpeakeasy {
			issuer, err := q.GetLiveInternalSessionIssuer(ctx, org)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return nil, oops.E(oops.CodeBadRequest, nil, "the organization has no Speakeasy issuer; set one up before adding it")
			case err != nil:
				return nil, oops.E(oops.CodeUnexpected, err, "read Speakeasy issuer").LogError(ctx, logger)
			}
			providers[i].issuer = issuer.ID
			continue
		}
		supplier, err := q.IsExternalIDJAGSupplier(ctx, repo.IsExternalIDJAGSupplierParams{OrganizationID: conv.ToPGText(org), ID: p.remoteIssuer})
		switch {
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read remote session issuer").LogError(ctx, logger)
		case !supplier:
			return nil, oops.E(oops.CodeBadRequest, nil, "remote_session_issuer_id must name an organization-level issuer that an organization user session issuer trusts")
		}
	}
	beforeRows, err := q.ListVendorIdentityProviders(ctx, repo.ListVendorIdentityProvidersParams{OrganizationID: org, VendorIssuer: vendor})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read identity provider order").LogError(ctx, logger)
	}
	before := vendorProvidersSnapshot(vendor, beforeRows)
	after := &audit.OrganizationEMAVendorIdentityProvidersSnapshot{VendorIssuer: vendor, Providers: make([]audit.OrganizationEMAVendorIdentityProvider, 0, len(providers))}
	for _, p := range providers {
		after.Providers = append(after.Providers, auditVendorProvider(nullID(p.remoteIssuer), nullID(p.issuer)))
	}
	if slices.Equal(before.Providers, after.Providers) {
		return s.vendorProvidersView(ctx, logger, q, org, vendor)
	}

	if _, err := q.DeleteVendorIdentityProviders(ctx, repo.DeleteVendorIdentityProvidersParams{OrganizationID: org, VendorIssuer: vendor}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear identity provider order").LogError(ctx, logger)
	}
	for i, p := range providers {
		if err := q.InsertVendorIdentityProvider(ctx, repo.InsertVendorIdentityProviderParams{
			OrganizationID:          org,
			VendorIssuer:            vendor,
			RemoteSessionIssuerID:   nullID(p.remoteIssuer),
			InternalSessionIssuerID: nullID(p.issuer),
			Position:                int32(i),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save identity provider order").LogError(ctx, logger)
		}
	}

	meta, err := orgrepo.New(dbtx).GetOrganizationMetadata(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization").LogError(ctx, logger)
	}
	if err := s.audit.LogOrganizationEMAVendorIdentityProvidersUpdated(ctx, dbtx, audit.LogOrganizationEMAVendorIdentityProvidersUpdatedEvent{
		OrganizationID:             org,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:           authCtx.Email,
		OrganizationName:           meta.Name,
		OrganizationSlug:           meta.Slug,
		OrganizationSnapshotBefore: before,
		OrganizationSnapshotAfter:  after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log identity provider order").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit identity provider order").LogError(ctx, logger)
	}
	return s.vendorProvidersView(ctx, logger, repo.New(s.db), org, vendor)
}

type vendorProvider struct {
	kind remotesessions.EMAProviderKind
	// remoteIssuer is the external supplier's remote session issuer.
	remoteIssuer uuid.UUID
	// issuer is the organization's Speakeasy issuer, resolved for speakeasy.
	issuer uuid.UUID
}

// parseVendorProviders validates an order: known kinds, a remote issuer
// exactly when external, and each supplier at most once.
func parseVendorProviders(in []*gen.VendorIdentityProviderInput) ([]vendorProvider, error) {
	out := make([]vendorProvider, 0, len(in))
	for _, p := range in {
		if p == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "providers must not contain null entries")
		}
		kind, ok := remotesessions.ParseEMAProviderKind(p.Kind)
		if !ok {
			return nil, oops.E(oops.CodeBadRequest, nil, "provider kind must be external or speakeasy")
		}
		remoteIssuer := uuid.Nil
		switch {
		case kind == remotesessions.EMAProviderExternal && p.RemoteSessionIssuerID == nil:
			return nil, oops.E(oops.CodeBadRequest, nil, "external suppliers need remote_session_issuer_id")
		case kind == remotesessions.EMAProviderSpeakeasy && p.RemoteSessionIssuerID != nil:
			return nil, oops.E(oops.CodeBadRequest, nil, "the speakeasy supplier takes no remote_session_issuer_id")
		case p.RemoteSessionIssuerID != nil:
			parsed, err := uuid.Parse(*p.RemoteSessionIssuerID)
			if err != nil || parsed == uuid.Nil {
				return nil, oops.E(oops.CodeBadRequest, err, "remote_session_issuer_id must be a UUID")
			}
			remoteIssuer = parsed
		}
		entry := vendorProvider{kind: kind, remoteIssuer: remoteIssuer, issuer: uuid.Nil}
		if slices.Contains(out, entry) {
			return nil, oops.E(oops.CodeBadRequest, nil, "each provider may appear once")
		}
		out = append(out, entry)
	}
	return out, nil
}

// vendorProvidersView reads the saved order, or the default when none is
// saved: every external supplier, then the Speakeasy issuer when it is on.
func (s *Service) vendorProvidersView(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID, vendor string) (*gen.VendorIdentityProvidersResult, error) {
	rows, err := q.ListVendorIdentityProviders(ctx, repo.ListVendorIdentityProvidersParams{OrganizationID: organizationID, VendorIssuer: vendor})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read identity provider order").LogError(ctx, logger)
	}
	result := &gen.VendorIdentityProvidersResult{VendorIssuer: vendor, IsDefault: len(rows) == 0, Providers: []*gen.VendorIdentityProvider{}}
	for _, row := range rows {
		result.Providers = append(result.Providers, &gen.VendorIdentityProvider{
			Kind:                    string(rowKind(row.RemoteSessionIssuerID)),
			RemoteSessionIssuerID:   nullUUIDString(row.RemoteSessionIssuerID),
			InternalSessionIssuerID: nullUUIDString(row.InternalSessionIssuerID),
			Unavailable:             row.ProviderUnavailable,
		})
	}
	if !result.IsDefault {
		return result, nil
	}
	suppliers, err := q.ListExternalIDJAGSuppliers(ctx, conv.ToPGText(organizationID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read external ID-JAG suppliers").LogError(ctx, logger)
	}
	for _, supplier := range suppliers {
		result.Providers = append(result.Providers, &gen.VendorIdentityProvider{Kind: string(remotesessions.EMAProviderExternal), RemoteSessionIssuerID: conv.PtrEmpty(supplier.ID.String()), InternalSessionIssuerID: nil, Unavailable: false})
	}
	issuer, err := q.GetLiveInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read Speakeasy issuer").LogError(ctx, logger)
	case !issuer.Disabled:
		result.Providers = append(result.Providers, &gen.VendorIdentityProvider{Kind: string(remotesessions.EMAProviderSpeakeasy), RemoteSessionIssuerID: nil, InternalSessionIssuerID: conv.PtrEmpty(issuer.ID.String()), Unavailable: false})
	}
	return result, nil
}

func vendorProvidersSnapshot(vendor string, rows []repo.ListVendorIdentityProvidersRow) *audit.OrganizationEMAVendorIdentityProvidersSnapshot {
	snapshot := &audit.OrganizationEMAVendorIdentityProvidersSnapshot{VendorIssuer: vendor, Providers: make([]audit.OrganizationEMAVendorIdentityProvider, 0, len(rows))}
	for _, row := range rows {
		snapshot.Providers = append(snapshot.Providers, auditVendorProvider(row.RemoteSessionIssuerID, row.InternalSessionIssuerID))
	}
	return snapshot
}

func auditVendorProvider(remoteIssuer, issuer uuid.NullUUID) audit.OrganizationEMAVendorIdentityProvider {
	return audit.OrganizationEMAVendorIdentityProvider{
		Kind:                    string(rowKind(remoteIssuer)),
		RemoteSessionIssuerID:   conv.PtrValOr(nullUUIDString(remoteIssuer), ""),
		InternalSessionIssuerID: conv.PtrValOr(nullUUIDString(issuer), ""),
	}
}

// rowKind names the supplier a policy row holds: a remote session issuer is
// external, anything else is the Speakeasy issuer.
func rowKind(remoteIssuer uuid.NullUUID) remotesessions.EMAProviderKind {
	if remoteIssuer.Valid {
		return remotesessions.EMAProviderExternal
	}
	return remotesessions.EMAProviderSpeakeasy
}

func nullID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}

func nullUUIDString(id uuid.NullUUID) *string {
	if !id.Valid {
		return nil
	}
	return conv.PtrEmpty(id.UUID.String())
}
