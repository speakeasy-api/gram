package remotesessions

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

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

func (s *Service) SetEMAProviders(ctx context.Context, payload *orgissuersgen.SetEMAProvidersPayload) (*types.EMAProviderOrder, error) {
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
		return nil, oops.E(oops.CodeForbidden, nil, "the EMA provider order cannot be changed from a support or impersonated session").LogError(ctx, logger)
	}
	vendorID, err := uuid.Parse(payload.RemoteSessionIssuerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote session issuer id")
	}
	providers, err := parseEMAProviders(payload.Providers)
	if err != nil {
		return nil, err
	}
	var expected []EMAProvider
	if payload.ExpectedProviders != nil {
		if expected, err = parseEMAProviders(payload.ExpectedProviders); err != nil {
			return nil, err
		}
	}
	// Restoring the external default stays possible with the rollout off, so
	// a saved order can always be undone.
	if len(providers) > 0 {
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
		return nil, oops.E(oops.CodeUnexpected, err, "bound ema provider order lock wait").LogError(ctx, logger)
	}
	if err := q.LockEMAProviders(ctx, org); err != nil {
		return nil, oops.E(oops.CodeUnavailable, err, "the EMA provider order is being changed; retry shortly").LogError(ctx, logger)
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
	beforeRows, err := q.ListEMAProviders(ctx, repo.ListEMAProvidersParams{OrganizationID: org, VendorIssuer: vendor})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
	}
	// A stale edit conflicts before its providers are checked, so one naming a
	// since-deleted provider reads as changed, not invalid.
	if payload.ExpectedProviders != nil && !savedOrderMatches(beforeRows, expected) {
		return nil, oops.E(oops.CodeConflict, nil, "the EMA provider order changed since you started editing; review it and try again").LogInfo(ctx, logger)
	}
	if err := resolveEMAProviderIDs(ctx, logger, q, org, providers); err != nil {
		return nil, err
	}
	before := &audit.RemoteSessionIssuerEMAProviders{VendorIssuer: vendor, Providers: make([]audit.RemoteSessionIssuerEMAProvider, 0, len(beforeRows))}
	for _, row := range beforeRows {
		before.Providers = append(before.Providers, auditEMAProvider(row.RemoteSessionIssuerID.UUID, row.InternalSessionIssuerID.UUID))
	}
	after := &audit.RemoteSessionIssuerEMAProviders{VendorIssuer: vendor, Providers: make([]audit.RemoteSessionIssuerEMAProvider, 0, len(providers))}
	for _, p := range providers {
		after.Providers = append(after.Providers, auditEMAProvider(p.RemoteSessionIssuerID, p.InternalSessionIssuerID))
	}
	scope := EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable}
	if slices.Equal(before.Providers, after.Providers) {
		order, err := ResolveEMAProviders(ctx, q, org, vendor, scope)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "release ema provider order").LogError(ctx, logger)
		}
		return s.liveEMAProvidersView(ctx, logger, org, order), nil
	}

	if _, err := q.DeleteEMAProviders(ctx, repo.DeleteEMAProvidersParams{OrganizationID: org, VendorIssuer: vendor}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear ema provider order").LogError(ctx, logger)
	}
	for i, p := range providers {
		if err := q.InsertEMAProvider(ctx, repo.InsertEMAProviderParams{
			OrganizationID:          org,
			VendorIssuer:            vendor,
			RemoteSessionIssuerID:   uuid.NullUUID{UUID: p.RemoteSessionIssuerID, Valid: p.RemoteSessionIssuerID != uuid.Nil},
			InternalSessionIssuerID: uuid.NullUUID{UUID: p.InternalSessionIssuerID, Valid: p.InternalSessionIssuerID != uuid.Nil},
			Position:                int32(i),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "save ema provider order").LogError(ctx, logger)
		}
	}
	if err := s.auditLogger.LogRemoteSessionIssuerUpdateEMAProviders(ctx, dbtx, audit.LogRemoteSessionIssuerUpdateEMAProvidersEvent{
		OrganizationID:         org,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		RemoteSessionIssuerURN: urn.NewRemoteSessionIssuer(vendorRow.ID),
		Slug:                   vendorRow.Slug,
		IssuerURL:              vendorRow.Issuer,
		Name:                   conv.FromPGText[string](vendorRow.Name),
		EMAProvidersBefore:     before,
		EMAProvidersAfter:      after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log ema provider order").LogError(ctx, logger)
	}
	// Read under the lock, so the response is this write's order.
	order, err := ResolveEMAProviders(ctx, q, org, vendor, scope)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit ema provider order").LogError(ctx, logger)
	}
	return s.liveEMAProvidersView(ctx, logger, org, order), nil
}

