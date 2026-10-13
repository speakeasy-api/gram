package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func (s *Service) SetEMAProvider(ctx context.Context, payload *orgissuersgen.SetEMAProviderPayload) (*types.EMAProviderSelection, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	org := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(org), attr.SlogUserID(authCtx.UserID))
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: org, Dimensions: nil}); err != nil {
		return nil, err
	}
	if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil, oops.E(oops.CodeForbidden, nil, "the EMA provider cannot be changed from a support or impersonated session").LogError(ctx, logger)
	}
	vendorID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote session issuer id")
	}
	provider, err := parseEMAProvider(payload.Provider)
	if err != nil {
		return nil, err
	}
	// Restoring the external issuer stays possible with the rollout off, so
	// a saved selection can always be undone.
	if provider != nil {
		if err := s.requireEMAProviders(ctx, logger, org); err != nil {
			return nil, err
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	q := repo.New(dbtx)
	if err := q.SetEMAProvidersLockTimeout(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "bound ema provider lock wait").LogError(ctx, logger)
	}
	if err := q.LockEMAProviders(ctx, org); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "the EMA provider is being changed; retry shortly").LogError(ctx, logger)
	}
	vendorRow, err := q.GetOrganizationRemoteSessionIssuerByID(ctx, repo.GetOrganizationRemoteSessionIssuerByIDParams{ID: vendorID, OrganizationID: conv.ToPGText(org), IncludeGlobal: true})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "remote session issuer not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "read remote session issuer").LogError(ctx, logger)
	}
	vendor, err := NormalizeVendorIssuer(vendorRow.Issuer)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "the remote session issuer's URL is not an issuer identifier")
	}
	if provider != nil {
		if err := resolveEMAProviderID(ctx, logger, q, org, provider); err != nil {
			return nil, err
		}
	}

	beforeRows, err := q.ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: org, VendorIssuer: vendor})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider").LogError(ctx, logger)
	}
	before := &audit.RemoteSessionIssuerEMAProviderSelection{VendorIssuer: vendor, Provider: nil}
	if len(beforeRows) > 0 {
		before.Provider = new(auditEMAProvider(beforeRows[0].RemoteSessionIssuerID.UUID, beforeRows[0].InternalSessionIssuerID.UUID))
	}
	after := &audit.RemoteSessionIssuerEMAProviderSelection{VendorIssuer: vendor, Provider: nil}
	if provider != nil {
		after.Provider = new(auditEMAProvider(provider.RemoteSessionIssuerID, provider.InternalSessionIssuerID))
	}
	scope := EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable}
	if len(beforeRows) <= 1 && equalAuditEMAProvider(before.Provider, after.Provider) {
		selection, err := ResolveEMAProvider(ctx, q, org, vendor, scope)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ema provider").LogError(ctx, logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "release ema provider").LogError(ctx, logger)
		}
		return s.liveEMAProviderView(ctx, logger, org, selection), nil
	}

	if _, err := q.DeleteEMAProviders(ctx, repo.DeleteEMAProvidersParams{OrganizationID: org, VendorIssuer: vendor}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear ema provider").LogError(ctx, logger)
	}
	if provider != nil {
		if err := q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{
			OrganizationID:          org,
			VendorIssuer:            vendor,
			RemoteSessionIssuerID:   uuid.NullUUID{UUID: provider.RemoteSessionIssuerID, Valid: provider.RemoteSessionIssuerID != uuid.Nil},
			InternalSessionIssuerID: uuid.NullUUID{UUID: provider.InternalSessionIssuerID, Valid: provider.InternalSessionIssuerID != uuid.Nil},
			Position:                0,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save ema provider").LogError(ctx, logger)
		}
	}
	if err := s.auditLogger.LogRemoteSessionIssuerUpdateEMAProvider(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEMAProviderEvent{
		OrganizationID:         org,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(vendorRow.ID),
		Slug:                   vendorRow.Slug,
		IssuerURL:              vendorRow.Issuer,
		Name:                   conv.FromPGText[string](vendorRow.Name),
		EMAProviderBefore:      before,
		EMAProviderAfter:       after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ema provider").LogError(ctx, logger)
	}
	// Read under the lock, so the response is this write's selection.
	selection, err := ResolveEMAProvider(ctx, q, org, vendor, scope)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ema provider").LogError(ctx, logger)
	}
	return s.liveEMAProviderView(ctx, logger, org, selection), nil
}

// requireEMAProviders checks the rollout flag for selecting a provider; a
// lookup failure reads as unavailable, never forbidden.
func (s *Service) requireEMAProviders(ctx context.Context, logger *slog.Logger, organizationID string) error {
	if s.featureFlags == nil {
		return oops.E(oops.CodeForbidden, nil, "selecting an EMA provider is not enabled for this organization")
	}
	org, err := orgrepo.New(s.db).GetOrganizationMetadata(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "EMA provider availability could not be determined").LogError(ctx, logger)
	}
	enabled, err := s.featureFlags.IsFlagEnabled(ctx, feature.FlagEMAProviders, organizationID, feature.OrgProjectGroups(org.Slug, ""))
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "EMA provider availability could not be determined").LogError(ctx, logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "selecting an EMA provider is not enabled for this organization")
	}
	return nil
}

