package remotesessions

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	orgissuersgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/issuerurl"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// emaService accumulates one vendor's bindings before the view is built.
type emaService struct {
	name     string
	issuerID uuid.UUID
	servers  map[uuid.UUID]struct{}
	ready    map[uuid.UUID]struct{}
}

func newEMAService() *emaService {
	return &emaService{name: "", issuerID: uuid.Nil, servers: map[uuid.UUID]struct{}{}, ready: map[uuid.UUID]struct{}{}}
}

// emaBindingSetUp reports whether a binding state means identity chaining was
// turned on for the server, whatever happened after.
func emaBindingSetUp(state string) bool {
	return state != "" && state != "unlinked" && state != "configuration_required"
}

func (s *Service) ListEMAServices(ctx context.Context, _ *orgissuersgen.ListEMAServicesPayload) (*orgissuersgen.ListEMAServicesResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	org := authCtx.ActiveOrganizationID
	logger := s.logger.With(attr.SlogOrganizationID(org))
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: org, Dimensions: nil}); err != nil {
		return nil, err
	}
	q := repo.New(s.db)

	rows, err := q.ListEMAServiceServers(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list identity chaining bindings").LogError(ctx, logger)
	}
	services := map[string]*emaService{}
	for _, row := range rows {
		vendor, err := NormalizeVendorIssuer(row.Issuer)
		if err != nil {
			continue
		}
		svc := services[vendor]
		if svc == nil {
			svc = newEMAService()
			services[vendor] = svc
		}
		// Rows arrive organization-level issuer first, then oldest, so the
		// first record seen is the one setEMAProviders should name.
		if svc.issuerID == uuid.Nil {
			svc.issuerID, svc.name = row.RemoteSessionIssuerID, row.Name
		}
		if !row.McpServerID.Valid {
			continue
		}
		if emaBindingSetUp(row.State) {
			svc.servers[row.McpServerID.UUID] = struct{}{}
		}
		if row.State == "ready" {
			svc.ready[row.McpServerID.UUID] = struct{}{}
		}
	}
	saved, err := q.ListEMAProviderVendors(ctx, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema provider orders").LogError(ctx, logger)
	}
	for _, vendor := range saved {
		if services[vendor] != nil {
			continue
		}
		svc := newEMAService()
		// A saved order outlives its last binding; find a record that names
		// the vendor so an administrator can still change or clear it.
		spellings := []string{vendor}
		if canonical, err := issuerurl.Parse(vendor); err == nil {
			spellings = canonical.MatchCandidates()
		}
		record, err := q.GetEMAVendorIssuerRecord(ctx, repo.GetEMAVendorIssuerRecordParams{Issuers: spellings, OrganizationID: org})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "read ema vendor issuer").LogError(ctx, logger)
		default:
			svc.issuerID, svc.name = record.ID, record.Name
		}
		services[vendor] = svc
	}

	acceptance, err := ListEMAVendorAcceptance(ctx, q, org)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema vendor acceptance").LogError(ctx, logger)
	}

	vendors := make([]string, 0, len(services))
	for vendor := range services {
		vendors = append(vendors, vendor)
	}
	sort.Strings(vendors)
	// Speakeasy readiness depends only on the organization's issuer, so one
	// check serves every vendor in the request.
	readiness := memoizedReadiness(s.speakeasyReadiness)
	scope := EMAProviderScope{Endpoint: false, TrustedIssuer: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, SpeakeasySigns: s.speakeasySigningAvailable}
	items := make([]*orgissuersgen.EMAService, 0, len(vendors))
	for _, vendor := range vendors {
		svc := services[vendor]
		order, err := ResolveEMAProviders(ctx, q, org, vendor, scope)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read ema provider order").LogError(ctx, logger)
		}
		order = StampEMAProviderLastAccepted(order, acceptance[vendor])
		order, err = ApplyEMAProviderReadiness(ctx, org, order, readiness)
		if err != nil {
			logger.WarnContext(ctx, "check Speakeasy issuer readiness", attr.SlogError(err), attr.SlogOAuthIssuer(vendor))
		}
		item := &orgissuersgen.EMAService{
			VendorIssuer:          vendor,
			Name:                  svc.name,
			RemoteSessionIssuerID: nil,
			EmaProviders:          EMAProviderOrderView(order),
			ServerCount:           len(svc.servers),
			ReadyServerCount:      len(svc.ready),
		}
		if svc.issuerID != uuid.Nil {
			item.RemoteSessionIssuerID = conv.PtrEmpty(svc.issuerID.String())
		}
		items = append(items, item)
	}
	return &orgissuersgen.ListEMAServicesResult{Items: items}, nil
}

// memoizedReadiness answers each issuer's readiness once per request.
func memoizedReadiness(check EMAProviderReadiness) EMAProviderReadiness {
	if check == nil {
		return nil
	}
	type answer struct {
		reason EMAProviderUnavailableReason
		err    error
	}
	seen := map[uuid.UUID]answer{}
	return func(ctx context.Context, organizationID string, issuerID uuid.UUID) (EMAProviderUnavailableReason, error) {
		if a, ok := seen[issuerID]; ok {
			return a.reason, a.err
		}
		reason, err := check(ctx, organizationID, issuerID)
		seen[issuerID] = answer{reason: reason, err: err}
		return reason, err
	}
}