// requireEMAProviders checks the rollout flag for saving an order; a lookup
// failure reads as unavailable, never forbidden.
func (s *Service) requireEMAProviders(ctx context.Context, logger *slog.Logger, organizationID string) error {
	if s.featureFlags == nil {
		return oops.E(oops.CodeForbidden, nil, "the EMA provider order is not enabled for this organization")
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
		return oops.E(oops.CodeForbidden, nil, "the EMA provider order is not enabled for this organization")
	}
	return nil
}

// savedOrderMatches reports whether the saved rows are the order a caller
// expected, entry by entry. The organization has one Speakeasy issuer, so a
// Speakeasy entry matches by kind.
func savedOrderMatches(rows []repo.ListEMAProvidersRow, expected []EMAProvider) bool {
	if len(rows) != len(expected) {
		return false
	}
	for i, row := range rows {
		want := expected[i]
		switch {
		case row.RemoteSessionIssuerID.Valid:
			if want.Kind != EMAProviderExternal || want.RemoteSessionIssuerID != row.RemoteSessionIssuerID.UUID {
				return false
			}
		case want.Kind != EMAProviderSpeakeasy:
			return false
		}
	}
	return true
}

// maxEMAProviders bounds an order's length; the API documents it.
const maxEMAProviders = 10

// parseEMAProviders validates an order's shape: a remote session issuer
// exactly for external entries, and each provider at most once. An empty
// order restores the endpoint's external issuer.
func parseEMAProviders(in []*types.EMAProviderInput) ([]EMAProvider, error) {
	if len(in) > maxEMAProviders {
		return nil, oops.E(oops.CodeBadRequest, nil, "an EMA provider order has at most %d providers", maxEMAProviders)
	}
	out := make([]EMAProvider, 0, len(in))
	for _, p := range in {
		if p == nil {
			return nil, oops.E(oops.CodeBadRequest, nil, "providers must not contain null entries")
		}
		entry := EMAProvider{Kind: EMAProviderKind(p.Kind), RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil}
		switch {
		case entry.Kind == EMAProviderExternal && p.RemoteSessionIssuerID == nil:
			return nil, oops.E(oops.CodeBadRequest, nil, "external providers need remote_session_issuer_id")
		case entry.Kind == EMAProviderSpeakeasy && p.RemoteSessionIssuerID != nil:
			return nil, oops.E(oops.CodeBadRequest, nil, "the speakeasy provider takes no remote_session_issuer_id")
		case entry.Kind != EMAProviderExternal && entry.Kind != EMAProviderSpeakeasy:
			return nil, oops.E(oops.CodeBadRequest, nil, "provider kind must be external or speakeasy")
		case p.RemoteSessionIssuerID != nil:
			id, err := uuid.Parse(*p.RemoteSessionIssuerID)
			if err != nil || id == uuid.Nil {
				return nil, oops.E(oops.CodeBadRequest, err, "remote_session_issuer_id must be a UUID")
			}
			entry.RemoteSessionIssuerID = id
		}
		if slices.Contains(out, entry) {
			return nil, oops.E(oops.CodeBadRequest, nil, "each provider may appear in the order once")
		}
		out = append(out, entry)
	}
	return out, nil
}