// parseEMAProvider reads an explicit provider; nil restores the endpoint's
// external issuer.
func parseEMAProvider(in *types.EMAProviderInput) (*EMAProvider, error) {
	if in == nil {
		return nil, nil
	}
	entry := EMAProvider{Kind: EMAProviderKind(in.Kind), RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: ""}
	switch {
	case entry.Kind == EMAProviderExternal && in.RemoteSessionIssuerID == nil:
		return nil, oops.E(oops.CodeBadRequest, nil, "external providers need remote_session_issuer_id")
	case entry.Kind == EMAProviderSpeakeasy && in.RemoteSessionIssuerID != nil:
		return nil, oops.E(oops.CodeBadRequest, nil, "the speakeasy provider takes no remote_session_issuer_id")
	case entry.Kind != EMAProviderExternal && entry.Kind != EMAProviderSpeakeasy:
		return nil, oops.E(oops.CodeBadRequest, nil, "provider kind must be external or speakeasy")
	case in.RemoteSessionIssuerID != nil:
		id, err := uuid.Parse(*in.RemoteSessionIssuerID)
		if err != nil || id == uuid.Nil {
			return nil, oops.E(oops.CodeBadRequest, err, "remote_session_issuer_id must be a UUID")
		}
		entry.RemoteSessionIssuerID = id
	}
	return &entry, nil
}

// resolveEMAProviderID checks an external provider is one the organization
// can name, and resolves speakeasy to its internal session issuer.
func resolveEMAProviderID(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, provider *EMAProvider) error {
	if provider.Kind == EMAProviderExternal {
		candidates, err := q.ListEMAExternalProviderCandidates(ctx, conv.ToPGText(organizationID))
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read external ema providers").LogError(ctx, logger)
		}
		if !slices.ContainsFunc(candidates, func(c repo.ListEMAExternalProviderCandidatesRow) bool { return c.ID == provider.RemoteSessionIssuerID }) {
			return oops.E(oops.CodeBadRequest, nil, "remote_session_issuer_id must name an organization-level issuer that an organization user session issuer trusts")
		}
		return nil
	}
	issuer, err := q.GetEMAInternalSessionIssuer(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeBadRequest, nil, "the organization has no Speakeasy issuer; set one up before selecting it")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "read Speakeasy issuer").LogError(ctx, logger)
	}
	provider.InternalSessionIssuerID = issuer.ID
	return nil
}

// emaProviderView is the organization-wide selection for vendor, the same
// one runtime selection resolves.
func (s *Service) emaProviderView(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID, vendor string) (*types.EMAProviderSelection, error) {
	selection, err := ResolveEMAProvider(ctx, q, organizationID, vendor, EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider").LogError(ctx, logger)
	}
	return s.liveEMAProviderView(ctx, logger, organizationID, selection), nil
}

// liveEMAProviderView runs after the selection's transaction releases its
// locks. A failed readiness read is reported as readiness_unknown: it must
// never fail a read, or a write that already committed.
func (s *Service) liveEMAProviderView(ctx context.Context, logger *slog.Logger, organizationID string, selection EMAProviderSelection) *types.EMAProviderSelection {
	selection, err := ApplyEMAProviderReadiness(ctx, organizationID, selection, s.speakeasyReadiness)
	if err != nil {
		logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err))
	}
	return EMAProviderSelectionView(selection)
}

// EMAProviderSelectionView renders a selection for the management API.
func EMAProviderSelectionView(selection EMAProviderSelection) *types.EMAProviderSelection {
	p := selection.Provider
	view := &types.EMAProvider{Kind: string(p.Kind), RemoteSessionIssuerID: nil, InternalSessionIssuerID: nil, UnavailableReason: nil}
	if p.RemoteSessionIssuerID != uuid.Nil {
		view.RemoteSessionIssuerID = conv.PtrEmpty(p.RemoteSessionIssuerID.String())
	}
	if p.InternalSessionIssuerID != uuid.Nil {
		view.InternalSessionIssuerID = conv.PtrEmpty(p.InternalSessionIssuerID.String())
	}
	if p.Unavailable != "" {
		view.UnavailableReason = conv.PtrEmpty(string(p.Unavailable))
	}
	return &types.EMAProviderSelection{VendorIssuer: selection.VendorIssuer, IsDefault: selection.IsDefault, Provider: view}
}

func auditEMAProvider(remoteIssuer, internalIssuer uuid.UUID) audit.RemoteSessionIssuerEMAProvider {
	p := audit.RemoteSessionIssuerEMAProvider{Kind: string(EMAProviderSpeakeasy), RemoteSessionIssuerID: "", InternalSessionIssuerID: ""}
	if remoteIssuer != uuid.Nil {
		p.Kind, p.RemoteSessionIssuerID = string(EMAProviderExternal), remoteIssuer.String()
	}
	if internalIssuer != uuid.Nil {
		p.InternalSessionIssuerID = internalIssuer.String()
	}
	return p
}

func equalAuditEMAProvider(a, b *audit.RemoteSessionIssuerEMAProvider) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