// resolveEMAProviderIDs checks each external provider is one the
// organization can name, and resolves speakeasy to its internal session issuer.
func resolveEMAProviderIDs(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID string, providers []EMAProvider) error {
	if len(providers) == 0 {
		return nil
	}
	candidates, err := q.ListEMAExternalProviderCandidates(ctx, conv.ToPGText(organizationID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read external ema providers").LogError(ctx, logger)
	}
	for i, p := range providers {
		if p.Kind == EMAProviderExternal {
			if !slices.ContainsFunc(candidates, func(c repo.ListEMAExternalProviderCandidatesRow) bool { return c.ID == p.RemoteSessionIssuerID }) {
				return oops.E(oops.CodeBadRequest, nil, "remote_session_issuer_id must name an organization-level issuer that an organization user session issuer trusts")
			}
			continue
		}
		issuer, err := q.GetEMAInternalSessionIssuer(ctx, organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return oops.E(oops.CodeBadRequest, nil, "the organization has no Speakeasy issuer; set one up before adding it")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "read Speakeasy issuer").LogError(ctx, logger)
		}
		providers[i].InternalSessionIssuerID = issuer.ID
	}
	return nil
}

// emaProvidersView is the organization-wide order for vendor, the same one
// runtime resolves.
func (s *Service) emaProvidersView(ctx context.Context, logger *slog.Logger, q *repo.Queries, organizationID, vendor string) (*types.EMAProviderOrder, error) {
	order, err := ResolveEMAProviders(ctx, q, organizationID, vendor, EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
	}
	return s.liveEMAProvidersView(ctx, logger, organizationID, order), nil
}

// liveEMAProvidersView runs after the order's transaction releases its
// locks, and stamps each provider's organization-wide acceptance so the field
// means the same on every response. A failed readiness read is reported as
// readiness_unknown, and a failed acceptance read leaves acceptance out: it
// must never fail a read, or a write that already committed.
func (s *Service) liveEMAProvidersView(ctx context.Context, logger *slog.Logger, organizationID string, order EMAProviderOrder) *types.EMAProviderOrder {
	order, err := ApplyEMAProviderReadiness(ctx, organizationID, order, s.speakeasyReadiness)
	if err != nil {
		logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err))
	}
	if accepted, err := VendorEMAAcceptance(ctx, repo.New(s.db), organizationID, order.VendorIssuer); err != nil {
		logger.WarnContext(ctx, "read ema vendor acceptance", attr.SlogError(err))
	} else {
		order = StampEMAProviderLastAccepted(order, accepted)
	}
	return EMAProviderOrderView(order)
}

// EMAProviderOrderView renders an order for the management API.
func EMAProviderOrderView(order EMAProviderOrder) *types.EMAProviderOrder {
	out := &types.EMAProviderOrder{VendorIssuer: order.VendorIssuer, IsDefault: order.IsDefault, Providers: make([]*types.EMAProvider, 0, len(order.Providers))}
	for _, p := range order.Providers {
		view := &types.EMAProvider{Kind: string(p.Kind), RemoteSessionIssuerID: nil, InternalSessionIssuerID: nil, UnavailableReason: nil, LastAcceptedAt: nil}
		if p.RemoteSessionIssuerID != uuid.Nil {
			view.RemoteSessionIssuerID = conv.PtrEmpty(p.RemoteSessionIssuerID.String())
		}
		if p.InternalSessionIssuerID != uuid.Nil {
			view.InternalSessionIssuerID = conv.PtrEmpty(p.InternalSessionIssuerID.String())
		}
		if p.Unavailable != "" {
			view.UnavailableReason = conv.PtrEmpty(string(p.Unavailable))
		}
		if p.LastAcceptedAt != nil {
			view.LastAcceptedAt = conv.PtrEmpty(p.LastAcceptedAt.UTC().Format(time.RFC3339))
		}
		out.Providers = append(out.Providers, view)
	}
	return out
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
